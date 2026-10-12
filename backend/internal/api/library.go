package api

import (
	"manga-track/backend/internal/repository"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const dashboardRecent = 12

func (s *Server) libraryItems(w http.ResponseWriter, r *http.Request, user repository.User) error {
	var items []repository.Manga
	var err error
	switch r.URL.Query().Get("kind") {
	case "favorites":
		items, err = s.store.Favorites(r.Context(), user.ID)
	case "to-read":
		items, err = s.store.ToRead(r.Context(), user.ID)
	default:
		return fail(http.StatusBadRequest, "invalid_kind", "kind must be favorites or to-read")
	}
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (s *Server) getLibrary(w http.ResponseWriter, r *http.Request, user repository.User) error {
	id, err := mangaID(r, "id")
	if err != nil {
		return err
	}
	l, err := s.store.Library(r.Context(), user.ID, id)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, l)
	return nil
}

func (s *Server) setLibraryFlags(w http.ResponseWriter, r *http.Request, user repository.User) error {
	id, err := mangaID(r, "id")
	if err != nil {
		return err
	}
	var body struct {
		ToRead   *bool `json:"toRead"`
		Favorite *bool `json:"favorite"`
	}
	if err = decode(w, r, &body); err != nil {
		return err
	}
	if err = s.ensureManga(r.Context(), id); err != nil {
		return err
	}
	l, err := s.store.SetLibraryFlags(r.Context(), user.ID, id, body.ToRead, body.Favorite)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, l)
	return nil
}

// chapterNumber accepts positive decimals such as "12.5" and returns their
// canonical form, so "12.50" and "12.5" address the same chapter.
func chapterNumber(raw string) (string, error) {
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 100000 || strings.ContainsAny(raw, "eE+-") || len(raw) > 12 {
		return "", fail(http.StatusBadRequest, "invalid_chapter", "Chapter must be a positive number, including decimals")
	}
	return strconv.FormatFloat(n, 'f', -1, 64), nil
}

func (s *Server) saveChapter(w http.ResponseWriter, r *http.Request, user repository.User) error {
	id, err := mangaID(r, "id")
	if err != nil {
		return err
	}
	chapter, err := chapterNumber(r.PathValue("chapter"))
	if err != nil {
		return err
	}
	var body struct {
		Read   *bool  `json:"read"`
		Rating *int   `json:"rating"`
		Notes  string `json:"notes"`
	}
	if err = decode(w, r, &body); err != nil {
		return err
	}
	if body.Read == nil || len(body.Notes) > 5000 || body.Rating != nil && (*body.Rating < 1 || *body.Rating > 5) {
		return fail(http.StatusBadRequest, "invalid_chapter_feedback", "Provide read, optional rating 1–5, and notes up to 5000 characters")
	}
	if err = s.ensureManga(r.Context(), id); err != nil {
		return err
	}
	c := repository.Chapter{Chapter: chapter, Read: *body.Read, Rating: body.Rating, Notes: body.Notes}
	l, err := s.store.SaveChapter(r.Context(), user.ID, id, c, time.Now().UTC())
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, l)
	return nil
}

func (s *Server) deleteChapter(w http.ResponseWriter, r *http.Request, user repository.User) error {
	id, err := mangaID(r, "id")
	if err != nil {
		return err
	}
	chapter, err := chapterNumber(r.PathValue("chapter"))
	if err != nil {
		return err
	}
	l, err := s.store.DeleteChapter(r.Context(), user.ID, id, chapter)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, l)
	return nil
}

// dashboard shows the latest read chapter of each manga plus popular titles.
// A catalog outage only adds a warning: reading history still loads.
func (s *Server) dashboard(w http.ResponseWriter, r *http.Request, user repository.User) error {
	recent, err := s.store.RecentReads(r.Context(), user.ID, dashboardRecent)
	if err != nil {
		return err
	}
	popular := []repository.Manga{}
	warning := ""
	if page, err := s.catalog.Popular(r.Context(), 1); err == nil {
		popular = page.Items
	} else {
		warning = "Catalog temporarily unavailable. Your reading history is available."
	}
	writeJSON(w, http.StatusOK, map[string]any{"recent": recent, "popular": popular, "catalogWarning": warning})
	return nil
}
