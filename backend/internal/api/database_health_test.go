package api

import (
	"manga-track/backend/internal/repository"
	"path/filepath"
	"strings"
	"testing"
)

type unavailableStore struct{ repository.Repository }

func (s unavailableStore) Refresh() error  { return repository.ErrDatabaseUnavailable }
func (s unavailableStore) Backend() string { return "postgres" }
func TestDatabaseOutageFailsHealthAndPrivateReads(t *testing.T) {
	st, err := repository.Open(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	s := New(unavailableStore{st}, &testCatalog{}, Config{AllowedOrigins: []string{"http://localhost:4200"}})
	for _, path := range []string{"health", "auth/me", "public/lists/shared"} {
		w := request(t, s, "GET", path, "", nil, 503)
		if !strings.Contains(w.Body.String(), "Database temporarily unavailable") {
			t.Fatal("outage error is not actionable")
		}
	}
}
