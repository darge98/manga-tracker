// Package testdb gives each test its own PostgreSQL schema, so tests can run
// in parallel against one database without seeing each other's data.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"manga-track/backend/internal/repository"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// URL returns a connection URL bound to a fresh, migrated-on-open schema that
// is dropped when the test ends. Without TEST_DATABASE_URL the test is skipped.
func URL(t testing.TB) string {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		// In CI a missing database is a configuration error, not a reason to skip.
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL must be set in CI")
		}
		t.Skip("TEST_DATABASE_URL not set; run: docker compose --profile test run --rm --build backend-test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("cannot connect to TEST_DATABASE_URL: %v", err)
	}
	defer conn.Close(ctx)
	suffix := make([]byte, 6)
	_, _ = rand.Read(suffix)
	schema := "test_" + hex.EncodeToString(suffix)
	if _, err = conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if conn, err := pgx.Connect(ctx, base); err == nil {
			_, _ = conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
			conn.Close(ctx)
		}
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema) // pgx forwards unknown parameters as session settings
	u.RawQuery = q.Encode()
	return u.String()
}

// Open returns a migrated store on a fresh schema, closed when the test ends.
func Open(t testing.TB) *repository.Store {
	t.Helper()
	return OpenURL(t, URL(t))
}

// OpenURL opens another store on an existing test schema, e.g. to simulate a
// second backend process.
func OpenURL(t testing.TB, databaseURL string) *repository.Store {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := repository.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	return store
}
