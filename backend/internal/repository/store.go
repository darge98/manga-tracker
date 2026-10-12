// Package repository persists users, sessions, catalog metadata, reading
// progress and lists in PostgreSQL. Every method runs targeted queries, so the
// cost of a request depends only on the data it touches.
package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound    = errors.New("record not found")
	ErrEmailTaken  = errors.New("email already registered")
	ErrUnavailable = errors.New("database temporarily unavailable")
)

type Store struct {
	pool *pgxpool.Pool
}

// querier is satisfied by both the pool and a transaction, so read helpers can
// run standalone or inside a write transaction.
type querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Open connects to PostgreSQL and applies pending migrations. Error messages
// never include the connection URL, which carries the password.
func Open(ctx context.Context, databaseURL string) (*Store, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, errors.New("DATABASE_URL must be a valid PostgreSQL connection URL")
	}
	if config.ConnConfig.ConnectTimeout == 0 {
		config.ConnConfig.ConnectTimeout = 10 * time.Second
	}
	if !strings.Contains(databaseURL, "pool_max_conns") {
		config.MaxConns = 10
	}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, errors.New("unable to configure PostgreSQL connection")
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL unavailable: check database readiness and DATABASE_URL")
	}
	if err = migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL schema migration failed: %w", err)
	}
	return &Store{pool: pool}, nil
}

func (s *Store) Close() { s.pool.Close() }

func (s *Store) Ping(ctx context.Context) error { return wrap(s.pool.Ping(ctx)) }

// inTx runs fn in a transaction, committing only if fn succeeds.
func (s *Store) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return wrap(err)
	}
	defer tx.Rollback(ctx)
	if err = fn(tx); err != nil {
		return err
	}
	return wrap(tx.Commit(ctx))
}

// wrap maps driver errors to the package's sentinel errors. Errors reported by
// the server (constraint violations, SQL bugs) are returned unchanged; anything
// else means the database could not be reached.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if errors.Is(err, ErrNotFound) || errors.Is(err, ErrEmailTaken) || errors.Is(err, ErrUnavailable) || errors.Is(err, context.Canceled) {
		return err
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
