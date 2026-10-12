// Package api implements the JSON HTTP API consumed by the Angular frontend.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"manga-track/backend/internal/auth"
	"manga-track/backend/internal/provider"
	"manga-track/backend/internal/repository"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
)

const requestTimeout = 30 * time.Second

type Config struct {
	AllowedOrigins []string
	SecureCookie   bool
	// TrustProxy makes the rate limiters use the X-Real-IP header set by the
	// reverse proxy. Enable it only when the API is reachable solely through it.
	TrustProxy bool
}

type Server struct {
	store     *repository.Store
	catalog   provider.Catalog
	config    Config
	mux       *http.ServeMux
	authLimit *rateLimiter
	browse    *rateLimiter
	dummyHash string
}

func New(store *repository.Store, catalog provider.Catalog, config Config) *Server {
	dummy, _ := auth.Hash("dummy-password-unmatchable")
	s := &Server{
		store:     store,
		catalog:   catalog,
		config:    config,
		mux:       http.NewServeMux(),
		authLimit: newRateLimiter(15, time.Minute),
		browse:    newRateLimiter(60, time.Minute),
		dummyHash: dummy,
	}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.Handle("GET /api/health", s.handle(s.health))

	s.mux.Handle("POST /api/auth/register", s.limit(s.authLimit, s.register))
	s.mux.Handle("POST /api/auth/login", s.limit(s.authLimit, s.login))
	s.mux.Handle("GET /api/auth/me", s.authed(s.me))
	s.mux.Handle("POST /api/auth/logout", s.handle(s.logout))

	s.mux.Handle("GET /api/manga", s.limit(s.browse, s.search))
	s.mux.Handle("GET /api/manga/popular", s.limit(s.browse, s.popular))
	s.mux.Handle("GET /api/manga/{id}", s.limit(s.browse, s.mangaDetail))

	s.mux.Handle("GET /api/dashboard", s.authed(s.dashboard))
	s.mux.Handle("GET /api/library", s.authed(s.libraryItems))
	s.mux.Handle("GET /api/library/{id}", s.authed(s.getLibrary))
	s.mux.Handle("PUT /api/library/{id}", s.authed(s.setLibraryFlags))
	s.mux.Handle("PUT /api/library/{id}/chapters/{chapter}", s.authed(s.saveChapter))
	s.mux.Handle("DELETE /api/library/{id}/chapters/{chapter}", s.authed(s.deleteChapter))

	s.mux.Handle("GET /api/lists", s.authed(s.ownLists))
	s.mux.Handle("POST /api/lists", s.authed(s.createList))
	s.mux.Handle("GET /api/lists/{id}", s.authed(s.getList))
	s.mux.Handle("PATCH /api/lists/{id}", s.authed(s.updateList))
	s.mux.Handle("DELETE /api/lists/{id}", s.authed(s.deleteList))
	s.mux.Handle("PUT /api/lists/{id}/items/{mangaId}", s.authed(s.addListItem))
	s.mux.Handle("DELETE /api/lists/{id}/items/{mangaId}", s.authed(s.removeListItem))
	s.mux.Handle("GET /api/public/lists/{id}", s.limit(s.browse, s.publicList))
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	origin := r.Header.Get("Origin")
	if origin != "" && !slices.Contains(s.config.AllowedOrigins, origin) {
		writeJSON(w, http.StatusForbidden, errorBody("origin_not_allowed", "Origin is not allowed"))
		return
	}
	if origin != "" {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
	} else if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		writeJSON(w, http.StatusForbidden, errorBody("cross_site_denied", "Cross-site request denied"))
		return
	}
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if _, pattern := s.mux.Handler(r); pattern == "" {
		s.notRouted(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()
	s.mux.ServeHTTP(w, r.WithContext(ctx))
}

// notRouted turns the mux's plain-text 404/405 replies into JSON errors.
func (s *Server) notRouted(w http.ResponseWriter, r *http.Request) {
	h, _ := s.mux.Handler(r)
	probe := &statusProbe{header: http.Header{}}
	h.ServeHTTP(probe, r)
	if probe.status == http.StatusMethodNotAllowed {
		w.Header().Set("Allow", probe.header.Get("Allow"))
		writeJSON(w, http.StatusMethodNotAllowed, errorBody("method_not_allowed", "Method not allowed"))
		return
	}
	writeJSON(w, http.StatusNotFound, errorBody("endpoint_not_found", "Endpoint not found"))
}

type statusProbe struct {
	header http.Header
	status int
}

func (p *statusProbe) Header() http.Header         { return p.header }
func (p *statusProbe) Write(b []byte) (int, error) { return len(b), nil }
func (p *statusProbe) WriteHeader(status int)      { p.status = status }

// Handlers return an error instead of writing failures themselves; writeError
// maps it to a status code and a user-facing message.
type handlerFunc func(http.ResponseWriter, *http.Request) error
type authedFunc func(http.ResponseWriter, *http.Request, repository.User) error

func (s *Server) handle(fn handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			writeError(w, err)
		}
	})
}

