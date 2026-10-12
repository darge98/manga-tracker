package repository_test

import (
	"context"
	"errors"
	"manga-track/backend/internal/auth"
	"manga-track/backend/internal/repository"
	"manga-track/backend/internal/testdb"
	"strconv"
	"sync"
	"testing"
	"time"
)

func seed(t *testing.T, s *repository.Store, userID string, mangaIDs ...int) {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateUser(ctx, repository.User{ID: userID, Username: "User " + userID, Email: userID + "@example.test", PasswordHash: "hash"}); err != nil {
		t.Fatal(err)
	}
	for _, id := range mangaIDs {
		if err := s.SaveManga(ctx, repository.Manga{ID: id, Title: "Manga " + strconv.Itoa(id), Genres: []string{}}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMigrationsRunOnceAcrossReopen(t *testing.T) {
	t.Parallel()
	url := testdb.URL(t)
	first := testdb.OpenURL(t, url)
	seed(t, first, "a", 13)
	second := testdb.OpenURL(t, url) // a second process re-runs migrate
	if _, err := second.Manga(context.Background(), 13); err != nil {
		t.Fatalf("data lost after reopening: %v", err)
	}
}

func TestUsersAndSessions(t *testing.T) {
	t.Parallel()
	s := testdb.Open(t)
	ctx := context.Background()
	seed(t, s, "a")
	if err := s.CreateUser(ctx, repository.User{ID: "b", Username: "Dup", Email: "a@example.test", PasswordHash: "x"}); !errors.Is(err, repository.ErrEmailTaken) {
		t.Fatalf("duplicate email: %v", err)
	}
	live, expired := auth.TokenHash("live"), auth.TokenHash("expired")
	if err := s.CreateSession(ctx, live, "a", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, expired, "a", time.Now().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if u, err := s.UserBySession(ctx, live); err != nil || u.ID != "a" {
		t.Fatalf("live session: %v %+v", err, u)
	}
	if _, err := s.UserBySession(ctx, expired); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("expired session accepted: %v", err)
	}
	if n, err := s.DeleteExpiredSessions(ctx); err != nil || n != 1 {
		t.Fatalf("cleanup removed %d: %v", n, err)
	}
	if _, err := s.UserBySession(ctx, live); err != nil {
		t.Fatal("cleanup removed a live session")
	}
}

func TestChapterTimestampsAndConcurrentWriters(t *testing.T) {
	t.Parallel()
	url := testdb.URL(t)
	s := testdb.OpenURL(t, url)
	other := testdb.OpenURL(t, url) // two backend processes
	ctx := context.Background()
	seed(t, s, "a", 13)
	first := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	if _, err := s.SaveChapter(ctx, "a", 13, repository.Chapter{Chapter: "12.5", Read: true}, first); err != nil {
		t.Fatal(err)
	}
	l, err := s.SaveChapter(ctx, "a", 13, repository.Chapter{Chapter: "12.5", Read: true, Notes: "edited"}, first.Add(time.Hour))
	if err != nil || !l.Chapters[0].ReadAt.Equal(first) || l.Chapters[0].Notes != "edited" {
		t.Fatalf("editing feedback must keep the first read time: %v %+v", err, l)
	}
	l, err = s.SaveChapter(ctx, "a", 13, repository.Chapter{Chapter: "12.5", Read: false}, first)
	if err != nil || l.Chapters[0].ReadAt != nil {
		t.Fatalf("unread must clear the read time: %v %+v", err, l)
	}

	var wg sync.WaitGroup
	for i := 1; i <= 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			target := s
			if i%2 == 0 {
				target = other
			}
			if _, err := target.SaveChapter(ctx, "a", 13, repository.Chapter{Chapter: strconv.Itoa(i), Read: true}, time.Now()); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	l, err = other.Library(ctx, "a", 13)
	if err != nil || len(l.Chapters) != 13 {
		t.Fatalf("lost concurrent chapter writes: %v, %d chapters", err, len(l.Chapters))
	}
	if l.Chapters[0].Chapter != "1" || l.Chapters[12].Chapter != "12.5" {
		t.Fatalf("chapters must be sorted numerically: %+v", l.Chapters)
	}
}

func TestListItemsKeepOrderUnderConcurrency(t *testing.T) {
	t.Parallel()
	s := testdb.Open(t)
	ctx := context.Background()
	ids := []int{1, 2, 3, 4, 5, 6, 7, 8}
	seed(t, s, "a", ids...)
	seed(t, s, "b")
	if err := s.CreateList(ctx, repository.List{ID: "list", OwnerID: "a", Name: "Mine", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if _, err := s.AddListItem(ctx, "list", "a", id); err != nil {
				t.Error(err)
			}
		}(id)
	}
	wg.Wait()
	l, err := s.AddListItem(ctx, "list", "a", 1) // duplicate is a no-op
	if err != nil || l.ItemCount != len(ids) {
		t.Fatalf("expected %d items: %v %+v", len(ids), err, l)
	}
	if _, err = s.AddListItem(ctx, "list", "b", 1); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("non-owner modified the list: %v", err)
	}
	if err = s.DeleteList(ctx, "list", "b"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("non-owner deleted the list: %v", err)
	}
	if l, err = s.RemoveListItem(ctx, "list", "a", 3); err != nil || l.ItemCount != len(ids)-1 {
		t.Fatalf("remove: %v %+v", err, l)
	}
}

func TestClosedPoolReportsUnavailable(t *testing.T) {
	t.Parallel()
	s := testdb.Open(t)
	s.Close()
	if err := s.Ping(context.Background()); !errors.Is(err, repository.ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
}
