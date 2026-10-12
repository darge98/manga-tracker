package api

import (
	"context"
	"encoding/json"
	"manga-track/backend/internal/auth"
	"manga-track/backend/internal/provider"
	"manga-track/backend/internal/repository"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testCatalog struct{ offline bool }

func (c *testCatalog) Search(_ context.Context, _ string, p int) (provider.Page, error) {
	return c.Popular(context.Background(), p)
}
func (c *testCatalog) Popular(_ context.Context, p int) (provider.Page, error) {
	if c.offline {
		return provider.Page{}, provider.ErrUnavailable
	}
	return provider.Page{Items: []repository.Manga{{ID: 13, Title: "One Piece", Genres: []string{"Adventure"}}}, Page: p}, nil
}
func (c *testCatalog) Detail(_ context.Context, id int) (repository.Manga, error) {
	if c.offline {
		return repository.Manga{}, provider.ErrUnavailable
	}
	return repository.Manga{ID: id, Title: "One Piece", Genres: []string{}}, nil
}
func request(t *testing.T, s *Server, m, path, body string, cookie *http.Cookie, expected int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(m, "http://localhost:8080/api/"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "http://localhost:4200")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != expected {
		t.Fatalf("%s %s expected %d got %d %s", m, path, expected, w.Code, w.Body.String())
	}
	return w
}
func TestAuthenticationPrivacyTrackingAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	store, e := repository.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	cat := &testCatalog{}
	s := New(store, cat, Config{AllowedOrigins: []string{"http://localhost:4200"}})
	w := request(t, s, "POST", "auth/register", `{"username":"Alice","email":"alice@example.com","password":"secure-pass-123"}`, nil, 200)
	alice := w.Result().Cookies()[0]
	if !alice.HttpOnly || alice.SameSite != http.SameSiteLaxMode {
		t.Fatal("cookie flags")
	}
	w = request(t, s, "POST", "auth/register", `{"username":"Bob","email":"bob@example.com","password":"secure-pass-456"}`, nil, 200)
	bob := w.Result().Cookies()[0]
	request(t, s, "GET", "auth/me", "", alice, 200)
	request(t, s, "GET", "library/13", "", nil, 401)
	request(t, s, "POST", "auth/login", `{"email":"alice@example.com","password":"wrong"}`, nil, 401)
	request(t, s, "GET", "manga?q=one", "", nil, 200)
	request(t, s, "PUT", "library/13/chapters/12.5", `{"read":true,"rating":5,"notes":"Great ending"}`, alice, 200)
	request(t, s, "PUT", "library/13/chapters/NaN", `{"read":true}`, alice, 400)
	request(t, s, "PUT", "library/13/chapters/1", `{"read":true,"rating":6}`, alice, 400)
	w = request(t, s, "GET", "library/13", "", bob, 200)
	if strings.Contains(w.Body.String(), "Great ending") {
		t.Fatal("feedback leaked")
	}
	request(t, s, "PUT", "library/13", `{"favorite":true,"toRead":true}`, alice, 200)
	w = request(t, s, "GET", "library?kind=favorites", "", alice, 200)
	if !strings.Contains(w.Body.String(), "One Piece") {
		t.Fatal("favorite missing")
	}
	w = request(t, s, "GET", "library?kind=favorites", "", bob, 200)
	if strings.Contains(w.Body.String(), "One Piece") {
		t.Fatal("favorite leaked")
	}
	w = request(t, s, "POST", "lists", `{"name":"Hidden","description":"Mine","isPublic":false}`, alice, 201)
	var created struct {
		List struct {
			ID string `json:"id"`
		} `json:"list"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)
	id := created.List.ID
	request(t, s, "PUT", "lists/"+id+"/items/13", `{}`, alice, 200)
	request(t, s, "GET", "public/lists/"+id, "", nil, 404)
	request(t, s, "GET", "lists/"+id, "", bob, 404)
	request(t, s, "PATCH", "lists/"+id, `{"isPublic":true}`, bob, 404)
	request(t, s, "DELETE", "lists/"+id, "", bob, 404)
	request(t, s, "PATCH", "lists/"+id, `{"isPublic":true}`, alice, 200)
	w = request(t, s, "GET", "public/lists/"+id, "", nil, 200)
	if strings.Contains(w.Body.String(), "Great ending") || strings.Contains(w.Body.String(), "alice@example") {
		t.Fatal("private fields exposed in public list")
	}
	request(t, s, "PATCH", "lists/"+id, `{"isPublic":false}`, alice, 200)
	request(t, s, "GET", "public/lists/"+id, "", nil, 404)
	// A client-supplied snapshot cannot poison the shared catalog or bypass upstream failure.
	request(t, s, "PUT", "library/99", `{"favorite":true,"manga":{"id":99,"title":"Fake client title"}}`, alice, 200)
	w = request(t, s, "GET", "library?kind=favorites", "", alice, 200)
	if strings.Contains(w.Body.String(), "Fake client title") {
		t.Fatal("client metadata entered global catalog")
	}
	// Canonical decimal ID updates one existing chapter rather than duplicating it.
	w = request(t, s, "GET", "library/13", "", alice, 200)
	var before repository.Library
	json.Unmarshal(w.Body.Bytes(), &before)
	request(t, s, "PUT", "library/13/chapters/12.50", `{"read":true,"rating":4,"notes":"Updated feedback"}`, alice, 200)
	w = request(t, s, "GET", "library/13", "", alice, 200)
	var after repository.Library
	json.Unmarshal(w.Body.Bytes(), &after)
	if len(after.Chapters) != 1 || !after.Chapters[0].ReadAt.Equal(*before.Chapters[0].ReadAt) {
		t.Fatal("feedback changed timestamp or chapter normalization failed")
	}
	request(t, s, "PUT", "library/13/chapters/12.5", `{"read":true,"rating":5,"notes":"Great ending"}`, alice, 200)

	cat.offline = true
	w = request(t, s, "GET", "manga/13", "", nil, 200)
	if !strings.Contains(w.Body.String(), "previously saved metadata") {
		t.Fatal("detail should remain available offline")
	}
	request(t, s, "GET", "manga/777", "", nil, 503)
	request(t, s, "PUT", "library/100", `{"favorite":true,"manga":{"id":100,"title":"Offline fake"}}`, alice, 503)
	request(t, s, "PUT", "library/13/chapters/2", `{"read":true,"rating":3,"notes":"Existing metadata works offline"}`, alice, 200)
	request(t, s, "PUT", "library/13/chapters/2", `{"read":false,"rating":null,"notes":""}`, alice, 200)
	w = request(t, s, "GET", "dashboard", "", alice, 200)
	if !strings.Contains(w.Body.String(), "Great ending") || !strings.Contains(w.Body.String(), "temporarily unavailable") {
		t.Fatal("offline dashboard should preserve history")
	}
	store.Close()
	store, e = repository.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	s = New(store, cat, Config{AllowedOrigins: []string{"http://localhost:4200"}})
	request(t, s, "GET", "auth/me", "", alice, 200)
	w = request(t, s, "GET", "library/13", "", alice, 200)
	if !strings.Contains(w.Body.String(), "12.5") || !strings.Contains(w.Body.String(), "Great ending") {
		t.Fatal("tracking not persisted")
	}
	store.View(func(st repository.State) {
		for key := range st.Sessions {
			if key == alice.Value {
				t.Fatal("raw session stored")
			}
		}
		for _, u := range st.Users {
			if strings.Contains(u.PasswordHash, "secure-pass") {
				t.Fatal("plaintext password")
			}
		}
	})
	request(t, s, "POST", "auth/logout", `{}`, alice, 200)
	request(t, s, "GET", "auth/me", "", alice, 401)
	request(t, s, "POST", "auth/login", `{"email":"alice@example.com","password":"secure-pass-123"}`, nil, 200)
}
func TestOriginBodyAndMethodProtection(t *testing.T) {
	st, e := repository.Open(filepath.Join(t.TempDir(), "db.json"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	s := New(st, &testCatalog{}, Config{AllowedOrigins: []string{"http://localhost:4200"}})
	r := httptest.NewRequest("POST", "http://localhost/api/auth/register", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin accepted")
	}
	request(t, s, "GET", "auth/login", "", nil, 405)
	request(t, s, "POST", "auth/register", `{"username":"a","email":"bad","password":"short"}`, nil, 400)
	request(t, s, "POST", "auth/register", `{} {}`, nil, 400)
	r = httptest.NewRequest("POST", "http://localhost/api/auth/register", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("simple cross-site body accepted")
	}
}

func TestDashboardLatestDistinctManga(t *testing.T) {
	st, e := repository.Open(filepath.Join(t.TempDir(), "db.json"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	now := time.Now().UTC()
	older := now.Add(-time.Hour)
	oldest := now.Add(-2 * time.Hour)
	e = st.Update(func(state *repository.State) error {
		state.Users["reader"] = repository.User{ID: "reader", Username: "Reader"}
		state.Sessions[auth.TokenHash("session")] = repository.Session{UserID: "reader", ExpiresAt: now.Add(time.Hour)}
		state.Manga[13] = repository.Manga{ID: 13, Title: "One Piece"}
		state.Manga[2] = repository.Manga{ID: 2, Title: "Berserk"}
		state.Libraries["reader"] = map[int]repository.Library{
			13: {MangaID: 13, Chapters: []repository.Chapter{{Chapter: "1", Read: true, ReadAt: &oldest}, {Chapter: "12.5", Read: true, ReadAt: &now}}},
			2:  {MangaID: 2, Chapters: []repository.Chapter{{Chapter: "3", Read: true, ReadAt: &older}}},
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	s := New(st, &testCatalog{offline: true}, Config{AllowedOrigins: []string{"http://localhost:4200"}})
	w := request(t, s, "GET", "dashboard", "", &http.Cookie{Name: "manga_session", Value: "session"}, 200)
	var result struct {
		Recent []recent `json:"recent"`
	}
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil {
		t.Fatal(e)
	}
	if len(result.Recent) != 2 || result.Recent[0].Manga.ID != 13 || result.Recent[0].Chapter != "12.5" || result.Recent[1].Manga.ID != 2 {
		t.Fatalf("dashboard should show latest event per manga: %+v", result.Recent)
	}
	w = request(t, s, "GET", "library/13", "", &http.Cookie{Name: "manga_session", Value: "session"}, 200)
	var library repository.Library
	json.Unmarshal(w.Body.Bytes(), &library)
	if len(library.Chapters) != 2 {
		t.Fatal("dashboard grouping should preserve full chapter history")
	}
}