func (s *Server) authed(fn authedFunc) http.Handler {
	return s.handle(func(w http.ResponseWriter, r *http.Request) error {
		user, err := s.currentUser(r)
		if err != nil {
			return err
		}
		return fn(w, r, user)
	})
}

func (s *Server) limit(limiter *rateLimiter, fn handlerFunc) http.Handler {
	return s.handle(func(w http.ResponseWriter, r *http.Request) error {
		if !limiter.allow(s.clientIP(r)) {
			return fail(http.StatusTooManyRequests, "rate_limited", "Too many requests. Try again in a minute.")
		}
		return fn(w, r)
	})
}

func (s *Server) clientIP(r *http.Request) string {
	if s.config.TrustProxy {
		if ip := strings.TrimSpace(r.Header.Get("X-Real-IP")); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) error {
	if err := s.store.Ping(r.Context()); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "provider": "jikan", "storage": "postgres"})
	return nil
}

// apiError carries a stable machine-readable code, which clients translate,
// and an English message for API consumers.
type apiError struct {
	status  int
	code    string
	message string
}

func (e apiError) Error() string { return e.message }

func fail(status int, code, message string) error { return apiError{status, code, message} }

func errorBody(code, message string) map[string]string {
	return map[string]string{"code": code, "error": message}
}

func writeError(w http.ResponseWriter, err error) {
	var ae apiError
	switch {
	case errors.As(err, &ae):
		writeJSON(w, ae.status, errorBody(ae.code, ae.message))
	case errors.Is(err, repository.ErrUnavailable):
		log.Printf("database unavailable: %v", err)
		writeJSON(w, http.StatusServiceUnavailable, errorBody("database_unavailable", "Database temporarily unavailable. Please try again."))
	case errors.Is(err, repository.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("not_found", "Not found"))
	case errors.Is(err, provider.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errorBody("manga_not_found", "Manga not found"))
	case errors.Is(err, provider.ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, errorBody("catalog_unavailable", "Catalog temporarily unavailable. Please try again."))
	default:
		log.Printf("request failed: %v", err)
		writeJSON(w, http.StatusInternalServerError, errorBody("internal_error", "Unable to save or load data"))
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decode accepts exactly one JSON object with known fields. Requiring the JSON
// content type also blocks cross-site HTML form submissions.
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fail(http.StatusUnsupportedMediaType, "unsupported_media_type", "Use Content-Type: application/json")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return fail(http.StatusBadRequest, "invalid_json", "Invalid JSON body")
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return fail(http.StatusBadRequest, "invalid_json", "Invalid JSON body")
	}
	return nil
}

func mangaID(r *http.Request, name string) (int, error) {
	n, err := strconv.Atoi(r.PathValue(name))
	if err != nil || n < 1 {
		return 0, fail(http.StatusBadRequest, "invalid_manga_id", "Invalid manga ID")
	}
	return n, nil
}
