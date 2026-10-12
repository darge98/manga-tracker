package api

import (
	"manga-track/backend/internal/provider"
	"manga-track/backend/internal/recommend"
	"manga-track/backend/internal/repository"
	"net/http"
	"sync"
)

const (
	recommendationSeeds = 5 // one catalog call each, cached with the detail page
	recommendationLimit = 12
)

// recommendations suggests manga from what MyAnimeList users recommend to
// readers of the manga this user liked most. Nothing is stored: results are
// computed from the library and the cached catalog responses.
func (s *Server) recommendations(w http.ResponseWriter, r *http.Request, user repository.User) error {
	stats, err := s.store.LibraryStats(r.Context(), user.ID)
	if err != nil {
		return err
	}
	seeds := recommend.Seeds(stats, recommendationSeeds)
	exclude := map[int]bool{}
	for _, st := range stats {
		exclude[st.MangaID] = true
	}

	recs := map[int][]provider.Recommendation{}
	failed := 0
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, seed := range seeds {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			list, err := s.catalog.Recommendations(r.Context(), id)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				return
			}
			recs[id] = list
		}(seed.MangaID)
	}
	wg.Wait()

	warning := ""
	if failed > 0 {
		warning = "Catalog temporarily unavailable. Recommendations may be incomplete."
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":          recommend.Rank(seeds, recs, exclude, recommendationLimit),
		"basedOn":        len(seeds),
		"catalogWarning": warning,
	})
	return nil
}
