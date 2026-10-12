package repository

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// migrationLock serializes migrations when several backend processes start together.
const migrationLock int64 = 713120261011

// migrate applies, in version order, every migrations/NNN_name.sql file not yet
// recorded in schema_migrations. Applied files must never be edited: add a new
// numbered file instead.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLock); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "CREATE TABLE IF NOT EXISTS schema_migrations (version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())"); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	applied, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return err
	}
	done := map[int]bool{}
	for _, version := range applied {
		done[version] = true
	}

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	seen := map[int]string{}
	for _, entry := range entries { // ReadDir returns entries sorted by name
		name := entry.Name()
		prefix, _, ok := strings.Cut(name, "_")
		version, convErr := strconv.Atoi(prefix)
		if !ok || convErr != nil || version < 1 {
			return fmt.Errorf("migration %s must be named NNN_description.sql", name)
		}
		if previous, duplicate := seen[version]; duplicate {
			return fmt.Errorf("migrations %s and %s share version %d", previous, name, version)
		}
		seen[version] = name
		if done[version] {
			continue
		}
		script, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, string(script)); err != nil {
			return fmt.Errorf("applying %s: %w", name, err)
		}
		if _, err = tx.Exec(ctx, "INSERT INTO schema_migrations(version) VALUES($1) ON CONFLICT DO NOTHING", version); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
