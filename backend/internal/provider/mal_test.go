package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func respond(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}, nil
}

func newTestMAL(transport roundTrip) *MAL {
	p := NewMAL("test-client-id")
	p.Client = &http.Client{Transport: transport}
	return p
}

// Shaped like the examples in the official API v2 specification.
const berserkNode = `{"id":2,"title":"Berserk","main_picture":{"medium":"https://cdn.test/m.jpg","large":"https://cdn.test/l.jpg"},
	"alternative_titles":{"synonyms":[],"en":"Berserk EN","ja":"ベルセルク"},"synopsis":"Guts","mean":9.3,"num_chapters":0,
	"status":"currently_publishing","genres":[{"id":1,"name":"Action"},{"id":10,"name":"Fantasy"}],
	"authors":[{"node":{"id":1868,"first_name":"Kentarou","last_name":"Miura"},"role":"Story & Art"}]}`

func TestMALSearchMappingAuthAndCache(t *testing.T) {
	calls := 0
	p := newTestMAL(func(r *http.Request) (*http.Response, error) {
		calls++
		q := r.URL.Query()
		if r.Header.Get("X-MAL-CLIENT-ID") != "test-client-id" {
			t.Fatal("client ID header missing")
		}
		if r.URL.Path != "/v2/manga" || q.Get("q") != "berserk" || q.Get("limit") != "24" || q.Get("offset") != "24" || q.Get("nsfw") != "false" || !strings.Contains(q.Get("fields"), "authors{first_name,last_name}") {
			t.Fatalf("unexpected request %s", r.URL)
		}
		return respond(200, `{"data":[{"node":`+berserkNode+`}],"paging":{"next":"https://api.myanimelist.net/v2/manga?offset=48"}}`)
	})
	page, err := p.Search(context.Background(), "berserk", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Search(context.Background(), "berserk", 2); err != nil || calls != 1 {
		t.Fatal("cache did not avoid the upstream request")
	}
	m := page.Items[0]
	if len(page.Items) != 1 || !page.HasNextPage || page.Page != 2 {
		t.Fatalf("bad page %+v", page)
	}
	if m.ID != 2 || m.Title != "Berserk EN" || m.CoverURL != "https://cdn.test/l.jpg" || m.Chapters != nil ||
		m.Status != "Publishing" || m.Author != "Kentarou Miura" || *m.Score != 9.3 ||
		strings.Join(m.Genres, ",") != "Action,Fantasy" || m.URL != "https://myanimelist.net/manga/2" {
		t.Fatalf("bad mapping %+v", m)
	}
}

func TestMALPopularAndDetail(t *testing.T) {
	p := newTestMAL(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/v2/manga/ranking":
			if r.URL.Query().Get("ranking_type") != "bypopularity" || r.URL.Query().Get("offset") != "0" {
				t.Fatalf("unexpected ranking request %s", r.URL)
			}
			return respond(200, `{"data":[{"node":`+berserkNode+`,"ranking":{"rank":1}}],"paging":{}}`)
		case "/v2/manga/2":
			return respond(200, strings.Replace(berserkNode, `"num_chapters":0`, `"num_chapters":364`, 1))
		}
		return respond(404, `{"error":"not_found"}`)
	})
	page, err := p.Popular(context.Background(), 1)
	if err != nil || len(page.Items) != 1 || page.HasNextPage {
		t.Fatalf("popular: %v %+v", err, page)
	}
	m, err := p.Detail(context.Background(), 2)
	if err != nil || m.ID != 2 || m.Chapters == nil || *m.Chapters != 364 {
		t.Fatalf("detail: %v %+v", err, m)
	}
	p.next = time.Time{}
	if _, err = p.Detail(context.Background(), 999); err != ErrNotFound {
		t.Fatalf("404 not mapped: %v", err)
	}
}

func TestMALErrorStatuses(t *testing.T) {
	for status, want := range map[int]error{400: ErrInvalidQuery, 401: ErrUnavailable, 403: ErrUnavailable, 500: ErrUnavailable} {
		p := newTestMAL(func(r *http.Request) (*http.Response, error) { return respond(status, `{"error":"x"}`) })
		if _, err := p.Search(context.Background(), "x", 1); err != want {
			t.Fatalf("HTTP %d: got %v, want %v", status, err, want)
		}
	}
	p := newTestMAL(func(r *http.Request) (*http.Response, error) {
		// Observed live response for a wrong client ID.
		return respond(400, `{"message":"Invalid client id","error":"bad_request"}`)
	})
	if _, err := p.Search(context.Background(), "berserk", 1); err != ErrUnavailable {
		t.Fatalf("invalid client ID must be ErrUnavailable, got %v", err)
	}
	p = NewMAL("")
	p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		t.Fatal("no request should be sent without a client ID")
		return nil, nil
	})}
	if _, err := p.Popular(context.Background(), 1); err != ErrUnavailable {
		t.Fatalf("missing client ID: %v", err)
	}
}

