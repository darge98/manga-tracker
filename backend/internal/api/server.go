package api

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"manga-track/backend/internal/auth"
	"manga-track/backend/internal/provider"
	"manga-track/backend/internal/repository"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Config struct {
	AllowedOrigins []string
	SecureCookie   bool
}
type rateEntry struct {
	count int
	start time.Time
}
type Server struct {
	Store     repository.Repository
	Catalog   provider.Catalog
	config    Config
	rateMu    sync.Mutex
	rates     map[string]rateEntry
	dummyHash string
}

func New(st repository.Repository, c provider.Catalog, cfg Config) *Server {
	dummy, _ := auth.Hash("dummy-password-unmatchable")
	return &Server{Store: st, Catalog: c, config: cfg, rates: map[string]rateEntry{}, dummyHash: dummy}
}

type apiError struct {
	status  int
	message string
}

func (e apiError) Error() string { return e.message }
func fail(n int, s string) error { return apiError{n, s} }
func write(w http.ResponseWriter, n int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(n)
	_ = json.NewEncoder(w).Encode(v)
}
func decode(w http.ResponseWriter, r *http.Request, v interface{}) error {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		return fail(415, "Use Content-Type: application/json")
	}
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return fail(400, "Invalid JSON body")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return fail(400, "Invalid JSON body")
	}
	return nil
}
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-store")
	o := r.Header.Get("Origin")
	allowed := o == ""
	for _, a := range s.config.AllowedOrigins {
		if a == o {
			allowed = true
		}
	}
	if !allowed {
		write(w, 403, map[string]string{"error": "Origin is not allowed"})
		return
	}
	if o != "" {
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" && o == "" {
		write(w, 403, map[string]string{"error": "Cross-site request denied"})
		return
	}
	if r.Method == "OPTIONS" {
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(204)
		return
	}
	if e := s.Store.Refresh(); e != nil {
		log.Print("database unavailable")
		write(w, 503, map[string]string{"error": "Database temporarily unavailable. Please try again."})
		return
	}
	e := s.route(w, r)
	if e != nil {
		var ae apiError
		if errors.As(e, &ae) {
			write(w, ae.status, map[string]string{"error": ae.message})
		} else if errors.Is(e, repository.ErrDatabaseUnavailable) {
			write(w, 503, map[string]string{"error": "Database temporarily unavailable. Please try again."})
		} else if errors.Is(e, provider.ErrNotFound) {
			write(w, 404, map[string]string{"error": "Manga not found"})
		} else if errors.Is(e, provider.ErrUnavailable) {
			write(w, 503, map[string]string{"error": "Catalog temporarily unavailable. Please try again."})
		} else {
			log.Printf("request failed: %v", e)
			write(w, 500, map[string]string{"error": "Unable to save or load data"})
		}
	}
}
func method(r *http.Request, m string) error {
	if r.Method != m {
		return fail(405, "Method not allowed")
	}
	return nil
}
func integer(v string) (int, error) {
	n, e := strconv.Atoi(v)
	if e != nil || n < 1 {
		return 0, fail(400, "Invalid manga ID")
	}
	return n, nil
}
func page(r *http.Request) int {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p < 1 {
		p = 1
	}
	if p > 1000 {
		p = 1000
	}
	return p
}
func (s *Server) user(r *http.Request) (repository.User, error) {
	c, e := r.Cookie("manga_session")
	if e != nil {
		return repository.User{}, fail(401, "Sign in to continue")
	}
	var u repository.User
	if viewErr := s.Store.View(func(st repository.State) {
		ss, ok := st.Sessions[auth.TokenHash(c.Value)]
		if ok && time.Now().Before(ss.ExpiresAt) {
			u = st.Users[ss.UserID]
		}
	}); viewErr != nil {
		return repository.User{}, viewErr
	}
	if u.ID == "" {
		return u, fail(401, "Sign in to continue")
	}
	return u, nil
}
func publicUser(u repository.User) interface{} {
	return map[string]string{"id": u.ID, "username": u.Username, "email": u.Email}
}
func (s *Server) setCookie(w http.ResponseWriter, t string, age int) {
	http.SetCookie(w, &http.Cookie{Name: "manga_session", Value: t, Path: "/", HttpOnly: true, Secure: s.config.SecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (s *Server) limited(r *http.Request) bool {
	s.rateMu.Lock()
	defer s.rateMu.Unlock()
	key := r.RemoteAddr
	if i := strings.LastIndex(key, ":"); i >= 0 {
		key = key[:i]
	}
	now := time.Now()
	e := s.rates[key]
	if now.Sub(e.start) > time.Minute {
		e = rateEntry{start: now}
	}
	e.count++
	s.rates[key] = e
	if len(s.rates) > 10000 {
		for k, v := range s.rates {
			if now.Sub(v.start) > time.Minute {
				delete(s.rates, k)
			}
		}
	}
	return e.count > 15
}
func (s *Server) session(w http.ResponseWriter, u repository.User) error {
	t, e := auth.Token()
	if e != nil {
		return e
	}
	if e = s.Store.Update(func(st *repository.State) error {
		for k, v := range st.Sessions {
			if time.Now().After(v.ExpiresAt) {
				delete(st.Sessions, k)
			}
		}
		st.Sessions[auth.TokenHash(t)] = repository.Session{UserID: u.ID, ExpiresAt: time.Now().Add(30 * 24 * time.Hour)}
		return nil
	}); e != nil {
		return e
	}
	s.setCookie(w, t, 30*24*3600)
	return nil
}
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request, register bool) error {
	if e := method(r, "POST"); e != nil {
		return e
	}
	if s.limited(r) {
		return fail(429, "Too many attempts. Try again in a minute.")
	}
	var b struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if e := decode(w, r, &b); e != nil {
		return e
	}
	b.Email = strings.ToLower(strings.TrimSpace(b.Email))
	if len(b.Password) > 256 {
		return fail(400, "Password is too long")
	}
	var u repository.User
	if register {
		b.Username = strings.TrimSpace(b.Username)
		parsed, e := mail.ParseAddress(b.Email)
		if e != nil || parsed.Address != b.Email || len(b.Email) > 254 || len(b.Username) < 2 || len(b.Username) > 50 || len(b.Password) < 10 {
			return fail(400, "Enter a valid email, username (2–50 characters) and password (at least 10 characters)")
		}
		id, e := auth.Token()
		if e != nil {
			return e
		}
		hash, e := auth.Hash(b.Password)
		if e != nil {
			return e
		}
		u = repository.User{ID: id[:24], Username: b.Username, Email: b.Email, PasswordHash: hash}
		if e = s.Store.Update(func(st *repository.State) error {
			for _, old := range st.Users {
				if old.Email == b.Email {
					return fail(409, "An account with this email already exists")
				}
			}
			st.Users[u.ID] = u
			return nil
		}); e != nil {
			return e
		}
	} else {
		if viewErr := s.Store.View(func(st repository.State) {
			for _, v := range st.Users {
				if v.Email == b.Email {
					u = v
					break
				}
			}
		}); viewErr != nil {
			return viewErr
		}
		hash := u.PasswordHash
		if hash == "" {
			hash = s.dummyHash
		}
		valid := auth.Verify(b.Password, hash)
		if !valid || u.ID == "" {
			return fail(401, "Email or password is incorrect")
		}
	}
	if e := s.session(w, u); e != nil {
		return e
	}
	write(w, 200, map[string]interface{}{"user": publicUser(u)})
	return nil
}
func (s *Server) remember(items []repository.Manga) error {
	return s.Store.Update(func(st *repository.State) error {
		for _, m := range items {
			st.Manga[m.ID] = m
		}
		return nil
	})
}
func (s *Server) ensureManga(r *http.Request, id int, m *repository.Manga) error {
	var found bool
	if viewErr := s.Store.View(func(st repository.State) { _, found = st.Manga[id] }); viewErr != nil {
		return viewErr
	}
	if found {
		return nil
	}
	item, e := s.Catalog.Detail(r.Context(), id)
	if e != nil {
		return e
	}
	return s.remember([]repository.Manga{item})
}
func (s *Server) route(w http.ResponseWriter, r *http.Request) error {
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return fail(404, "Endpoint not found")
	}
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/"), "/")
	p := strings.Split(path, "/")
	switch path {
	case "health":
		if e := method(r, "GET"); e != nil {
			return e
		}
		write(w, 200, map[string]string{"status": "ok", "provider": "jikan", "storage": s.Store.Backend()})
		return nil
	case "auth/register":
		return s.authenticate(w, r, true)
	case "auth/login":
		return s.authenticate(w, r, false)
	case "auth/me":
		if e := method(r, "GET"); e != nil {
			return e
		}
		u, e := s.user(r)
		if e != nil {
			return e
		}
		write(w, 200, map[string]interface{}{"user": publicUser(u)})
		return nil
	case "auth/logout":
		if e := method(r, "POST"); e != nil {
			return e
		}
		if c, e := r.Cookie("manga_session"); e == nil {
			if e = s.Store.Update(func(st *repository.State) error { delete(st.Sessions, auth.TokenHash(c.Value)); return nil }); e != nil {
				return e
			}
		}
		s.setCookie(w, "", -1)
		write(w, 200, map[string]bool{"ok": true})
		return nil
	case "manga", "manga/popular":
		if e := method(r, "GET"); e != nil {
			return e
		}
		var result provider.Page
		var e error
		if path == "manga/popular" {
			result, e = s.Catalog.Popular(r.Context(), page(r))
		} else {
			q := strings.TrimSpace(r.URL.Query().Get("q"))
			if len(q) > 200 {
				return fail(400, "Search is too long")
			}
			result, e = s.Catalog.Search(r.Context(), q, page(r))
		}
		if e != nil {
			return e
		}
		if e = s.remember(result.Items); e != nil {
			return e
		}
		write(w, 200, result)
		return nil
	case "dashboard":
		return s.dashboard(w, r)
	case "library":
		return s.libraryItems(w, r)
	case "lists":
		return s.lists(w, r)
	}
	if len(p) == 2 && p[0] == "manga" {
		if e := method(r, "GET"); e != nil {
			return e
		}
		id, e := integer(p[1])
		if e != nil {
			return e
		}
		m, e := s.Catalog.Detail(r.Context(), id)
		warning := ""
		if e != nil {
			if !errors.Is(e, provider.ErrUnavailable) {
				return e
			}
			var found bool
			if viewErr := s.Store.View(func(st repository.State) { m, found = st.Manga[id] }); viewErr != nil {
				return viewErr
			}
			if !found {
				return e
			}
			warning = "Catalog temporarily unavailable. Showing previously saved metadata."
		} else if e = s.remember([]repository.Manga{m}); e != nil {
			return e
		}
		write(w, 200, map[string]interface{}{"manga": m, "catalogWarning": warning})
		return nil
	}
	if len(p) >= 2 && p[0] == "library" {
		return s.library(w, r, p)
	}
	if len(p) >= 2 && p[0] == "lists" {
		return s.list(w, r, p, false)
	}
	if len(p) == 3 && p[0] == "public" && p[1] == "lists" {
		return s.list(w, r, p[1:], true)
	}
	return fail(404, "Endpoint not found")
}
