package recommend

import (
	"manga-track/backend/internal/provider"
	"manga-track/backend/internal/repository"
	"testing"
)

func rating(r float64) *float64 { return &r }

func TestSeedsWeighWhatTheReaderLiked(t *testing.T) {
	stats := []repository.LibraryStat{
		{MangaID: 1, Title: "Favorite", Favorite: true},
		{MangaID: 2, Title: "Rated 5", AvgRating: rating(5), ReadChapters: 10},
		{MangaID: 3, Title: "Read a lot", ReadChapters: 300},
		{MangaID: 4, Title: "Disliked", AvgRating: rating(1), ReadChapters: 20},
		{MangaID: 5, Title: "Only to read"},
	}
	seeds := Seeds(stats, 10)
	if len(seeds) != 3 || seeds[0].MangaID != 1 || seeds[1].MangaID != 2 || seeds[2].MangaID != 3 {
		t.Fatalf("seeds must be liked manga, strongest first: %+v", seeds)
	}
	if got := Seeds(stats, 2); len(got) != 2 {
		t.Fatalf("seeds must be capped: %+v", got)
	}
}

func TestRankAggregatesExcludesAndExplains(t *testing.T) {
	m := func(id int) repository.Manga { return repository.Manga{ID: id, Title: "M"} }
	seeds := []Seed{{MangaID: 1, Title: "Berserk", Weight: 3}, {MangaID: 2, Title: "Vagabond", Weight: 1}}
	recs := map[int][]provider.Recommendation{
		1: {{Manga: m(10), Votes: 10}, {Manga: m(11), Votes: 20}, {Manga: m(2), Votes: 50}},
		2: {{Manga: m(10), Votes: 30}, {Manga: m(12), Votes: 1}},
	}
	items := Rank(seeds, recs, map[int]bool{1: true, 2: true}, 10)
	ids := []int{}
	for _, it := range items {
		ids = append(ids, it.Manga.ID)
	}
	// 10 is suggested by both seeds and beats 11, suggested by one with more votes;
	// 2 is in the library; 12 has a single weak vote.
	if len(ids) != 3 || ids[0] != 10 || ids[1] != 11 || ids[2] != 12 {
		t.Fatalf("unexpected ranking %v", ids)
	}
	if items[0].Because.Title != "Berserk" {
		t.Fatalf("Because must name the strongest contribution: %+v", items[0].Because)
	}
	if got := Rank(seeds, recs, nil, 1); len(got) != 1 {
		t.Fatal("limit not applied")
	}
	if got := Rank(nil, recs, nil, 10); len(got) != 0 {
		t.Fatal("no seeds, no recommendations")
	}
}
