package repository

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPostgresConfigurationNeverFallsBackToJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unexpected.json")
	store, err := OpenConfigured("not-a-database-url", path)
	if err == nil {
		store.Close()
		t.Fatal("invalid database configuration was accepted")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("PostgreSQL configuration unexpectedly touched the JSON store")
	}
	if strings.Contains(errString(err), "not-a-database-url") {
		t.Fatal("connection URL leaked into error")
	}
}
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
func legacyFixture() State {
	st := empty()
	now := time.Now().UTC()
	rating := 5
	st.Users["a"] = User{ID: "a", Username: "Alice", Email: "alice@example.test", PasswordHash: "existing-password-hash"}
	st.Manga[13] = Manga{ID: 13, Title: "One Piece", Genres: []string{}}
	st.Libraries["a"] = map[int]Library{13: {MangaID: 13, Chapters: []Chapter{{Chapter: "12.5", Read: true, ReadAt: &now, Rating: &rating, Notes: "private"}}}}
	st.Lists["list"] = List{ID: "list", OwnerID: "a", Name: "Reading", CreatedAt: now, ItemIDs: []int{13}}
	return st
}
func TestLegacyImportValidationRejectsOrphansAndInvalidChapters(t *testing.T) {
	if err := validateImport(legacyFixture()); err != nil {
		t.Fatal(err)
	}
	st := legacyFixture()
	st.Lists["list"] = List{ID: "list", OwnerID: "unknown", Name: "List", ItemIDs: []int{13}}
	if validateImport(st) == nil {
		t.Fatal("orphaned owner accepted")
	}
	st = legacyFixture()
	l := st.Libraries["a"][13]
	l.Chapters = append(l.Chapters, l.Chapters[0])
	st.Libraries["a"][13] = l
	if validateImport(st) == nil {
		t.Fatal("duplicate chapters accepted")
	}
	st = legacyFixture()
	l = st.Libraries["a"][13]
	l.Chapters[0].Read = false
	st.Libraries["a"][13] = l
	if validateImport(st) == nil {
		t.Fatal("unread chapter with read timestamp accepted")
	}
	st = legacyFixture()
	st.Manga[13] = Manga{ID: 14, Title: "Mismatch"}
	if validateImport(st) == nil {
		t.Fatal("metadata ID mismatch accepted")
	}
}
