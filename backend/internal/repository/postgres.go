package repository

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS

const databaseLock int64 = 713120261011
const databaseTimeout = 20 * time.Second

type Repository interface {
	View(func(State)) error
	Update(func(*State) error) error
	Refresh() error
	Backend() string
	Close() error
}

// OpenConfigured never falls back to a JSON file if PostgreSQL was requested.
func OpenConfigured(databaseURL, path string) (*Store, error) {
	if databaseURL == "" {
		return Open(path)
	}
	return OpenPostgres(databaseURL)
}
func OpenPostgres(databaseURL string) (*Store, error) {
	registered := false
	for _, name := range sql.Drivers() {
		if name == "postgres" {
			registered = true
		}
	}
	if !registered {
		return nil, errors.New("PostgreSQL driver not enabled: rebuild with go build -tags postgres (Docker Compose includes this driver)")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		return nil, errors.New("DATABASE_URL must be a valid postgres:// or postgresql:// connection URL")
	}
	query := parsed.Query()
	if query.Get("connect_timeout") == "" {
		query.Set("connect_timeout", "10")
		parsed.RawQuery = query.Encode()
	}
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		return nil, errors.New("unable to configure PostgreSQL connection")
	}
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	s := &Store{db: db, state: empty()}
	success := false
	defer func() {
		if !success {
			db.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		return nil, errors.New("PostgreSQL unavailable: check database readiness and DATABASE_URL")
	}
	if err = s.migrate(ctx); err != nil {
		return nil, fmt.Errorf("PostgreSQL schema migration failed: %w", err)
	}
	if err = s.Refresh(); err != nil {
		return nil, err
	}
	success = true
	return s, nil
}
func (s *Store) Backend() string {
	if s.db != nil {
		return "postgres"
	}
	return "json"
}

var ErrDatabaseUnavailable = errors.New("database temporarily unavailable")

