package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"manga-track/backend/internal/auth"
	"manga-track/backend/internal/provider"
	"manga-track/backend/internal/repository"
	"manga-track/backend/internal/testdb"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testOrigin = "http://localhost:4200"

type testCatalog struct{ offline bool }

func (c *testCatalog) Search(ctx context.Context, _ string, p int) (provider.Page, error) {
	return c.Popular(ctx, p)
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

func newServer(store *repository.Store, catalog provider.Catalog) *Server {
	return New(store, catalog, Config{AllowedOrigins: []string{testOrigin}})
}

func request(t *testing.T, s *Server, method, path, body string, cookie *http.Cookie, expected int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "http://localhost:8080/api/"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", testOrigin)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != expected {
		t.Fatalf("%s %s expected %d got %d %s", method, path, expected, w.Code, w.Body.String())
	}
	return w
}

func register(t *testing.T, s *Server, name string) *http.Cookie {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"email":"%s@example.test","password":"password-%s"}`, name, strings.ToLower(name), name)
	return request(t, s, "POST", "auth/register", body, nil, 200).Result().Cookies()[0]
}

func createList(t *testing.T, s *Server, cookie *http.Cookie, body string) string {
	t.Helper()
	var created struct {
		List struct {
			ID string `json:"id"`
		} `json:"list"`
	}
	if err := json.Unmarshal(request(t, s, "POST", "lists", body, cookie, 201).Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return created.List.ID
}

func TestAuthenticationAndSessions(t *testing.T) {
	t.Parallel()
	url := testdb.URL(t)
	s := newServer(testdb.OpenURL(t, url), &testCatalog{})
	alice := register(t, s, "Alice")
	if !alice.HttpOnly || alice.SameSite != http.SameSiteLaxMode {
		t.Fatal("session cookie must be HttpOnly and SameSite=Lax")
	}
	bob := register(t, s, "Bob")
	request(t, s, "POST", "auth/register", `{"username":"Dup","email":"ALICE@example.test","password":"password-dup"}`, nil, 409)
	me := request(t, s, "GET", "auth/me", "", alice, 200).Body.String()
	if !strings.Contains(me, "alice@example.test") || strings.Contains(me, "pbkdf2") || strings.Contains(me, "password") {
		t.Fatalf("auth/me must expose the user without the password hash: %s", me)
	}
	request(t, s, "GET", "library/13", "", nil, 401)
	request(t, s, "POST", "auth/login", `{"email":"alice@example.test","password":"wrong"}`, nil, 401)
	request(t, s, "POST", "auth/login", `{"email":"nobody@example.test","password":"wrong"}`, nil, 401)

	// A second backend process sees sessions and their revocation immediately.
	other := newServer(testdb.OpenURL(t, url), &testCatalog{})
	request(t, other, "GET", "auth/me", "", bob, 200)
	request(t, s, "POST", "auth/logout", `{}`, bob, 200)
	request(t, other, "GET", "auth/me", "", bob, 401)
	request(t, other, "GET", "auth/me", "", alice, 200)
	request(t, other, "POST", "auth/login", `{"email":"bob@example.test","password":"password-Bob"}`, nil, 200)
}

func TestLibraryTrackingAndPrivacy(t *testing.T) {
	t.Parallel()
	catalog := &testCatalog{}
	s := newServer(testdb.Open(t), catalog)
	alice, bob := register(t, s, "Alice"), register(t, s, "Bob")

	request(t, s, "PUT", "library/13/chapters/12.5", `{"read":true,"rating":5,"notes":"Great ending"}`, alice, 200)
	request(t, s, "PUT", "library/13/chapters/12.5", `{"read":true,"rating":2,"notes":"Bob note"}`, bob, 200)
	request(t, s, "PUT", "library/13/chapters/NaN", `{"read":true}`, alice, 400)
	request(t, s, "PUT", "library/13/chapters/1e3", `{"read":true}`, alice, 400)
	request(t, s, "PUT", "library/13/chapters/1", `{"read":true,"rating":6}`, alice, 400)
	request(t, s, "PUT", "library/13/chapters/1", `{"rating":3}`, alice, 400)
	if strings.Contains(request(t, s, "GET", "library/13", "", bob, 200).Body.String(), "Great ending") {
		t.Fatal("feedback leaked across users")
	}

	// "12.50" is the same chapter as "12.5"; editing keeps the first read time.
	var before, after repository.Library
	json.Unmarshal(request(t, s, "GET", "library/13", "", alice, 200).Body.Bytes(), &before)
	request(t, s, "PUT", "library/13/chapters/12.50", `{"read":true,"rating":4,"notes":"Updated"}`, alice, 200)
	json.Unmarshal(request(t, s, "GET", "library/13", "", alice, 200).Body.Bytes(), &after)
	if len(after.Chapters) != 1 || after.Chapters[0].Chapter != "12.5" || !after.Chapters[0].ReadAt.Equal(*before.Chapters[0].ReadAt) {
		t.Fatalf("chapter normalization or timestamp broken: %+v", after)
	}

	// Deleting is per user, canonicalizes the number and is idempotent.
	request(t, s, "PUT", "library/13/chapters/7", `{"read":true}`, alice, 200)
	request(t, s, "DELETE", "library/13/chapters/7.0", "", bob, 200)
	if !strings.Contains(request(t, s, "GET", "library/13", "", alice, 200).Body.String(), `"chapter":"7"`) {
		t.Fatal("another user deleted the chapter")
	}
	var afterDelete repository.Library
	json.Unmarshal(request(t, s, "DELETE", "library/13/chapters/7.0", "", alice, 200).Body.Bytes(), &afterDelete)
	if len(afterDelete.Chapters) != 1 || afterDelete.Chapters[0].Chapter != "12.5" {
		t.Fatalf("chapter not deleted: %+v", afterDelete)
	}
	request(t, s, "DELETE", "library/13/chapters/7", "", alice, 200)
	request(t, s, "DELETE", "library/13/chapters/abc", "", alice, 400)

	request(t, s, "PUT", "library/13", `{"favorite":true,"toRead":true}`, alice, 200)
	for _, kind := range []string{"favorites", "to-read"} {
		if !strings.Contains(request(t, s, "GET", "library?kind="+kind, "", alice, 200).Body.String(), "One Piece") {
			t.Fatalf("%s missing", kind)
		}
		if strings.Contains(request(t, s, "GET", "library?kind="+kind, "", bob, 200).Body.String(), "One Piece") {
			t.Fatalf("%s leaked", kind)
		}
	}
	request(t, s, "PUT", "library/13", `{"favorite":false}`, alice, 200)
	if strings.Contains(request(t, s, "GET", "library?kind=favorites", "", alice, 200).Body.String(), "One Piece") {
		t.Fatal("favorite not removed")
	}
	if !strings.Contains(request(t, s, "GET", "library?kind=to-read", "", alice, 200).Body.String(), "One Piece") {
		t.Fatal("updating one flag must not reset the other")
	}
	request(t, s, "GET", "library?kind=other", "", alice, 400)

	// Clients cannot supply catalog metadata.
	request(t, s, "PUT", "library/99", `{"favorite":true,"manga":{"id":99,"title":"Fake"}}`, alice, 400)

	catalog.offline = true
	if !strings.Contains(request(t, s, "GET", "manga/13", "", nil, 200).Body.String(), "previously saved metadata") {
		t.Fatal("detail should fall back to saved metadata")
	}
	request(t, s, "GET", "manga/777", "", nil, 503)
	request(t, s, "PUT", "library/100", `{"favorite":true}`, alice, 503)
	request(t, s, "PUT", "library/13/chapters/2", `{"read":true,"notes":"works offline"}`, alice, 200)
	request(t, s, "PUT", "library/13/chapters/2", `{"read":false,"rating":null,"notes":""}`, alice, 200)
	dash := request(t, s, "GET", "dashboard", "", alice, 200).Body.String()
	if !strings.Contains(dash, "Updated") || !strings.Contains(dash, "temporarily unavailable") || strings.Contains(dash, "Bob note") {
		t.Fatalf("offline dashboard must keep only the user's history: %s", dash)
	}
}

func TestDashboardShowsLatestReadPerManga(t *testing.T) {
	t.Parallel()
	store := testdb.Open(t)
	ctx := context.Background()
	if err := store.CreateUser(ctx, repository.User{ID: "reader", Username: "Reader", Email: "reader@example.test", PasswordHash: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, auth.TokenHash("session"), "reader", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	onePiece, berserk := repository.Manga{ID: 13, Title: "One Piece"}, repository.Manga{ID: 2, Title: "Berserk"}
	now := time.Now().UTC()
	reads := []struct {
		manga   repository.Manga
		chapter string
		at      time.Time
	}{{onePiece, "1", now.Add(-2 * time.Hour)}, {onePiece, "12.5", now}, {berserk, "3", now.Add(-time.Hour)}}
	for _, read := range reads {
		if _, err := store.SaveChapter(ctx, "reader", read.manga, repository.Chapter{Chapter: read.chapter, Read: true}, read.at); err != nil {
			t.Fatal(err)
		}
	}
	s := newServer(store, &testCatalog{offline: true})
	cookie := &http.Cookie{Name: "manga_session", Value: "session"}
	var result struct {
		Recent []repository.RecentRead `json:"recent"`
	}
	if err := json.Unmarshal(request(t, s, "GET", "dashboard", "", cookie, 200).Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Recent) != 2 || result.Recent[0].Manga.ID != 13 || result.Recent[0].Chapter != "12.5" || result.Recent[1].Manga.ID != 2 {
		t.Fatalf("dashboard should show the latest event per manga: %+v", result.Recent)
	}
}

func TestListsOwnershipAndPublicSharing(t *testing.T) {
	t.Parallel()
	s := newServer(testdb.Open(t), &testCatalog{})
	alice, bob := register(t, s, "Alice"), register(t, s, "Bob")
	request(t, s, "PUT", "library/13/chapters/1", `{"read":true,"rating":5,"notes":"Private note"}`, alice, 200)
	id := createList(t, s, alice, `{"name":"Hidden","description":"Mine","isPublic":false}`)
	request(t, s, "POST", "lists", `{"name":"  ","description":"","isPublic":false}`, alice, 400)

	request(t, s, "PUT", "lists/"+id+"/items/13", `{}`, alice, 200)
	if !strings.Contains(request(t, s, "PUT", "lists/"+id+"/items/13", `{}`, alice, 200).Body.String(), `"itemCount":1`) {
		t.Fatal("list should deduplicate additions")
	}
	request(t, s, "GET", "public/lists/"+id, "", nil, 404)
	for _, call := range []struct{ method, path, body string }{
		{"GET", "lists/" + id, ""},
		{"PATCH", "lists/" + id, `{"isPublic":true}`},
		{"PUT", "lists/" + id + "/items/13", `{}`},
		{"DELETE", "lists/" + id + "/items/13", ""},
		{"DELETE", "lists/" + id, ""},
	} {
		request(t, s, call.method, call.path, call.body, bob, 404)
	}
	if strings.Contains(request(t, s, "GET", "lists", "", bob, 200).Body.String(), "Hidden") {
		t.Fatal("list leaked into another user's lists")
	}

	request(t, s, "PATCH", "lists/"+id, `{"name":"Shared","isPublic":true}`, alice, 200)
	public := request(t, s, "GET", "public/lists/"+id, "", nil, 200).Body.String()
	if !strings.Contains(public, "Shared") || !strings.Contains(public, "One Piece") {
		t.Fatalf("public list incomplete: %s", public)
	}
	for _, secret := range []string{"password", "email", "ownerId", "notes", "rating", "readAt", "Private note"} {
		if strings.Contains(public, secret) {
			t.Fatalf("public list leaks %s", secret)
		}
	}
	request(t, s, "PATCH", "lists/"+id, `{"isPublic":false}`, alice, 200)
	request(t, s, "GET", "public/lists/"+id, "", nil, 404)
	request(t, s, "DELETE", "lists/"+id+"/items/13", "", alice, 200)
	if strings.Contains(request(t, s, "GET", "lists/"+id, "", alice, 200).Body.String(), "One Piece") {
		t.Fatal("item removal failed")
	}
	request(t, s, "DELETE", "lists/"+id, "", alice, 200)
	request(t, s, "GET", "lists/"+id, "", alice, 404)
}

func TestOriginBodyAndRouting(t *testing.T) {
	t.Parallel()
	s := newServer(testdb.Open(t), &testCatalog{})
	r := httptest.NewRequest("POST", "http://localhost/api/auth/register", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross origin accepted")
	}
	r = httptest.NewRequest("POST", "http://localhost/api/auth/register", strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "text/plain")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 415 {
		t.Fatal("non-JSON body accepted")
	}
	request(t, s, "POST", "auth/register", `{"username":"a","email":"bad","password":"short"}`, nil, 400)
	request(t, s, "POST", "auth/register", `{} {}`, nil, 400)
	request(t, s, "GET", "manga/abc", "", nil, 400)

	if !strings.Contains(request(t, s, "POST", "auth/register", `{"username":"Dup","email":"x","password":"short"}`, nil, 400).Body.String(), `"code":"invalid_registration"`) {
		t.Fatal("errors must carry a stable code")
	}
	w = request(t, s, "GET", "auth/login", "", nil, 405)
	if !strings.Contains(w.Body.String(), `"error"`) || w.Header().Get("Allow") == "" {
		t.Fatalf("405 must be JSON with an Allow header: %v %s", w.Header(), w.Body.String())
	}
	if !strings.Contains(request(t, s, "GET", "nope", "", nil, 404).Body.String(), "Endpoint not found") {
		t.Fatal("404 must be JSON")
	}
	if !strings.Contains(request(t, s, "GET", "health", "", nil, 200).Body.String(), `"provider":"myanimelist","status":"ok","storage":"postgres"`) {
		t.Fatal("health must report storage")
	}
}

func TestRateLimitsUseProxyClientIP(t *testing.T) {
	t.Parallel()
	store := testdb.Open(t)
	login := func(s *Server, ip string) int {
		r := httptest.NewRequest("POST", "http://localhost/api/auth/login", strings.NewReader(`{"email":"x@example.test","password":"wrong"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Real-IP", ip)
		r.RemoteAddr = "172.18.0.5:41000" // the reverse proxy
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	behindProxy := New(store, &testCatalog{}, Config{AllowedOrigins: []string{testOrigin}, TrustProxy: true})
	for range 15 {
		login(behindProxy, "203.0.113.1")
	}
	if code := login(behindProxy, "203.0.113.1"); code != 429 {
		t.Fatalf("16th attempt from one client: got %d", code)
	}
	if code := login(behindProxy, "203.0.113.2"); code != 401 {
		t.Fatalf("other clients must not share the limit: got %d", code)
	}

	// Without TrustProxy the header is ignored (it could be forged).
	direct := New(store, &testCatalog{}, Config{})
	for range 15 {
		login(direct, "203.0.113.3")
	}
	if code := login(direct, "203.0.113.4"); code != 429 {
		t.Fatalf("forged X-Real-IP bypassed the limit: got %d", code)
	}

	for range 60 {
		request(t, behindProxy, "GET", "manga?q=xyz", "", nil, 200)
	}
	request(t, behindProxy, "GET", "manga?q=xyz", "", nil, 429)
}

