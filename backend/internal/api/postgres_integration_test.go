//go:build postgres

package api

import (
	"encoding/json"
	"manga-track/backend/internal/repository"
	"os"
	"strings"
	"testing"
)

func TestPostgresAPIAuthenticationTrackingPrivacyAndReconnect(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("requires a dedicated EMPTY PostgreSQL TEST_DATABASE_URL; live database not verified")
	}
	store, err := repository.OpenPostgres(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err = store.View(func(st repository.State) {
		if len(st.Users)+len(st.Manga)+len(st.Lists)+len(st.Sessions)+len(st.Libraries) != 0 {
			t.Fatal("refusing PostgreSQL API test against nonempty database")
		}
	}); err != nil {
		t.Fatal(err)
	}
	s := New(store, &testCatalog{}, Config{AllowedOrigins: []string{"http://localhost:4200"}})
	a := request(t, s, "POST", "auth/register", `{"username":"PG Alice","email":"pg-alice@example.test","password":"postgres-password-a"}`, nil, 200).Result().Cookies()[0]
	b := request(t, s, "POST", "auth/register", `{"username":"PG Bob","email":"pg-bob@example.test","password":"postgres-password-b"}`, nil, 200).Result().Cookies()[0]
	var owners []string
	if err = store.View(func(st repository.State) {
		for id, u := range st.Users {
			if u.Email == "pg-alice@example.test" || u.Email == "pg-bob@example.test" {
				owners = append(owners, id)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := store.Update(func(st *repository.State) error {
			for _, id := range owners {
				delete(st.Users, id)
				delete(st.Libraries, id)
				for token, ss := range st.Sessions {
					if ss.UserID == id {
						delete(st.Sessions, token)
					}
				}
				for listID, l := range st.Lists {
					if l.OwnerID == id {
						delete(st.Lists, listID)
					}
				}
			}
			// Dedicated test DB only. Preserve shared metadata if another test user references it.
			used := false
			for _, entries := range st.Libraries {
				if _, ok := entries[13]; ok {
					used = true
				}
			}
			for _, l := range st.Lists {
				for _, id := range l.ItemIDs {
					if id == 13 {
						used = true
					}
				}
			}
			if !used {
				delete(st.Manga, 13)
			}
			return nil
		})
		if err != nil {
			t.Error("cannot clean PostgreSQL API test:", err)
		}
	})
	request(t, s, "GET", "manga?q=one", "", nil, 200)
	request(t, s, "PUT", "library/13/chapters/0.5", `{"read":true,"rating":5,"notes":"Alice private feedback"}`, a, 200)
	request(t, s, "PUT", "library/13", `{"favorite":true,"toRead":true}`, a, 200)
	response := request(t, s, "GET", "library/13", "", b, 200)
	if strings.Contains(response.Body.String(), "Alice private feedback") {
		t.Fatal("feedback leaked across users in PostgreSQL")
	}
	created := request(t, s, "POST", "lists", `{"name":"PG collection","description":"Share","isPublic":true}`, a, 201)
	var dto struct {
		List struct {
			ID string `json:"id"`
		} `json:"list"`
	}
	json.Unmarshal(created.Body.Bytes(), &dto)
	request(t, s, "PUT", "lists/"+dto.List.ID+"/items/13", `{}`, a, 200)
	request(t, s, "PATCH", "lists/"+dto.List.ID, `{"isPublic":false}`, b, 404)
	response = request(t, s, "GET", "public/lists/"+dto.List.ID, "", nil, 200)
	if strings.Contains(response.Body.String(), "Alice private feedback") || strings.Contains(response.Body.String(), "pg-alice@") {
		t.Fatal("private data leaked through PostgreSQL public list")
	}
	request(t, s, "PATCH", "lists/"+dto.List.ID, `{"isPublic":false}`, a, 200)
	request(t, s, "GET", "public/lists/"+dto.List.ID, "", nil, 404)
	reopened, openErr := repository.OpenPostgres(dsn)
	if openErr != nil {
		t.Fatal(openErr)
	}
	store.Close()
	store = reopened
	s = New(store, &testCatalog{}, Config{AllowedOrigins: []string{"http://localhost:4200"}})
	request(t, s, "GET", "auth/me", "", a, 200)
	response = request(t, s, "GET", "library/13", "", a, 200)
	if !strings.Contains(response.Body.String(), "Alice private feedback") || !strings.Contains(response.Body.String(), `"chapter":"0.5"`) {
		t.Fatal("PostgreSQL feedback or decimal chapter did not survive reconnect")
	}
	request(t, s, "POST", "auth/login", `{"email":"pg-alice@example.test","password":"postgres-password-a"}`, nil, 200)
	request(t, s, "POST", "auth/logout", `{}`, b, 200)
	request(t, s, "GET", "auth/me", "", b, 401)
	request(t, s, "GET", "auth/me", "", a, 200)
}