// Refresh is a readiness check. Reads never publish a shared PostgreSQL cache:
// each View consumes its own consistent transaction snapshot, so an older read
// cannot overwrite newer session revocation in another request/process.
func (s *Store) Refresh() error {
	if s.db == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		return ErrDatabaseUnavailable
	}
	return nil
}
func (s *Store) viewPostgres(fn func(State)) error {
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return ErrDatabaseUnavailable
	}
	defer tx.Rollback()
	state, err := loadState(ctx, tx)
	if err != nil {
		return ErrDatabaseUnavailable
	}
	if err = tx.Commit(); err != nil {
		return ErrDatabaseUnavailable
	}
	fn(state)
	return nil
}
func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return errors.New("cannot start migration transaction")
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", databaseLock); err != nil {
		return errors.New("cannot acquire migration lock")
	}
	data, err := migrations.ReadFile("migrations/001_initial.sql")
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, string(data)); err != nil {
		return errors.New("cannot apply initial schema")
	}
	return tx.Commit()
}
func (s *Store) updatePostgres(fn func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), databaseTimeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return errors.New("PostgreSQL unavailable")
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", databaseLock); err != nil {
		return errors.New("unable to lock PostgreSQL transaction")
	}
	before, err := loadState(ctx, tx)
	if err != nil {
		return errors.New("unable to read PostgreSQL data")
	}
	raw, err := json.Marshal(before)
	if err != nil {
		return err
	}
	var next State
	if err = json.Unmarshal(raw, &next); err != nil {
		return err
	}
	if err = fn(&next); err != nil {
		return err
	}
	if err = saveDiff(ctx, tx, before, next); err != nil {
		return errors.New("unable to persist PostgreSQL transaction")
	}
	if err = tx.Commit(); err != nil {
		return errors.New("unable to commit PostgreSQL transaction")
	}
	s.state = next
	return nil
}
func loadState(ctx context.Context, tx *sql.Tx) (State, error) {
	state := empty()
	readers := []struct {
		query string
		read  func(*sql.Rows) error
	}{
		{"SELECT id,username,email,password_hash FROM users", func(rows *sql.Rows) error {
			var u User
			if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash); err != nil {
				return err
			}
			state.Users[u.ID] = u
			return nil
		}},
		{"SELECT token_hash,user_id,expires_at FROM sessions", func(rows *sql.Rows) error {
			var key string
			var value Session
			if err := rows.Scan(&key, &value.UserID, &value.ExpiresAt); err != nil {
				return err
			}
			state.Sessions[key] = value
			return nil
		}},
		{"SELECT id,metadata FROM manga", func(rows *sql.Rows) error {
			var id int
			var raw []byte
			if err := rows.Scan(&id, &raw); err != nil {
				return err
			}
			var m Manga
			if err := json.Unmarshal(raw, &m); err != nil {
				return err
			}
			state.Manga[id] = m
			return nil
		}},
		{"SELECT user_id,manga_id,to_read,favorite FROM library", func(rows *sql.Rows) error {
			var owner string
			var l Library
			if err := rows.Scan(&owner, &l.MangaID, &l.ToRead, &l.Favorite); err != nil {
				return err
			}
			l.Chapters = []Chapter{}
			if state.Libraries[owner] == nil {
				state.Libraries[owner] = map[int]Library{}
			}
			state.Libraries[owner][l.MangaID] = l
			return nil
		}},
		{"SELECT user_id,manga_id,trim_scale(chapter)::text,is_read,read_at,rating,notes FROM chapters ORDER BY chapter", func(rows *sql.Rows) error {
			var owner string
			var id int
			var c Chapter
			if err := rows.Scan(&owner, &id, &c.Chapter, &c.Read, &c.ReadAt, &c.Rating, &c.Notes); err != nil {
				return err
			}
			l := state.Libraries[owner][id]
			l.Chapters = append(l.Chapters, c)
			state.Libraries[owner][id] = l
			return nil
		}},
		{"SELECT id,owner_id,name,description,is_public,created_at FROM lists", func(rows *sql.Rows) error {
			var l List
			if err := rows.Scan(&l.ID, &l.OwnerID, &l.Name, &l.Description, &l.IsPublic, &l.CreatedAt); err != nil {
				return err
			}
			l.ItemIDs = []int{}
			state.Lists[l.ID] = l
			return nil
		}},
		{"SELECT list_id,manga_id FROM list_items ORDER BY list_id,position", func(rows *sql.Rows) error {
			var id string
			var mangaID int
			if err := rows.Scan(&id, &mangaID); err != nil {
				return err
			}
			l := state.Lists[id]
			l.ItemIDs = append(l.ItemIDs, mangaID)
			state.Lists[id] = l
			return nil
		}},
	}
	for _, reader := range readers {
		rows, err := tx.QueryContext(ctx, reader.query)
		if err != nil {
			return State{}, err
		}
		for rows.Next() {
			if err = reader.read(rows); err != nil {
				rows.Close()
				return State{}, err
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return State{}, err
		}
	}
	return state, nil
}