func TestDatabaseOutage(t *testing.T) {
	t.Parallel()
	store := testdb.Open(t)
	s := newServer(store, &testCatalog{})
	cookie := register(t, s, "Alice")
	store.Close()
	for _, path := range []string{"health", "auth/me", "public/lists/shared"} {
		if !strings.Contains(request(t, s, "GET", path, "", cookie, 503).Body.String(), "Database temporarily unavailable") {
			t.Fatalf("%s: outage error is not actionable", path)
		}
	}
}

type strictCatalog struct{ testCatalog }

func (c *strictCatalog) Search(_ context.Context, q string, _ int) (provider.Page, error) {
	if q == "" {
		panic("empty searches must use Popular")
	}
	return provider.Page{}, provider.ErrInvalidQuery
}

func TestSearchRejectionAndEmptyQuery(t *testing.T) {
	t.Parallel()
	s := newServer(testdb.Open(t), &strictCatalog{})
	for _, q := range []string{"ab", "%E9%80%B2%E6%92%83"} { // "ab", "進撃"
		if !strings.Contains(request(t, s, "GET", "manga?q="+q, "", nil, 400).Body.String(), `"code":"search_too_short"`) {
			t.Fatalf("%s: short search must be rejected before the catalog", q)
		}
	}
	if !strings.Contains(request(t, s, "GET", "manga?q=abc", "", nil, 400).Body.String(), `"code":"invalid_search"`) {
		t.Fatal("rejected search must return invalid_search")
	}
	if !strings.Contains(request(t, s, "GET", "manga?q=%20", "", nil, 200).Body.String(), "One Piece") {
		t.Fatal("empty search should return popular manga")
	}
}

