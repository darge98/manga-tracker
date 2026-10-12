package api

import (
	"errors"
	"log"
	"manga-track/backend/internal/auth"
	"manga-track/backend/internal/repository"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

const (
	sessionCookie   = "manga_session"
	sessionLifetime = 30 * 24 * time.Hour
)

type credentials struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) decodeCredentials(w http.ResponseWriter, r *http.Request) (credentials, error) {
	var c credentials
	if err := decode(w, r, &c); err != nil {
		return c, err
	}
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	if len(c.Password) > 256 {
		return c, fail(http.StatusBadRequest, "password_too_long", "Password is too long")
	}
	return c, nil
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) error {
	c, err := s.decodeCredentials(w, r)
	if err != nil {
		return err
	}
	c.Username = strings.TrimSpace(c.Username)
	parsed, err := mail.ParseAddress(c.Email)
	if err != nil || parsed.Address != c.Email || len(c.Email) > 254 || len(c.Username) < 2 || len(c.Username) > 50 || !validPassword(c.Password) {
		return fail(http.StatusBadRequest, "invalid_registration", "Enter a valid email, username (2–50 characters) and password (at least 10 characters)")
	}
	id, err := auth.Token()
	if err != nil {
		return err
	}
	hash, err := auth.Hash(c.Password)
	if err != nil {
		return err
	}
	user := repository.User{ID: id[:24], Username: c.Username, Email: c.Email, PasswordHash: hash}
	if err = s.store.CreateUser(r.Context(), user); errors.Is(err, repository.ErrEmailTaken) {
		return fail(http.StatusConflict, "email_taken", "An account with this email already exists")
	} else if err != nil {
		return err
	}
	return s.startSession(w, r, user)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	c, err := s.decodeCredentials(w, r)
	if err != nil {
		return err
	}
	user, err := s.store.UserByEmail(r.Context(), c.Email)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	// Verify against a dummy hash for unknown emails so timing does not reveal accounts.
	hash := user.PasswordHash
	if hash == "" {
		hash = s.dummyHash
	}
	if !auth.Verify(c.Password, hash) || user.ID == "" {
		return fail(http.StatusUnauthorized, "invalid_credentials", "Email or password is incorrect")
	}
	return s.startSession(w, r, user)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user repository.User) error {
	token, err := auth.Token()
	if err != nil {
		return err
	}
	if err = s.store.CreateSession(r.Context(), auth.TokenHash(token), user.ID, time.Now().Add(sessionLifetime)); err != nil {
		return err
	}
	// Serverless instances can't run a periodic job, so expired sessions are
	// pruned here; the query uses the expiry index and is cheap.
	if _, err = s.store.DeleteExpiredSessions(r.Context()); err != nil {
		log.Printf("session cleanup failed: %v", err)
	}
	s.setCookie(w, token, int(sessionLifetime.Seconds()))
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
	return nil
}

func (s *Server) me(w http.ResponseWriter, _ *http.Request, user repository.User) error {
	writeJSON(w, http.StatusOK, map[string]any{"user": user})
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		if err = s.store.DeleteSession(r.Context(), auth.TokenHash(c.Value)); err != nil {
			return err
		}
	}
	s.setCookie(w, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func validPassword(password string) bool { return len(password) >= 10 && len(password) <= 256 }

// checkPassword guards sensitive account changes. A wrong password is 403, not
// 401: the session is still valid and the client must not sign out.
func checkPassword(user repository.User, password string) error {
	if len(password) > 256 || !auth.Verify(password, user.PasswordHash) {
		return fail(http.StatusForbidden, "wrong_password", "Current password is incorrect")
	}
	return nil
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, user repository.User) error {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if !validPassword(body.NewPassword) {
		return fail(http.StatusBadRequest, "invalid_new_password", "New password must be 10–256 characters")
	}
	if err := checkPassword(user, body.CurrentPassword); err != nil {
		return err
	}
	hash, err := auth.Hash(body.NewPassword)
	if err != nil {
		return err
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return fail(http.StatusUnauthorized, "unauthenticated", "Sign in to continue")
	}
	if err = s.store.UpdatePassword(r.Context(), user.ID, hash, auth.TokenHash(c.Value)); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (s *Server) deleteAccount(w http.ResponseWriter, r *http.Request, user repository.User) error {
	var body struct {
		Password string `json:"password"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if err := checkPassword(user, body.Password); err != nil {
		return err
	}
	if err := s.store.DeleteUser(r.Context(), user.ID); err != nil {
		return err
	}
	s.setCookie(w, "", -1)
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (s *Server) currentUser(r *http.Request) (repository.User, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return repository.User{}, fail(http.StatusUnauthorized, "unauthenticated", "Sign in to continue")
	}
	user, err := s.store.UserBySession(r.Context(), auth.TokenHash(c.Value))
	if errors.Is(err, repository.ErrNotFound) {
		return user, fail(http.StatusUnauthorized, "unauthenticated", "Sign in to continue")
	}
	return user, err
}

func (s *Server) setCookie(w http.ResponseWriter, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: s.config.SecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