// saveDiff changes only rows touched by the business mutation. All values are parameters.
func saveDiff(ctx context.Context, tx *sql.Tx, before, next State) error {
	exec := func(query string, args ...interface{}) error {
		_, err := tx.ExecContext(ctx, query, args...)
		return err
	}
	// Parent inserts/updates precede their dependent rows.
	for id, u := range next.Users {
		if reflect.DeepEqual(before.Users[id], u) {
			continue
		}
		if err := exec("INSERT INTO users(id,username,email,password_hash) VALUES($1,$2,$3,$4) ON CONFLICT(id) DO UPDATE SET username=EXCLUDED.username,email=EXCLUDED.email,password_hash=EXCLUDED.password_hash", id, u.Username, u.Email, u.PasswordHash); err != nil {
			return err
		}
	}
	for id, m := range next.Manga {
		if reflect.DeepEqual(before.Manga[id], m) {
			continue
		}
		raw, err := json.Marshal(m)
		if err != nil {
			return err
		}
		if err = exec("INSERT INTO manga(id,metadata) VALUES($1,$2::jsonb) ON CONFLICT(id) DO UPDATE SET metadata=EXCLUDED.metadata", id, string(raw)); err != nil {
			return err
		}
	}
	for token := range before.Sessions {
		if _, ok := next.Sessions[token]; !ok {
			if err := exec("DELETE FROM sessions WHERE token_hash=$1", token); err != nil {
				return err
			}
		}
	}
	for token, value := range next.Sessions {
		if reflect.DeepEqual(before.Sessions[token], value) {
			continue
		}
		if err := exec("INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3) ON CONFLICT(token_hash) DO UPDATE SET user_id=EXCLUDED.user_id,expires_at=EXCLUDED.expires_at", token, value.UserID, value.ExpiresAt); err != nil {
			return err
		}
	}
	for owner, entries := range before.Libraries {
		for id := range entries {
			if _, ok := next.Libraries[owner][id]; !ok {
				if err := exec("DELETE FROM library WHERE user_id=$1 AND manga_id=$2", owner, id); err != nil {
					return err
				}
			}
		}
	}
	for owner, entries := range next.Libraries {
		for id, l := range entries {
			old, exists := before.Libraries[owner][id]
			if !exists || old.Favorite != l.Favorite || old.ToRead != l.ToRead {
				if err := exec("INSERT INTO library(user_id,manga_id,to_read,favorite) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,manga_id) DO UPDATE SET to_read=EXCLUDED.to_read,favorite=EXCLUDED.favorite", owner, id, l.ToRead, l.Favorite); err != nil {
					return err
				}
			}
			oldChapters := map[string]Chapter{}
			newChapters := map[string]Chapter{}
			for _, c := range old.Chapters {
				oldChapters[c.Chapter] = c
			}
			for _, c := range l.Chapters {
				newChapters[c.Chapter] = c
			}
			for key := range oldChapters {
				if _, ok := newChapters[key]; !ok {
					if err := exec("DELETE FROM chapters WHERE user_id=$1 AND manga_id=$2 AND chapter=$3::numeric", owner, id, key); err != nil {
						return err
					}
				}
			}
			for key, c := range newChapters {
				if reflect.DeepEqual(oldChapters[key], c) {
					continue
				}
				if err := exec("INSERT INTO chapters(user_id,manga_id,chapter,is_read,read_at,rating,notes) VALUES($1,$2,$3::numeric,$4,$5,$6,$7) ON CONFLICT(user_id,manga_id,chapter) DO UPDATE SET is_read=EXCLUDED.is_read,read_at=EXCLUDED.read_at,rating=EXCLUDED.rating,notes=EXCLUDED.notes", owner, id, key, c.Read, c.ReadAt, c.Rating, c.Notes); err != nil {
					return err
				}
			}
		}
	}
	for id := range before.Lists {
		if _, ok := next.Lists[id]; !ok {
			if err := exec("DELETE FROM lists WHERE id=$1", id); err != nil {
				return err
			}
		}
	}
	for id, l := range next.Lists {
		old, exists := before.Lists[id]
		base := l
		base.ItemIDs = nil
		oldBase := old
		oldBase.ItemIDs = nil
		if !exists || !reflect.DeepEqual(base, oldBase) {
			if err := exec("INSERT INTO lists(id,owner_id,name,description,is_public,created_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT(id) DO UPDATE SET owner_id=EXCLUDED.owner_id,name=EXCLUDED.name,description=EXCLUDED.description,is_public=EXCLUDED.is_public", id, l.OwnerID, l.Name, l.Description, l.IsPublic, l.CreatedAt); err != nil {
				return err
			}
		}
		if !exists || !reflect.DeepEqual(old.ItemIDs, l.ItemIDs) {
			if err := exec("DELETE FROM list_items WHERE list_id=$1", id); err != nil {
				return err
			}
			for position, mangaID := range l.ItemIDs {
				if err := exec("INSERT INTO list_items(list_id,manga_id,position) VALUES($1,$2,$3)", id, mangaID, position); err != nil {
					return err
				}
			}
		}
	}
	for id := range before.Users {
		if _, ok := next.Users[id]; !ok {
			if err := exec("DELETE FROM users WHERE id=$1", id); err != nil {
				return err
			}
		}
	}
	for id := range before.Manga {
		if _, ok := next.Manga[id]; !ok {
			if err := exec("DELETE FROM manga WHERE id=$1", id); err != nil {
				return err
			}
		}
	}
	return nil
}

