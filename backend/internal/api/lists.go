package api

import (
	"manga-track/backend/internal/auth"
	"manga-track/backend/internal/repository"
	"net/http"
	"sort"
	"strings"
	"time"
)

func listDTO(l repository.List) interface{} {
	return map[string]interface{}{"id": l.ID, "name": l.Name, "description": l.Description, "isPublic": l.IsPublic, "itemCount": len(l.ItemIDs), "createdAt": l.CreatedAt}
}
func (s *Server) lists(w http.ResponseWriter, r *http.Request) error {
	u, e := s.user(r)
	if e != nil {
		return e
	}
	if r.Method == "GET" {
		ls := []repository.List{}
		if viewErr := s.Store.View(func(st repository.State) {
			for _, l := range st.Lists {
				if l.OwnerID == u.ID {
					ls = append(ls, l)
				}
			}
		}); viewErr != nil {
			return viewErr
		}
		sort.Slice(ls, func(i, j int) bool { return ls[i].CreatedAt.Before(ls[j].CreatedAt) })
		items := []interface{}{}
		for _, l := range ls {
			items = append(items, listDTO(l))
		}
		write(w, 200, map[string]interface{}{"items": items})
		return nil
	}
	if e = method(r, "POST"); e != nil {
		return e
	}
	var b struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsPublic    bool   `json:"isPublic"`
	}
	if e = decode(w, r, &b); e != nil {
		return e
	}
	b.Name = strings.TrimSpace(b.Name)
	if len(b.Name) < 1 || len(b.Name) > 100 || len(b.Description) > 1000 {
		return fail(400, "List name is required (max 100 characters), description max 1000")
	}
	id, e := auth.Token()
	if e != nil {
		return e
	}
	l := repository.List{ID: id[:24], OwnerID: u.ID, Name: b.Name, Description: b.Description, IsPublic: b.IsPublic, ItemIDs: []int{}, CreatedAt: time.Now().UTC()}
	if e = s.Store.Update(func(st *repository.State) error { st.Lists[l.ID] = l; return nil }); e != nil {
		return e
	}
	write(w, 201, map[string]interface{}{"list": listDTO(l), "items": []repository.Manga{}})
	return nil
}
func (s *Server) list(w http.ResponseWriter, r *http.Request, p []string, public bool) error {
	var u repository.User
	var e error
	if !public {
		u, e = s.user(r)
		if e != nil {
			return e
		}
	}
	var l repository.List
	if viewErr := s.Store.View(func(st repository.State) { l = st.Lists[p[1]] }); viewErr != nil {
		return viewErr
	}
	if l.ID == "" || public && !l.IsPublic || !public && l.OwnerID != u.ID {
		return fail(404, "List not found")
	}
	if public && (len(p) != 2 || r.Method != "GET") {
		return fail(405, "Method not allowed")
	}
	if len(p) == 2 && r.Method == "GET" {
		items := []repository.Manga{}
		if viewErr := s.Store.View(func(st repository.State) {
			for _, id := range l.ItemIDs {
				if m, ok := st.Manga[id]; ok {
					items = append(items, m)
				}
			}
		}); viewErr != nil {
			return viewErr
		}
		write(w, 200, map[string]interface{}{"list": listDTO(l), "items": items})
		return nil
	}
	if len(p) == 2 && r.Method == "DELETE" {
		e = s.Store.Update(func(st *repository.State) error { delete(st.Lists, l.ID); return nil })
		if e != nil {
			return e
		}
		write(w, 200, map[string]bool{"ok": true})
		return nil
	}
	if len(p) == 2 && r.Method == "PATCH" {
		var b struct {
			Name        *string `json:"name"`
			Description *string `json:"description"`
			IsPublic    *bool   `json:"isPublic"`
		}
		if e = decode(w, r, &b); e != nil {
			return e
		}
		e = s.Store.Update(func(st *repository.State) error {
			v, ok := st.Lists[l.ID]
			if !ok || v.OwnerID != u.ID {
				return fail(404, "List not found")
			}
			if b.Name != nil {
				v.Name = strings.TrimSpace(*b.Name)
				if len(v.Name) < 1 || len(v.Name) > 100 {
					return fail(400, "List name must be 1–100 characters")
				}
			}
			if b.Description != nil {
				if len(*b.Description) > 1000 {
					return fail(400, "Description is too long")
				}
				v.Description = *b.Description
			}
			if b.IsPublic != nil {
				v.IsPublic = *b.IsPublic
			}
			st.Lists[l.ID] = v
			l = v
			return nil
		})
		if e != nil {
			return e
		}
		write(w, 200, map[string]interface{}{"list": listDTO(l)})
		return nil
	}
	if len(p) == 4 && p[2] == "items" && (r.Method == "PUT" || r.Method == "DELETE") {
		id, err := integer(p[3])
		if err != nil {
			return err
		}
		if r.Method == "PUT" {
			var b struct {
				Manga *repository.Manga `json:"manga"`
			}
			if e = decode(w, r, &b); e != nil {
				return e
			}
			if e = s.ensureManga(r, id, b.Manga); e != nil {
				return e
			}
		}
		e = s.Store.Update(func(st *repository.State) error {
			v, ok := st.Lists[l.ID]
			if !ok || v.OwnerID != u.ID {
				return fail(404, "List not found")
			}
			ids := []int{}
			for _, old := range v.ItemIDs {
				if old != id {
					ids = append(ids, old)
				}
			}
			if r.Method == "PUT" {
				ids = append(ids, id)
			}
			v.ItemIDs = ids
			st.Lists[l.ID] = v
			l = v
			return nil
		})
		if e != nil {
			return e
		}
		write(w, 200, map[string]interface{}{"list": listDTO(l)})
		return nil
	}
	return fail(405, "Method not allowed for list endpoint")
}
