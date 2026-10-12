//go:build postgres

package repository

import (
	"encoding/json"
	"errors"
	"manga-track/backend/internal/auth"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func integrationStore(t *testing.T) (*Store, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires TEST_DATABASE_URL pointing to a dedicated EMPTY PostgreSQL database; not verified")
	}
	s, err := OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err = s.View(func(st State) {
		if !isEmpty(st) {
			t.Fatal("integration tests refuse a nonempty database; use a dedicated test database")
		}
	}); err != nil {
		t.Fatal(err)
	}
	return s, dsn
}
func cleanupIntegrationRecords(t *testing.T, s *Store) {
	t.Helper()
	t.Cleanup(func() {
		err := s.Update(func(st *State) error {
			for _, id := range []string{"a", "b"} {
				delete(st.Users, id)
				delete(st.Libraries, id)
			}
			for token, session := range st.Sessions {
				if session.UserID == "a" || session.UserID == "b" {
					delete(st.Sessions, token)
				}
			}
			for id, l := range st.Lists {
				if l.OwnerID == "a" || l.OwnerID == "b" {
					delete(st.Lists, id)
				}
			}
			for _, id := range []int{13, 2} {
				used := false
				for _, entries := range st.Libraries {
					if _, ok := entries[id]; ok {
						used = true
					}
				}
				for _, l := range st.Lists {
					for _, item := range l.ItemIDs {
						if item == id {
							used = true
						}
					}
				}
				if !used {
					delete(st.Manga, id)
				}
			}
			return nil
		})
		if err != nil {
			t.Error("cannot clean integration fixture:", err)
		}
	})
}
func TestPostgresPersistenceConstraintsAndConcurrentWriters(t *testing.T) {
	s, dsn := integrationStore(t)
	cleanupIntegrationRecords(t, s)
	now := time.Now().UTC()
	if err := s.Update(func(st *State) error {
		*st = legacyFixture()
		st.Users["b"] = User{ID: "b", Username: "Bob", Email: "bob@example.test", PasswordHash: "second-password-hash"}
		st.Sessions[auth.TokenHash("fixture-session")] = Session{UserID: "a", ExpiresAt: now.Add(time.Hour)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	other, err := OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	var wg sync.WaitGroup
	for i := 1; i <= 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target := s
			if i%2 == 0 {
				target = other
			}
			if err := target.Update(func(st *State) error {
				l := st.Libraries["a"][13]
				l.Chapters = append(l.Chapters, Chapter{Chapter: strconv.Itoa(i), Read: true, ReadAt: &now, Notes: "saved"})
				st.Libraries["a"][13] = l
				return nil
			}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	if err = other.View(func(st State) {
		if len(st.Libraries["a"][13].Chapters) != 13 {
			t.Fatal("lost concurrent chapter update")
		}
		if len(st.Libraries["b"]) != 0 {
			t.Fatal("feedback leaked across users")
		}
		if st.Users["a"].PasswordHash != "existing-password-hash" {
			t.Fatal("password hash changed")
		}
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.db.Exec("UPDATE chapters SET rating=6 WHERE user_id=$1", "a"); err == nil {
		t.Fatal("rating constraint missing")
	}
	if _, err = s.db.Exec("INSERT INTO list_items(list_id,manga_id,position) VALUES($1,$2,$3)", "list", 999, 1); err == nil {
		t.Fatal("membership foreign key missing")
	}
	if _, err = s.db.Exec("INSERT INTO manga(id,metadata) VALUES($1,$2::jsonb)", 2, `{}`); err == nil {
		t.Fatal("empty manga metadata constraint missing")
	}
	if err = s.Update(func(st *State) error {
		u := st.Users["a"]
		u.Username = "Changed"
		st.Users["a"] = u
		return errors.New("cancel")
	}); err == nil {
		t.Fatal("expected callback rollback")
	}
	if err = other.View(func(st State) {
		if st.Users["a"].Username != "Alice" {
			t.Fatal("rollback was published")
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.Update(func(st *State) error { delete(st.Sessions, auth.TokenHash("fixture-session")); return nil }); err != nil {
		t.Fatal(err)
	}
	if err = other.View(func(st State) {
		if len(st.Sessions) != 0 {
			t.Fatal("other process retained revoked session")
		}
	}); err != nil {
		t.Fatal(err)
	}
	other.Close()
	reconnected, err := OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	if err = reconnected.View(func(st State) {
		if len(st.Libraries["a"][13].Chapters) != 13 || len(st.Lists["list"].ItemIDs) != 1 {
			t.Fatal("data did not survive reconnect")
		}
	}); err != nil {
		t.Fatal(err)
	}
}
func TestPostgresLegacyImportIsAtomicAndPreservesSource(t *testing.T) {
	s, _ := integrationStore(t)
	cleanupIntegrationRecords(t, s)
	fixture := legacyFixture()
	raw, _ := json.Marshal(fixture)
	path := filepath.Join(t.TempDir(), "legacy.json")
	os.WriteFile(path, raw, 0400)
	if err := s.ImportJSON(path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != string(raw) {
		t.Fatal("source JSON was changed")
	}
	if err = s.ImportJSON(path); err == nil {
		t.Fatal("import into nonempty target was accepted")
	}
	if err = s.View(func(st State) {
		if st.Users["a"].PasswordHash != "existing-password-hash" || st.Libraries["a"][13].Chapters[0].Notes != "private" {
			t.Fatal("legacy credentials or feedback not preserved")
		}
	}); err != nil {
		t.Fatal(err)
	}
}
