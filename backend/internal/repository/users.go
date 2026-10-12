package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type User struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
}

// CreateUser returns ErrEmailTaken when the (lowercase) email is already registered.
func (s *Store) CreateUser(ctx context.Context, u User) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO users(id,username,email,password_hash) VALUES($1,$2,$3,$4)",
		u.ID, u.Username, u.Email, u.PasswordHash)
	if isUniqueViolation(err) {
		return ErrEmailTaken
	}
	return wrap(err)
}

func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx,
		"SELECT id,username,email,password_hash FROM users WHERE email=$1", email,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash)
	return u, wrap(err)
}

// CreateSession stores only the hash of the session token.
func (s *Store) CreateSession(ctx context.Context, tokenHash, userID string, expiresAt time.Time) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3)",
		tokenHash, userID, expiresAt)
	return wrap(err)
}

// UserBySession returns ErrNotFound for unknown or expired sessions.
func (s *Store) UserBySession(ctx context.Context, tokenHash string) (User, error) {
	var u User
	err := s.pool.QueryRow(ctx, `
		SELECT u.id,u.username,u.email,u.password_hash
		FROM sessions s JOIN users u ON u.id=s.user_id
		WHERE s.token_hash=$1 AND s.expires_at > now()`, tokenHash,
	).Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash)
	return u, wrap(err)
}

// UpdatePassword stores a new hash and signs out every other session of the
// user, keeping only keepSession (the one that made the change).
func (s *Store) UpdatePassword(ctx context.Context, userID, passwordHash, keepSession string) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE users SET password_hash=$2 WHERE id=$1", userID, passwordHash)
		if err != nil {
			return wrap(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		_, err = tx.Exec(ctx, "DELETE FROM sessions WHERE user_id=$1 AND token_hash<>$2", userID, keepSession)
		return wrap(err)
	})
}

// DeleteUser removes the account and, through ON DELETE CASCADE, its
// sessions, library, chapters and lists. Shared catalog metadata stays.
func (s *Store) DeleteUser(ctx context.Context, userID string) error {
	tag, err := s.pool.Exec(ctx, "DELETE FROM users WHERE id=$1", userID)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return wrap(err)
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", tokenHash)
	return wrap(err)
}

func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE expires_at <= now()")
	return tag.RowsAffected(), wrap(err)
}