func TestChangePassword(t *testing.T) {
	t.Parallel()
	s := newServer(testdb.Open(t), &testCatalog{})
	first := register(t, s, "Alice") // password-Alice
	second := request(t, s, "POST", "auth/login", `{"email":"alice@example.test","password":"password-Alice"}`, nil, 200).Result().Cookies()[0]

	w := request(t, s, "PUT", "auth/password", `{"currentPassword":"wrong-password","newPassword":"new-password-123"}`, first, 403)
	if !strings.Contains(w.Body.String(), `"code":"wrong_password"`) {
		t.Fatalf("wrong current password: %s", w.Body.String())
	}
	request(t, s, "GET", "auth/me", "", first, 200) // a wrong password must not end the session
	request(t, s, "PUT", "auth/password", `{"currentPassword":"password-Alice","newPassword":"short"}`, first, 400)
	request(t, s, "PUT", "auth/password", `{"currentPassword":"password-Alice","newPassword":"new-password-123"}`, nil, 401)

	request(t, s, "PUT", "auth/password", `{"currentPassword":"password-Alice","newPassword":"new-password-123"}`, first, 200)
	request(t, s, "GET", "auth/me", "", first, 200)  // the session that changed it stays
	request(t, s, "GET", "auth/me", "", second, 401) // every other session is closed
	request(t, s, "POST", "auth/login", `{"email":"alice@example.test","password":"password-Alice"}`, nil, 401)
	request(t, s, "POST", "auth/login", `{"email":"alice@example.test","password":"new-password-123"}`, nil, 200)
}

