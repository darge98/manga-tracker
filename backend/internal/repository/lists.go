package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type List struct {
	ID          string    `json:"id"`
	OwnerID     string    `json:"-"` // never exposed, lists can be public
	Name        string    `json:"name"`
	Description string    `json:"description"`
	IsPublic    bool      `json:"isPublic"`
	ItemCount   int       `json:"itemCount"`
	CreatedAt   time.Time `json:"createdAt"`
}

// ListPatch holds the fields to change; nil fields are left untouched.
type ListPatch struct {
	Name        *string
	Description *string
	IsPublic    *bool
}

const listColumns = `l.id,l.owner_id,l.name,l.description,l.is_public,l.created_at,
	(SELECT count(*) FROM list_items i WHERE i.list_id=l.id)`

func scanList(row pgx.Row) (List, error) {
	var l List
	err := row.Scan(&l.ID, &l.OwnerID, &l.Name, &l.Description, &l.IsPublic, &l.CreatedAt, &l.ItemCount)
	l.CreatedAt = l.CreatedAt.UTC()
	return l, wrap(err)
}

func (s *Store) ListsByOwner(ctx context.Context, ownerID string) ([]List, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+listColumns+" FROM lists l WHERE l.owner_id=$1 ORDER BY l.created_at, l.id", ownerID)
	if err != nil {
		return nil, wrap(err)
	}
	lists, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (List, error) { return scanList(row) })
	if lists == nil {
		lists = []List{}
	}
	return lists, wrap(err)
}

func (s *Store) CreateList(ctx context.Context, l List) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO lists(id,owner_id,name,description,is_public,created_at) VALUES($1,$2,$3,$4,$5,$6)",
		l.ID, l.OwnerID, l.Name, l.Description, l.IsPublic, l.CreatedAt)
	return wrap(err)
}

// List returns any list by ID; callers check ownership or visibility.
func (s *Store) List(ctx context.Context, id string) (List, error) {
	return scanList(s.pool.QueryRow(ctx, "SELECT "+listColumns+" FROM lists l WHERE l.id=$1", id))
}

func (s *Store) ListItems(ctx context.Context, listID string) ([]Manga, error) {
	return collectManga(s.pool.Query(ctx, `
		SELECT m.metadata FROM list_items i JOIN manga m ON m.id=i.manga_id
		WHERE i.list_id=$1 ORDER BY i.position`, listID))
}

// UpdateList returns ErrNotFound unless the list exists and belongs to ownerID.
func (s *Store) UpdateList(ctx context.Context, id, ownerID string, patch ListPatch) (List, error) {
	return scanList(s.pool.QueryRow(ctx, `
		UPDATE lists l SET
			name=COALESCE($3::text,l.name),
			description=COALESCE($4::text,l.description),
			is_public=COALESCE($5::boolean,l.is_public)
		WHERE l.id=$1 AND l.owner_id=$2
		RETURNING `+listColumns, id, ownerID, patch.Name, patch.Description, patch.IsPublic))
}

func (s *Store) DeleteList(ctx context.Context, id, ownerID string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM lists WHERE id=$1 AND owner_id=$2", id, ownerID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return wrap(err)
}

// AddListItem appends the manga to the list; adding it again is a no-op.
func (s *Store) AddListItem(ctx context.Context, id, ownerID string, mangaID int) (List, error) {
	return s.changeItems(ctx, id, ownerID, `
		INSERT INTO list_items(list_id,manga_id,position)
		SELECT $1,$2,COALESCE(max(position)+1,0) FROM list_items WHERE list_id=$1
		ON CONFLICT(list_id,manga_id) DO NOTHING`, mangaID)
}

func (s *Store) RemoveListItem(ctx context.Context, id, ownerID string, mangaID int) (List, error) {
	return s.changeItems(ctx, id, ownerID, "DELETE FROM list_items WHERE list_id=$1 AND manga_id=$2", mangaID)
}

// changeItems locks the owned list row so concurrent additions get distinct positions.
func (s *Store) changeItems(ctx context.Context, id, ownerID, statement string, mangaID int) (List, error) {
	var out List
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var locked string
		if err := tx.QueryRow(ctx, "SELECT id FROM lists WHERE id=$1 AND owner_id=$2 FOR UPDATE", id, ownerID).Scan(&locked); err != nil {
			return wrap(err)
		}
		if _, err := tx.Exec(ctx, statement, id, mangaID); err != nil {
			return wrap(err)
		}
		var err error
		out, err = scanList(tx.QueryRow(ctx, "SELECT "+listColumns+" FROM lists l WHERE l.id=$1", id))
		return err
	})
	return out, err
}
