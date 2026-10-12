package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

type Manga struct {
	ID          int      `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	CoverURL    string   `json:"coverUrl"`
	Chapters    *int     `json:"chapters"`
	Status      string   `json:"status"`
	Genres      []string `json:"genres"`
	Score       *float64 `json:"score"`
	Author      string   `json:"author"`
	URL         string   `json:"url"`
}

type Chapter struct {
	Chapter string     `json:"chapter"` // canonical decimal text, e.g. "12.5"
	Read    bool       `json:"read"`
	ReadAt  *time.Time `json:"readAt"`
	Rating  *int       `json:"rating"`
	Notes   string     `json:"notes"`
}

type Library struct {
	MangaID  int       `json:"mangaId"`
	Chapters []Chapter `json:"chapters"`
	ToRead   bool      `json:"toRead"`
	Favorite bool      `json:"favorite"`
}

type RecentRead struct {
	Manga   Manga     `json:"manga"`
	Chapter string    `json:"chapter"`
	ReadAt  time.Time `json:"readAt"`
	Rating  *int      `json:"rating"`
	Notes   string    `json:"notes"`
}

// SaveManga stores catalog metadata, skipping the write when nothing changed.
func (s *Store) SaveManga(ctx context.Context, m Manga) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO manga(id,metadata) VALUES($1,$2)
		ON CONFLICT(id) DO UPDATE SET metadata=EXCLUDED.metadata
		WHERE manga.metadata IS DISTINCT FROM EXCLUDED.metadata`, m.ID, m)
	return wrap(err)
}

func (s *Store) Manga(ctx context.Context, id int) (Manga, error) {
	var m Manga
	err := s.pool.QueryRow(ctx, "SELECT metadata FROM manga WHERE id=$1", id).Scan(&m)
	return m, wrap(err)
}

// Library returns the user's progress for one manga, or an empty entry.
func (s *Store) Library(ctx context.Context, userID string, mangaID int) (Library, error) {
	return library(ctx, s.pool, userID, mangaID)
}

func library(ctx context.Context, q querier, userID string, mangaID int) (Library, error) {
	l := Library{MangaID: mangaID, Chapters: []Chapter{}}
	err := q.QueryRow(ctx, "SELECT to_read,favorite FROM library WHERE user_id=$1 AND manga_id=$2",
		userID, mangaID).Scan(&l.ToRead, &l.Favorite)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, nil // chapters reference library rows, so there are none either
	}
	if err != nil {
		return l, wrap(err)
	}
	rows, err := q.Query(ctx, `
		SELECT trim_scale(chapter)::text,is_read,read_at,rating,notes
		FROM chapters WHERE user_id=$1 AND manga_id=$2 ORDER BY chapter`, userID, mangaID)
	if err != nil {
		return l, wrap(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c Chapter
		if err = rows.Scan(&c.Chapter, &c.Read, &c.ReadAt, &c.Rating, &c.Notes); err != nil {
			return l, wrap(err)
		}
		if c.ReadAt != nil {
			utc := c.ReadAt.UTC()
			c.ReadAt = &utc
		}
		l.Chapters = append(l.Chapters, c)
	}
	return l, wrap(rows.Err())
}

// SetLibraryFlags updates the flags that are not nil, creating the entry if needed.
func (s *Store) SetLibraryFlags(ctx context.Context, userID string, mangaID int, toRead, favorite *bool) (Library, error) {
	var out Library
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO library(user_id,manga_id,to_read,favorite)
			VALUES($1,$2,COALESCE($3::boolean,false),COALESCE($4::boolean,false))
			ON CONFLICT(user_id,manga_id) DO UPDATE SET
				to_read=COALESCE($3::boolean,library.to_read),
				favorite=COALESCE($4::boolean,library.favorite)`,
			userID, mangaID, toRead, favorite)
		if err != nil {
			return wrap(err)
		}
		out, err = library(ctx, tx, userID, mangaID)
		return err
	})
	return out, err
}

// SaveChapter replaces the feedback for one chapter. The first read timestamp
// is kept while the chapter stays read; marking it unread clears it.
func (s *Store) SaveChapter(ctx context.Context, userID string, mangaID int, c Chapter, now time.Time) (Library, error) {
	var out Library
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO library(user_id,manga_id) VALUES($1,$2) ON CONFLICT DO NOTHING", userID, mangaID)
		if err != nil {
			return wrap(err)
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO chapters(user_id,manga_id,chapter,is_read,read_at,rating,notes)
			VALUES($1,$2,$3::text::numeric,$4::boolean,CASE WHEN $4::boolean THEN $5::timestamptz END,$6,$7)
			ON CONFLICT(user_id,manga_id,chapter) DO UPDATE SET
				is_read=EXCLUDED.is_read,
				read_at=CASE WHEN EXCLUDED.is_read THEN COALESCE(chapters.read_at,EXCLUDED.read_at) END,
				rating=EXCLUDED.rating,
				notes=EXCLUDED.notes`,
			userID, mangaID, c.Chapter, c.Read, now, c.Rating, c.Notes)
		if err != nil {
			return wrap(err)
		}
		out, err = library(ctx, tx, userID, mangaID)
		return err
	})
	return out, err
}

// Favorites and ToRead list the manga flagged in the user's library, by title.
func (s *Store) Favorites(ctx context.Context, userID string) ([]Manga, error) {
	return s.flagged(ctx, "SELECT m.metadata FROM library l JOIN manga m ON m.id=l.manga_id WHERE l.user_id=$1 AND l.favorite ORDER BY m.metadata->>'title', m.id", userID)
}

func (s *Store) ToRead(ctx context.Context, userID string) ([]Manga, error) {
	return s.flagged(ctx, "SELECT m.metadata FROM library l JOIN manga m ON m.id=l.manga_id WHERE l.user_id=$1 AND l.to_read ORDER BY m.metadata->>'title', m.id", userID)
}

func (s *Store) flagged(ctx context.Context, query, userID string) ([]Manga, error) {
	return collectManga(s.pool.Query(ctx, query, userID))
}

// RecentReads returns the latest read chapter of each manga, newest first.
func (s *Store) RecentReads(ctx context.Context, userID string, limit int) ([]RecentRead, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT m.metadata, r.chapter, r.read_at, r.rating, r.notes
		FROM (
			SELECT DISTINCT ON (manga_id) manga_id, trim_scale(chapter)::text AS chapter, read_at, rating, notes
			FROM chapters WHERE user_id=$1 AND is_read
			ORDER BY manga_id, read_at DESC, chapter DESC
		) r JOIN manga m ON m.id=r.manga_id
		ORDER BY r.read_at DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, wrap(err)
	}
	recent, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (RecentRead, error) {
		var r RecentRead
		err := row.Scan(&r.Manga, &r.Chapter, &r.ReadAt, &r.Rating, &r.Notes)
		r.ReadAt = r.ReadAt.UTC()
		return r, err
	})
	return recent, wrap(err)
}

func collectManga(rows pgx.Rows, err error) ([]Manga, error) {
	if err != nil {
		return nil, wrap(err)
	}
	items, err := pgx.CollectRows(rows, pgx.RowTo[Manga])
	if items == nil {
		items = []Manga{}
	}
	return items, wrap(err)
}
