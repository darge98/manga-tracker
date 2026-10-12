package repository_test

import (
	"context"
	"errors"
	"manga-track/backend/internal/repository"
	"manga-track/backend/internal/testdb"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func exists(t *testing.T, s *repository.Store, id int) bool {
	t.Helper()
	_, err := s.Manga(context.Background(), id)
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func TestCatalogStoresOnlyWhatListsDisplay(t *testing.T) {
	t.Parallel()
	s := testdb.Open(t)
	ctx := context.Background()
	seed(t, s, "a")

	// Browsing alone never stores a manga.
	if err := s.RefreshManga(ctx, manga(7)); err != nil || exists(t, s, 7) {
		t.Fatalf("refresh must not insert: %v", err)
	}
	if _, err := s.SaveChapter(ctx, "a", manga(7), repository.Chapter{Chapter: "1", Read: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	m, err := s.Manga(ctx, 7)
	if err != nil || m.Title != "Manga 7" || m.Description != "" || m.Author != "" || len(m.Genres) != 1 {
		t.Fatalf("stored entry must keep card fields and drop synopsis/authors: %v %+v", err, m)
	}
	fresh := manga(7)
	fresh.Title = "Renamed"
	if err = s.RefreshManga(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	if m, _ = s.Manga(ctx, 7); m.Title != "Renamed" || m.Description != "" {
		t.Fatalf("refresh must update stored fields only: %+v", m)
	}
}

func TestUnreferencedMangaArePruned(t *testing.T) {
	t.Parallel()
	s := testdb.Open(t)
	ctx := context.Background()
	seed(t, s, "a", "b")
	on, off := true, false

	// Library: removing the last flag, or the last chapter, drops the entry and the manga.
	if _, err := s.SetLibraryFlags(ctx, "a", manga(1), &on, nil); err != nil {
		t.Fatal(err)
	}
	if l, err := s.SetLibraryFlags(ctx, "a", manga(1), &off, nil); err != nil || l.ToRead || exists(t, s, 1) {
		t.Fatalf("empty entry and its manga must go: %v %+v", err, l)
	}
	if _, err := s.SaveChapter(ctx, "a", manga(2), repository.Chapter{Chapter: "3", Read: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.DeleteChapter(ctx, "a", 2, "3"); err != nil || exists(t, s, 2) {
		t.Fatalf("deleting the last chapter must prune the manga: %v", err)
	}

	// A manga stays while anyone still references it.
	if _, err := s.SaveChapter(ctx, "b", manga(3), repository.Chapter{Chapter: "1", Read: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateList(ctx, repository.List{ID: "list", OwnerID: "a", Name: "L", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{3, 4, 5} {
		if _, err := s.AddListItem(ctx, "list", "a", manga(id)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.RemoveListItem(ctx, "list", "a", 3); err != nil || !exists(t, s, 3) {
		t.Fatalf("manga still in b's library must stay: %v", err)
	}
	if _, err := s.RemoveListItem(ctx, "list", "a", 4); err != nil || exists(t, s, 4) {
		t.Fatalf("unreferenced manga must be pruned: %v", err)
	}
	if err := s.DeleteList(ctx, "list", "a"); err != nil || exists(t, s, 5) {
		t.Fatalf("deleting a list must prune its manga: %v", err)
	}

	// Deleting an account prunes only what it alone referenced.
	if _, err := s.SaveChapter(ctx, "a", manga(3), repository.Chapter{Chapter: "1", Read: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveChapter(ctx, "a", manga(6), repository.Chapter{Chapter: "1", Read: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteUser(ctx, "a"); err != nil || exists(t, s, 6) || !exists(t, s, 3) {
		t.Fatalf("account deletion pruning is wrong: %v", err)
	}
}

func TestEmptyEntriesAreNotStored(t *testing.T) {
	t.Parallel()
	s := testdb.Open(t)
	ctx := context.Background()
	seed(t, s, "a")
	off := false
	if l, err := s.SetLibraryFlags(ctx, "a", manga(1), &off, &off); err != nil || exists(t, s, 1) || l.MangaID != 1 {
		t.Fatalf("turning flags off on a missing entry must store nothing: %v %+v", err, l)
	}
	if _, err := s.SaveChapter(ctx, "a", manga(2), repository.Chapter{Chapter: "1", Read: true, Notes: "x"}, time.Now()); err != nil {
		t.Fatal(err)
	}
	l, err := s.SaveChapter(ctx, "a", manga(2), repository.Chapter{Chapter: "1"}, time.Now())
	if err != nil || len(l.Chapters) != 0 || exists(t, s, 2) {
		t.Fatalf("an empty chapter must delete the row (and the now-empty entry): %v %+v", err, l)
	}
}

func TestPerUserLimits(t *testing.T) {
	t.Parallel()
	s := testdb.Open(t)
	s.Limits = repository.Limits{ChaptersPerUser: 2, LibraryPerUser: 2, ListsPerUser: 1, ItemsPerList: 1}
	ctx := context.Background()
	seed(t, s, "a")
	read := func(id int, chapter string) error {
		_, err := s.SaveChapter(ctx, "a", manga(id), repository.Chapter{Chapter: chapter, Read: true}, time.Now())
		return err
	}
	if err := read(1, "1"); err != nil {
		t.Fatal(err)
	}
	if err := read(1, "2"); err != nil {
		t.Fatal(err)
	}
	if err := read(1, "3"); !errors.Is(err, repository.ErrLimitReached) {
		t.Fatalf("chapter limit: %v", err)
	}
	if err := read(1, "2"); err != nil {
		t.Fatalf("updating an existing chapter must work at the limit: %v", err)
	}
	on := true
	if _, err := s.SetLibraryFlags(ctx, "a", manga(2), &on, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SetLibraryFlags(ctx, "a", manga(3), &on, nil); !errors.Is(err, repository.ErrLimitReached) {
		t.Fatalf("library limit: %v", err)
	}
	if exists(t, s, 3) {
		t.Fatal("a refused entry must not store its manga")
	}
	if err := s.CreateList(ctx, repository.List{ID: "l1", OwnerID: "a", Name: "One", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateList(ctx, repository.List{ID: "l2", OwnerID: "a", Name: "Two", CreatedAt: time.Now()}); !errors.Is(err, repository.ErrLimitReached) {
		t.Fatalf("list limit: %v", err)
	}
	if _, err := s.AddListItem(ctx, "l1", "a", manga(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddListItem(ctx, "l1", "a", manga(1)); err != nil {
		t.Fatalf("re-adding an item must stay a no-op at the limit: %v", err)
	}
	if _, err := s.AddListItem(ctx, "l1", "a", manga(4)); !errors.Is(err, repository.ErrLimitReached) {
		t.Fatalf("items per list limit: %v", err)
	}
}

// Migration 002 rewrites data stored by the previous schema version.
func TestLeanCatalogMigrationOnOldData(t *testing.T) {
	t.Parallel()
	url := testdb.URL(t)
	s := testdb.OpenURL(t, url)
	ctx := context.Background()
	seed(t, s, "a")
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	// Data as version 1 stored it: full metadata, browsed-only manga, empty rows.
	for _, stmt := range []string{
		`INSERT INTO manga(id,metadata) VALUES
			(1,'{"id":1,"title":"Kept","description":"long synopsis","author":"A"}'),
			(2,'{"id":2,"title":"Only browsed","description":"x"}'),
			(3,'{"id":3,"title":"Empty entry"}')`,
		`INSERT INTO library(user_id,manga_id,to_read) VALUES ('a',1,true),('a',3,false)`,
		`INSERT INTO chapters(user_id,manga_id,chapter,is_read,notes) VALUES ('a',3,1,false,'')`,
		`DELETE FROM schema_migrations WHERE version=2`,
	} {
		if _, err = conn.Exec(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	reopened := testdb.OpenURL(t, url) // runs migration 002 again
	m, err := reopened.Manga(ctx, 1)
	if err != nil || m.Title != "Kept" || m.Description != "" || m.Author != "" {
		t.Fatalf("referenced manga must be kept without synopsis/authors: %v %+v", err, m)
	}
	if exists(t, reopened, 2) || exists(t, reopened, 3) {
		t.Fatal("browsed-only manga and empty entries must be removed")
	}
	var indexes string
	if err = conn.QueryRow(ctx, `SELECT string_agg(indexname, ',') FROM pg_indexes WHERE indexname IN ('library_manga_idx','list_items_manga_idx')`).Scan(&indexes); err != nil || !strings.Contains(indexes, "library_manga_idx") || !strings.Contains(indexes, "list_items_manga_idx") {
		t.Fatalf("reference indexes missing: %q %v", indexes, err)
	}
}

func TestLibraryStats(t *testing.T) {
	t.Parallel()
	s := testdb.Open(t)
	ctx := context.Background()
	seed(t, s, "a", "b")
	on := true
	four, two := 4, 2
	if _, err := s.SetLibraryFlags(ctx, "a", manga(1), nil, &on); err != nil {
		t.Fatal(err)
	}
	for _, c := range []repository.Chapter{{Chapter: "1", Read: true, Rating: &four}, {Chapter: "2", Read: true, Rating: &two}, {Chapter: "3", Notes: "unread"}} {
		if _, err := s.SaveChapter(ctx, "a", manga(2), c, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.SaveChapter(ctx, "b", manga(3), repository.Chapter{Chapter: "1", Read: true}, time.Now()); err != nil {
		t.Fatal(err)
	}
	stats, err := s.LibraryStats(ctx, "a")
	if err != nil || len(stats) != 2 {
		t.Fatalf("one row per own library entry: %v %+v", err, stats)
	}
	byID := map[int]repository.LibraryStat{}
	for _, st := range stats {
		byID[st.MangaID] = st
	}
	if f := byID[1]; !f.Favorite || f.AvgRating != nil || f.ReadChapters != 0 || f.Title != "Manga 1" {
		t.Fatalf("favorite without chapters: %+v", f)
	}
	if r := byID[2]; r.AvgRating == nil || *r.AvgRating != 3 || r.ReadChapters != 2 {
		t.Fatalf("average rating and read count: %+v", r)
	}
}