func TestDeleteAccount(t *testing.T) {
	t.Parallel()
	s := newServer(testdb.Open(t), &testCatalog{})
	alice, bob := register(t, s, "Alice"), register(t, s, "Bob")
	request(t, s, "PUT", "library/13/chapters/1", `{"read":true,"notes":"Alice note"}`, alice, 200)
	request(t, s, "PUT", "library/13/chapters/1", `{"read":true,"notes":"Bob note"}`, bob, 200)
	id := createList(t, s, alice, `{"name":"Shared","description":"","isPublic":true}`)
	request(t, s, "PUT", "lists/"+id+"/items/13", `{}`, alice, 200)
	request(t, s, "GET", "public/lists/"+id, "", nil, 200)

	request(t, s, "DELETE", "auth/account", `{"password":"wrong-password"}`, alice, 403)
	request(t, s, "GET", "auth/me", "", alice, 200)

	w := request(t, s, "DELETE", "auth/account", `{"password":"password-Alice"}`, alice, 200)
	if c := w.Result().Cookies(); len(c) != 1 || c[0].MaxAge >= 0 {
		t.Fatal("deleting the account must clear the session cookie")
	}
	request(t, s, "GET", "auth/me", "", alice, 401)
	request(t, s, "GET", "public/lists/"+id, "", nil, 404)
	request(t, s, "POST", "auth/login", `{"email":"alice@example.test","password":"password-Alice"}`, nil, 401)
	if !strings.Contains(request(t, s, "GET", "library/13", "", bob, 200).Body.String(), "Bob note") {
		t.Fatal("deleting one account must not touch other users")
	}
	register(t, s, "Alice") // the email can be used again
}

