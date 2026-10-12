// Package recommend turns a reader's library and MyAnimeList user
// recommendations into a ranked list of suggestions. It is pure: callers
// fetch the data, so the ranking is easy to test and to tune.
package recommend

import (
	"manga-track/backend/internal/provider"
	"manga-track/backend/internal/repository"
	"math"
	"sort"
)

// Seed is a manga the reader liked, weighted by how much.
type Seed struct {
	MangaID int
	Title   string
	Weight  float64
}

// Because names the liked manga that contributed most to a suggestion.
type Because struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type Item struct {
	Manga   repository.Manga `json:"manga"`
	Because Because          `json:"because"`
}

// weight scores how much the reader liked a manga: a favorite counts most,
// then the average chapter rating (below 3 counts against), then how far they
// read. A manga only saved "to read" has no weight: it was not read yet.
func weight(st repository.LibraryStat) float64 {
	w := 0.0
	if st.Favorite {
		w += 3
	}
	if st.AvgRating != nil {
		w += *st.AvgRating - 3
	}
	return w + math.Min(float64(st.ReadChapters), 100)/100
}

// Seeds returns up to max manga the reader liked most, strongest first.
func Seeds(stats []repository.LibraryStat, max int) []Seed {
	seeds := []Seed{}
	for _, st := range stats {
		if w := weight(st); w > 0 {
			seeds = append(seeds, Seed{st.MangaID, st.Title, w})
		}
	}
	sort.Slice(seeds, func(i, j int) bool {
		if seeds[i].Weight != seeds[j].Weight {
			return seeds[i].Weight > seeds[j].Weight
		}
		return seeds[i].MangaID < seeds[j].MangaID
	})
	if len(seeds) > max {
		seeds = seeds[:max]
	}
	return seeds
}

// Rank scores every recommended manga by the sum, over the seeds that lead to
// it, of seed weight times log(1+votes): titles suggested from several liked
// manga rise, and vote counts matter with diminishing returns. Manga in
// exclude (the reader's library) are skipped.
func Rank(seeds []Seed, recs map[int][]provider.Recommendation, exclude map[int]bool, limit int) []Item {
	type scored struct {
		item  Item
		score float64
		best  float64 // strongest single contribution, for Because
	}
	byID := map[int]*scored{}
	for _, seed := range seeds {
		for _, r := range recs[seed.MangaID] {
			if exclude[r.Manga.ID] {
				continue
			}
			contribution := seed.Weight * math.Log1p(float64(r.Votes))
			c := byID[r.Manga.ID]
			if c == nil {
				c = &scored{item: Item{Manga: r.Manga}}
				byID[r.Manga.ID] = c
			}
			c.score += contribution
			if contribution > c.best {
				c.best = contribution
				c.item.Because = Because{seed.MangaID, seed.Title}
			}
		}
	}
	ranked := make([]*scored, 0, len(byID))
	for _, c := range byID {
		ranked = append(ranked, c)
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].item.Manga.ID < ranked[j].item.Manga.ID
	})
	items := []Item{}
	for _, c := range ranked {
		if len(items) == limit {
			break
		}
		items = append(items, c.item)
	}
	return items
}