// ImportJSON is an explicit one-time operation. It only commits into an empty target
// database and never edits/deletes the source JSON file or changes existing passwords.
func (s *Store) ImportJSON(path string) error {
	if s.db == nil {
		return errors.New("import requires DATABASE_URL and the postgres build tag")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read legacy JSON: %w", err)
	}
	var snapshot State
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		return errors.New("legacy JSON is not valid")
	}
	if snapshot.Users == nil || snapshot.Sessions == nil || snapshot.Manga == nil || snapshot.Libraries == nil || snapshot.Lists == nil {
		return errors.New("legacy JSON has an invalid structure")
	}
	return s.Update(func(target *State) error {
		if !isEmpty(*target) {
			return errors.New("refusing import: PostgreSQL target already contains data")
		}
		if err := validateImport(snapshot); err != nil {
			return err
		}
		*target = snapshot
		return nil
	})
}
func isEmpty(st State) bool {
	return len(st.Users) == 0 && len(st.Sessions) == 0 && len(st.Manga) == 0 && len(st.Libraries) == 0 && len(st.Lists) == 0
}
func validateImport(st State) error {
	// Reject orphaned or malformed records before SQL; constraints are a final check.
	for id, u := range st.Users {
		if id != u.ID || len(u.Username) < 2 || len(u.Username) > 50 || u.PasswordHash == "" || u.Email == "" || u.Email != strings.ToLower(u.Email) {
			return errors.New("legacy user fields are invalid")
		}
	}
	for id, m := range st.Manga {
		if id < 1 || m.ID != id || strings.TrimSpace(m.Title) == "" {
			return errors.New("legacy manga metadata is invalid")
		}
	}
	for token, session := range st.Sessions {
		if len(token) != 64 || st.Users[session.UserID].ID == "" {
			return errors.New("invalid legacy session or orphaned user")
		}
	}
	for owner, entries := range st.Libraries {
		if st.Users[owner].ID == "" {
			return errors.New("legacy library has no user")
		}
		for id, l := range entries {
			if st.Manga[id].ID != id || l.MangaID != id {
				return errors.New("legacy library has invalid manga")
			}
			seen := map[float64]bool{}
			for _, c := range l.Chapters {
				number, err := strconv.ParseFloat(c.Chapter, 64)
				if err != nil || math.IsNaN(number) || math.IsInf(number, 0) || number <= 0 || number > 100000 || seen[number] || c.Read != (c.ReadAt != nil) || len(c.Notes) > 5000 || c.Rating != nil && (*c.Rating < 1 || *c.Rating > 5) {
					return errors.New("legacy chapter data is invalid")
				}
				seen[number] = true
			}
		}
	}
	for id, l := range st.Lists {
		if id != l.ID || len(l.Name) < 1 || len(l.Name) > 100 || len(l.Description) > 1000 {
			return errors.New("legacy list fields are invalid")
		}
		if st.Users[l.OwnerID].ID == "" {
			return errors.New("legacy list has no owner")
		}
		ids := append([]int(nil), l.ItemIDs...)
		sort.Ints(ids)
		for i, id := range ids {
			if st.Manga[id].ID != id || i > 0 && id == ids[i-1] {
				return errors.New("legacy list contains invalid or duplicate manga")
			}
		}
	}
	return nil
}