func TestSameOriginNeedsNoConfiguration(t *testing.T) {
	t.Parallel()
	s := New(testdb.Open(t), &testCatalog{}, Config{}) // no AllowedOrigins at all
	call := func(target, origin string) int {
		r := httptest.NewRequest("GET", target, nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w.Code
	}
	// Production domain, preview URL and local Docker all work unconfigured.
	for _, host := range []string{"https://manga.example.com", "https://manga-git-main-me.vercel.app", "http://127.0.0.1:8080"} {
		if code := call(host+"/api/health", host); code != 200 {
			t.Fatalf("same-origin request from %s rejected: %d", host, code)
		}
	}
	if code := call("https://manga.example.com/api/health", "https://evil.example"); code != 403 {
		t.Fatalf("cross-origin request accepted: %d", code)
	}
	if code := call("https://manga.example.com/api/health", "https://manga.example.com.evil.example"); code != 403 {
		t.Fatalf("look-alike origin accepted: %d", code)
	}
}

func TestLoginPrunesExpiredSessions(t *testing.T) {
	t.Parallel()
	store := testdb.Open(t)
	s := newServer(store, &testCatalog{})
	register(t, s, "Alice")
	ctx := context.Background()
	user, err := store.UserByEmail(ctx, "alice@example.test")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CreateSession(ctx, auth.TokenHash("old"), user.ID, time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	request(t, s, "POST", "auth/login", `{"email":"alice@example.test","password":"password-Alice"}`, nil, 200)
	if n, err := store.DeleteExpiredSessions(ctx); err != nil || n != 0 {
		t.Fatalf("login should have pruned the expired session (left %d, %v)", n, err)
	}
}

// countingCatalog records Detail calls, to check when the catalog is consulted.
type countingCatalog struct {
	testCatalog
	details int
}

func (c *countingCatalog) Detail(ctx context.Context, id int) (repository.Manga, error) {
	c.details++
	return c.testCatalog.Detail(ctx, id)
}

func TestBrowsingDoesNotStoreManga(t *testing.T) {
	t.Parallel()
	store := testdb.Open(t)
	catalog := &countingCatalog{}
	s := newServer(store, catalog)
	alice := register(t, s, "Alice")
	request(t, s, "GET", "manga/42", "", nil, 200)
	if _, err := store.Manga(context.Background(), 42); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("viewing a manga must not store it: %v", err)
	}
	// Only a referenced manga is stored, and offline its saved card data is served.
	request(t, s, "PUT", "library/42/chapters/1", `{"read":true}`, alice, 200)
	if _, err := store.Manga(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	// An empty chapter is a deletion and needs no catalog call.
	before := catalog.details
	request(t, s, "PUT", "library/42/chapters/1", `{"read":false}`, alice, 200)
	if catalog.details != before {
		t.Fatal("deleting through an empty chapter must not call the catalog")
	}
	if _, err := store.Manga(context.Background(), 42); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("the manga must be pruned once nothing references it")
	}
	// Turning a flag off on a manga never added needs no catalog call either.
	request(t, s, "PUT", "library/43", `{"favorite":false}`, alice, 200)
	if catalog.details != before {
		t.Fatal("turning a flag off must not call the catalog")
	}
}

func TestLimitsAndWriteRateLimit(t *testing.T) {
	t.Parallel()
	store := testdb.Open(t)
	store.Limits = repository.Limits{ChaptersPerUser: 1, LibraryPerUser: 10, ListsPerUser: 10, ItemsPerList: 10}
	s := newServer(store, &testCatalog{})
	alice := register(t, s, "Alice")
	request(t, s, "PUT", "library/13/chapters/1", `{"read":true}`, alice, 200)
	w := request(t, s, "PUT", "library/13/chapters/2", `{"read":true}`, alice, 403)
	if !strings.Contains(w.Body.String(), `"code":"limit_reached"`) {
		t.Fatalf("limit error: %s", w.Body.String())
	}

	s.writes = newRateLimiter(2, time.Minute)
	request(t, s, "PUT", "library/13", `{"favorite":true}`, alice, 200)
	request(t, s, "PUT", "library/13", `{"favorite":false}`, alice, 200)
	if !strings.Contains(request(t, s, "PUT", "library/13", `{"favorite":true}`, alice, 429).Body.String(), "rate_limited") {
		t.Fatal("writes beyond the limit must be rejected")
	}
	request(t, s, "GET", "library/13", "", alice, 200) // reads are not limited
}
