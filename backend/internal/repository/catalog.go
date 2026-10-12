package repository

import (
	"context"
	"errors"
	"log"

	"github.com/jackc/pgx/v5"
)

// The manga table is a lean cache of catalog data, kept only for titles that
// some library entry or list references: enough to render cards and lists
// without one MyAnimeList call per item. Synopsis and authors are not stored;
// the detail page reads them live from the catalog.

// catalogEntry is the stored subset of Manga.
type catalogEntry struct {
	ID       int      `json:"id"`
	Title    string   `json:"title"`
	CoverURL string   `json:"coverUrl"`
	Chapters *int     `json:"chapters"`
	Status   string   `json:"status"`
	Genres   []string `json:"genres"`
	Score    *float64 `json:"score"`
	URL      string   `json:"url"`
}

func entryOf(m Manga) catalogEntry {
	return catalogEntry{m.ID, m.Title, m.CoverURL, m.Chapters, m.Status, m.Genres, m.Score, m.URL}
}

// ErrLimitReached means a per-user quota (see Limits) would be exceeded.
var ErrLimitReached = errors.New("limit reached")

// Limits cap what one account can store, so a single user cannot exhaust the
// database. They are generous for real readers.
type Limits struct {
	ChaptersPerUser int
	LibraryPerUser  int
	ListsPerUser    int
	ItemsPerList    int
}

var DefaultLimits = Limits{ChaptersPerUser: 20000, LibraryPerUser: 2000, ListsPerUser: 100, ItemsPerList: 500}

// Manga returns the stored catalog entry; Description and Author are empty.
func (s *Store) Manga(ctx context.Context, id int) (Manga, error) {
	var m Manga
	err := s.pool.QueryRow(ctx, "SELECT metadata FROM manga WHERE id=$1", id).Scan(&m)
	return m, wrap(err)
}

// RefreshManga updates an already stored entry with fresh catalog data. It
// never inserts: browsing alone must not grow the table.
func (s *Store) RefreshManga(ctx context.Context, m Manga) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE manga SET metadata=$2 WHERE id=$1 AND metadata IS DISTINCT FROM $2::jsonb`, m.ID, entryOf(m))
	return wrap(err)
}

// storeManga inserts the entry if missing, inside the transaction that is
// about to reference it, so the reference and the row are committed together.
func storeManga(ctx context.Context, tx pgx.Tx, m Manga) error {
	_, err := tx.Exec(ctx, "INSERT INTO manga(id,metadata) VALUES($1,$2) ON CONFLICT(id) DO NOTHING", m.ID, entryOf(m))
	return wrap(err)
}

// pruneManga deletes the given entries if nothing references them anymore. It
// runs after the change that dropped a reference has committed. If another
// request references one of them meanwhile, the foreign key rejects the
// delete and the entry stays, which is correct; so failures are only logged.
func (s *Store) pruneManga(ctx context.Context, ids []int) {
	if len(ids) == 0 {
		return
	}
	_, err := s.pool.Exec(ctx, `
		DELETE FROM manga m WHERE m.id = ANY($1)
		  AND NOT EXISTS (SELECT 1 FROM library l WHERE l.manga_id = m.id)
		  AND NOT EXISTS (SELECT 1 FROM list_items i WHERE i.manga_id = m.id)`, ids)
	if err != nil {
		log.Printf("pruning unreferenced manga failed: %v", err)
	}
}

// checkLimit returns ErrLimitReached when the count query already reaches limit.
func checkLimit(ctx context.Context, tx pgx.Tx, limit int, countQuery string, args ...any) error {
	var n int
	if err := tx.QueryRow(ctx, countQuery, args...).Scan(&n); err != nil {
		return wrap(err)
	}
	if n >= limit {
		return ErrLimitReached
	}
	return nil
}