func TestMALRejectsInvalidSuccessBodiesWithoutCaching(t *testing.T) {
	for _, body := range []string{`{}`, `{"id":14,"title":"Wrong ID"}`, `{"id":2,"title":" "}`} {
		current := body
		calls := 0
		p := newTestMAL(func(r *http.Request) (*http.Response, error) { calls++; return respond(200, current) })
		if _, err := p.Detail(context.Background(), 2); err != ErrUnavailable {
			t.Fatalf("body %s should fail, got %v", body, err)
		}
		if len(p.cache) != 0 {
			t.Fatal("invalid metadata cached")
		}
		current = berserkNode
		p.next = time.Time{}
		if m, err := p.Detail(context.Background(), 2); err != nil || m.ID != 2 || calls != 2 {
			t.Fatal("valid retry should succeed without a stale cached failure")
		}
	}
	for _, body := range []string{`{}`, `{"data":null}`, `{"data":[{"node":{"title":"No ID"}}]}`} {
		p := newTestMAL(func(r *http.Request) (*http.Response, error) { return respond(200, body) })
		if _, err := p.Search(context.Background(), "query", 1); err != ErrUnavailable {
			t.Fatalf("invalid page %s returned %v", body, err)
		}
	}
	p := newTestMAL(func(r *http.Request) (*http.Response, error) { return respond(200, `{"data":[],"paging":{}}`) })
	if page, err := p.Search(context.Background(), "no results", 1); err != nil || len(page.Items) != 0 {
		t.Fatal("valid empty search rejected")
	}
}

func TestMALSearchEncodingAndAdmissionInterval(t *testing.T) {
	query := "a&nsfw=true#日本?"
	calls := 0
	p := newTestMAL(func(r *http.Request) (*http.Response, error) {
		calls++
		values := r.URL.Query()
		if values.Get("q") != query || len(values["nsfw"]) != 1 || values.Get("nsfw") != "false" {
			t.Fatal("query escaped incorrectly")
		}
		return respond(200, `{"data":[]}`)
	})
	start := time.Now()
	if _, err := p.Search(context.Background(), query, 1); err != nil {
		t.Fatal(err)
	}
	if p.next.Sub(start) < time.Second {
		t.Fatal("upstream admission exceeds 60/minute")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.Search(ctx, "different", 1); err != ErrUnavailable || calls != 1 {
		t.Fatal("cancelled throttle wait should not reach upstream")
	}
}

func TestMALCacheIsNotBlockedByUpstreamCalls(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	p := newTestMAL(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("q") == "slow" {
			close(started)
			<-release
		}
		return respond(200, `{"data":[]}`)
	})
	if _, err := p.Search(context.Background(), "cached", 1); err != nil {
		t.Fatal(err)
	}
	p.next = time.Time{}
	go p.Search(context.Background(), "slow", 1)
	<-started
	done := make(chan error, 1)
	go func() { _, err := p.Search(context.Background(), "cached", 1); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cached response waited for an unrelated upstream call")
	}
	close(release)
}

func TestMALQueueFailsFastWhenFull(t *testing.T) {
	calls := 0
	p := newTestMAL(func(r *http.Request) (*http.Response, error) { calls++; return respond(200, `{"data":[]}`) })
	p.next = time.Now().Add(maxQueueWait + time.Second) // backlog already beyond the limit
	start := time.Now()
	if _, err := p.Search(context.Background(), "queued", 1); err != ErrUnavailable || calls != 0 {
		t.Fatalf("expected immediate ErrUnavailable, got %v after %d calls", err, calls)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("full queue should not wait")
	}
}

func TestMALCacheEvictsOneEntryWhenFull(t *testing.T) {
	p := NewMAL("id")
	for i := range cacheSize {
		p.store(fmt.Sprint(i), []byte("{}"))
	}
	p.store("new", []byte("{}"))
	if len(p.cache) != cacheSize {
		t.Fatalf("cache size %d, want %d", len(p.cache), cacheSize)
	}
	if _, ok := p.cached("new"); !ok {
		t.Fatal("new entry missing")
	}
}
