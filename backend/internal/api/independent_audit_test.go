package api

import (
	"encoding/json"
	"manga-track/backend/internal/repository"
	"path/filepath"
	"strings"
	"testing"
)

// This test exercises the actual HTTP handlers and disk store without a network
// listener. Catalog data is explicitly a test fixture, not a live provider proof.
func TestIndependentBusinessIsolationListsAndUnknownChapters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.json")
	store, err := repository.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	s := New(store, &testCatalog{}, Config{AllowedOrigins: []string{"http://localhost:4200"}})
	a := request(t, s, "POST", "auth/register", `{"username":"Audit A","email":"audit-a@example.test","password":"audit-password-123"}`, nil, 200).Result().Cookies()[0]
	b := request(t, s, "POST", "auth/register", `{"username":"Audit B","email":"audit-b@example.test","password":"audit-password-456"}`, nil, 200).Result().Cookies()[0]
	request(t, s, "POST", "auth/register", `{"username":"Duplicate","email":"AUDIT-A@example.test","password":"audit-password-123"}`, nil, 409)
	request(t, s, "GET", "manga?q=unknown&page=2", "", nil, 200)
	request(t, s, "PUT", "library/13/chapters/1234.5", `{"read":true,"rating":5,"notes":"Private A chapter note"}`, a, 200)
	request(t, s, "PUT", "library/13/chapters/1234.5", `{"read":true,"rating":2,"notes":"Private B chapter note"}`, b, 200)
	var la, lb repository.Library
	json.Unmarshal(request(t, s, "GET", "library/13", "", a, 200).Body.Bytes(), &la)
	json.Unmarshal(request(t, s, "GET", "library/13", "", b, 200).Body.Bytes(), &lb)
	if len(la.Chapters) != 1 || len(lb.Chapters) != 1 || la.Chapters[0].Notes == lb.Chapters[0].Notes || *la.Chapters[0].Rating != 5 || *lb.Chapters[0].Rating != 2 {
		t.Fatal("chapter feedback is not independent per user")
	}
	request(t, s, "PUT", "library/13/chapters/1234.5", `{"read":false,"rating":4,"notes":"A retained unread feedback"}`, a, 200)
	json.Unmarshal(request(t, s, "GET", "library/13", "", a, 200).Body.Bytes(), &la)
	if la.Chapters[0].Read || la.Chapters[0].ReadAt != nil || la.Chapters[0].Notes != "A retained unread feedback" {
		t.Fatal("unread should clear reading timestamp and preserve supplied feedback")
	}
	dash := request(t, s, "GET", "dashboard", "", a, 200).Body.String()
	if strings.Contains(dash, "Private B chapter note") || strings.Contains(dash, "A retained unread feedback") {
		t.Fatal("dashboard leaked other-user or unread chapter")
	}
	dash = request(t, s, "GET", "dashboard", "", b, 200).Body.String()
	if !strings.Contains(dash, "Private B chapter note") || strings.Contains(dash, "Private A") {
		t.Fatal("Bob dashboard has wrong reading history")
	}
	request(t, s, "PUT", "library/13", `{"favorite":true,"toRead":true}`, a, 200)
	if !strings.Contains(request(t, s, "GET", "library?kind=to-read", "", a, 200).Body.String(), "One Piece") {
		t.Fatal("to-read not saved")
	}
	if strings.Contains(request(t, s, "GET", "library?kind=to-read", "", b, 200).Body.String(), "One Piece") {
		t.Fatal("to-read leaked")
	}
	request(t, s, "PUT", "library/13", `{"favorite":false,"toRead":false}`, a, 200)
	for _, kind := range []string{"favorites", "to-read"} {
		if strings.Contains(request(t, s, "GET", "library?kind="+kind, "", a, 200).Body.String(), "One Piece") {
			t.Fatal("base list removal not saved")
		}
	}
	created := request(t, s, "POST", "lists", `{"name":"Audit private","description":"Personal collection","isPublic":false}`, a, 201)
	var dto struct {
		List struct {
			ID string `json:"id"`
		} `json:"list"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &dto); err != nil {
		t.Fatal(err)
	}
	id := dto.List.ID
	request(t, s, "PUT", "lists/"+id+"/items/13", `{}`, a, 200)
	request(t, s, "PUT", "lists/"+id+"/items/13", `{}`, a, 200)
	if !strings.Contains(request(t, s, "GET", "lists/"+id, "", a, 200).Body.String(), `"itemCount":1`) {
		t.Fatal("list should deduplicate additions")
	}
	request(t, s, "DELETE", "lists/"+id+"/items/13", "", b, 404)
	request(t, s, "DELETE", "lists/"+id+"/items/13", "", a, 200)
	if strings.Contains(request(t, s, "GET", "lists/"+id, "", a, 200).Body.String(), "One Piece") {
		t.Fatal("list item removal failed")
	}
	request(t, s, "PUT", "lists/"+id+"/items/13", `{}`, a, 200)
	request(t, s, "PATCH", "lists/"+id, `{"name":"Audit shared","isPublic":true}`, a, 200)
	pub := request(t, s, "GET", "public/lists/"+id, "", nil, 200).Body.String()
	if !strings.Contains(pub, "Audit shared") || !strings.Contains(pub, "One Piece") {
		t.Fatal("public collection missing metadata")
	}
	for _, secret := range []string{"password", "email", "ownerId", "notes", "rating", "readAt", "Private B", "retained unread"} {
		if strings.Contains(pub, secret) {
			t.Fatalf("public list leaks %s", secret)
		}
	}
	request(t, s, "PATCH", "lists/"+id, `{"isPublic":false}`, a, 200)
	request(t, s, "GET", "public/lists/"+id, "", nil, 404)
	request(t, s, "GET", "lists/"+id, "", b, 404)
	store.Close()
	store, err = repository.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s = New(store, &testCatalog{}, Config{AllowedOrigins: []string{"http://localhost:4200"}})
	json.Unmarshal(request(t, s, "GET", "library/13", "", b, 200).Body.Bytes(), &lb)
	if len(lb.Chapters) != 1 || lb.Chapters[0].Notes != "Private B chapter note" || !lb.Chapters[0].Read {
		t.Fatal("feedback did not survive store reopen")
	}
	request(t, s, "GET", "lists/"+id, "", a, 200)
	request(t, s, "GET", "public/lists/"+id, "", nil, 404)
	request(t, s, "DELETE", "lists/"+id, "", a, 200)
	request(t, s, "GET", "lists/"+id, "", a, 404)
	request(t, s, "POST", "auth/logout", `{}`, b, 200)
	request(t, s, "GET", "library/13", "", b, 401)
	request(t, s, "GET", "library/13", "", a, 200)
}
