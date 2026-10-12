package api

import (
	"errors"
	"manga-track/backend/internal/auth"
	"manga-track/backend/internal/repository"
	"net/http"
	"strings"
	"time"
)

var errListNotFound = fail(http.StatusNotFound, "List not found")

func validListName(name string) bool { return len(name) >= 1 && len(name) <= 100 }

// notFoundAsList reports missing or foreign lists with the same 404, so
// callers cannot probe for other users' list IDs.
func notFoundAsList(err error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return errListNotFound
	}
	return err
}

func (s *Server) ownLists(w http.ResponseWriter, r *http.Request, user repository.User) error {
	lists, err := s.store.ListsByOwner(r.Context(), user.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": lists})
	return nil
}

func (s *Server) createList(w http.ResponseWriter, r *http.Request, user repository.User) error {
	var body struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		IsPublic    bool   `json:"isPublic"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	body.Name = strings.TrimSpace(body.Name)
	if !validListName(body.Name) || len(body.Description) > 1000 {
		return fail(http.StatusBadRequest, "List name is required (max 100 characters), description max 1000")
	}
	id, err := auth.Token()
	if err != nil {
		return err
	}
	l := repository.List{ID: id[:24], OwnerID: user.ID, Name: body.Name, Description: body.Description, IsPublic: body.IsPublic, CreatedAt: time.Now().UTC()}
	if err = s.store.CreateList(r.Context(), l); err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"list": l, "items": []repository.Manga{}})
	return nil
}

func (s *Server) getList(w http.ResponseWriter, r *http.Request, user repository.User) error {
	return s.writeList(w, r, func(l repository.List) bool { return l.OwnerID == user.ID })
}

func (s *Server) publicList(w http.ResponseWriter, r *http.Request) error {
	return s.writeList(w, r, func(l repository.List) bool { return l.IsPublic })
}

func (s *Server) writeList(w http.ResponseWriter, r *http.Request, visible func(repository.List) bool) error {
	l, err := s.store.List(r.Context(), r.PathValue("id"))
	if err != nil {
		return notFoundAsList(err)
	}
	if !visible(l) {
		return errListNotFound
	}
	items, err := s.store.ListItems(r.Context(), l.ID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": l, "items": items})
	return nil
}

func (s *Server) updateList(w http.ResponseWriter, r *http.Request, user repository.User) error {
	var body struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		IsPublic    *bool   `json:"isPublic"`
	}
	if err := decode(w, r, &body); err != nil {
		return err
	}
	if body.Name != nil {
		trimmed := strings.TrimSpace(*body.Name)
		if !validListName(trimmed) {
			return fail(http.StatusBadRequest, "List name must be 1–100 characters")
		}
		body.Name = &trimmed
	}
	if body.Description != nil && len(*body.Description) > 1000 {
		return fail(http.StatusBadRequest, "Description is too long")
	}
	l, err := s.store.UpdateList(r.Context(), r.PathValue("id"), user.ID, repository.ListPatch{Name: body.Name, Description: body.Description, IsPublic: body.IsPublic})
	if err != nil {
		return notFoundAsList(err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": l})
	return nil
}

func (s *Server) deleteList(w http.ResponseWriter, r *http.Request, user repository.User) error {
	if err := s.store.DeleteList(r.Context(), r.PathValue("id"), user.ID); err != nil {
		return notFoundAsList(err)
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	return nil
}

func (s *Server) addListItem(w http.ResponseWriter, r *http.Request, user repository.User) error {
	id, err := mangaID(r, "mangaId")
	if err != nil {
		return err
	}
	// Check ownership before ensureManga, which may call the external catalog.
	l, err := s.store.List(r.Context(), r.PathValue("id"))
	if err != nil {
		return notFoundAsList(err)
	}
	if l.OwnerID != user.ID {
		return errListNotFound
	}
	if err = s.ensureManga(r.Context(), id); err != nil {
		return err
	}
	l, err = s.store.AddListItem(r.Context(), l.ID, user.ID, id)
	if err != nil {
		return notFoundAsList(err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": l})
	return nil
}

func (s *Server) removeListItem(w http.ResponseWriter, r *http.Request, user repository.User) error {
	id, err := mangaID(r, "mangaId")
	if err != nil {
		return err
	}
	l, err := s.store.RemoveListItem(r.Context(), r.PathValue("id"), user.ID, id)
	if err != nil {
		return notFoundAsList(err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"list": l})
	return nil
}
