package api

import (
	"context"
	"errors"
	"manga-track/backend/internal/provider"
	"manga-track/backend/internal/repository"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// minSearchLength matches MyAnimeList, which rejects shorter queries.
const minSearchLength = 3

func pageParam(r *http.Request) int {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	return min(max(p, 1), 1000)
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 200 {
		return fail(http.StatusBadRequest, "search_too_long", "Search is too long")
	}
	if q == "" {
		return s.popular(w, r)
	}
	if utf8.RuneCountInString(q) < minSearchLength {
		return fail(http.StatusBadRequest, "search_too_short", "Search needs at least 3 characters")
	}
	result, err := s.catalog.Search(r.Context(), q, pageParam(r))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

func (s *Server) popular(w http.ResponseWriter, r *http.Request) error {
	result, err := s.catalog.Popular(r.Context(), pageParam(r))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, result)
	return nil
}

// mangaDetail stores fresh metadata so the page keeps working, with a warning,
// when the catalog is down.
func (s *Server) mangaDetail(w http.ResponseWriter, r *http.Request) error {
	id, err := mangaID(r, "id")
	if err != nil {
		return err
	}
	warning := ""
	m, err := s.catalog.Detail(r.Context(), id)
	switch {
	case err == nil:
		if err = s.store.SaveManga(r.Context(), m); err != nil {
			return err
		}
	case errors.Is(err, provider.ErrUnavailable):
		saved, storeErr := s.store.Manga(r.Context(), id)
		if errors.Is(storeErr, repository.ErrNotFound) {
			return err
		} else if storeErr != nil {
			return storeErr
		}
		m = saved
		warning = "Catalog temporarily unavailable. Showing previously saved metadata."
	default:
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"manga": m, "catalogWarning": warning})
	return nil
}

// ensureManga makes sure metadata exists before a library or list row
// references it. Metadata always comes from the catalog, never from clients.
func (s *Server) ensureManga(ctx context.Context, id int) error {
	_, err := s.store.Manga(ctx, id)
	if !errors.Is(err, repository.ErrNotFound) {
		return err
	}
	m, err := s.catalog.Detail(ctx, id)
	if err != nil {
		return err
	}
	return s.store.SaveManga(ctx, m)
}
