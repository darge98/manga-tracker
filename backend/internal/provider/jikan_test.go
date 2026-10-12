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
func TestJikanMappingNullableFieldsCacheAndError(t *testing.T) {
	calls := 0
	p := NewJikan()
	p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Query().Get("q") != "one piece" {
			t.Fatal("search encoding")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[{"mal_id":13,"title":"One Piece","title_english":null,"chapters":null,"score":9.2,"synopsis":"Pirates","images":{"jpg":{"large_image_url":"https://cdn.test/cover.jpg"}},"genres":[{"name":"Adventure"}],"authors":[{"name":"Oda"}]}],"pagination":{"has_next_page":true}}`))}, nil
	})}
	a, e := p.Search(context.Background(), "one piece", 1)
	if e != nil {
		t.Fatal(e)
	}
	_, e = p.Search(context.Background(), "one piece", 1)
	if e != nil || calls != 1 {
		t.Fatal("cache did not avoid upstream request")
	}
	if len(a.Items) != 1 || a.Items[0].ID != 13 || a.Items[0].Title != "One Piece" || a.Items[0].Chapters != nil || !a.HasNextPage || a.Items[0].Author != "Oda" {
		t.Fatalf("bad mapping %+v", a)
	}
	p.next = time.Time{}
	p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 429, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	if _, e = p.Detail(context.Background(), 999); e != ErrUnavailable {
		t.Fatalf("unexpected rate-limit error %v", e)
	}
	p.next = time.Time{}
	p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}
	if _, e = p.Detail(context.Background(), 999); e != ErrNotFound {
		t.Fatal("404 not mapped")
	}
}

func TestJikanRejectsInvalidSuccessBodiesWithoutCaching(t *testing.T) {
	for _, body := range []string{`{}`, `{"data":null}`, `{"data":{"mal_id":14,"title":"Wrong ID"}}`, `{"data":{"mal_id":13,"title":" "}}`} {
		p := NewJikan()
		current := body
		calls := 0
		p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			calls++
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(current))}, nil
		})}
		if _, e := p.Detail(context.Background(), 13); e != ErrUnavailable {
			t.Fatalf("body %s should fail, got %v", body, e)
		}
		if len(p.cache) != 0 {
			t.Fatal("invalid metadata cached")
		}
		current = `{"data":{"mal_id":13,"title":"One Piece","chapters":null}}`
		p.next = time.Time{}
		if m, e := p.Detail(context.Background(), 13); e != nil || m.ID != 13 || calls != 2 {
			t.Fatal("valid retry should succeed without stale invalid cache")
		}
	}
	for _, body := range []string{`{}`, `{"data":null}`, `{"data":[{"title":"No ID"}]}`} {
		p := NewJikan()
		p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
		if _, e := p.Search(context.Background(), "query", 1); e != ErrUnavailable {
			t.Fatalf("invalid page %s returned %v", body, e)
		}
		if len(p.cache) != 0 {
			t.Fatal("invalid page cached")
		}
	}
	p := NewJikan()
	p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[],"pagination":{"has_next_page":false}}`))}, nil
	})}
	if page, e := p.Search(context.Background(), "no results", 1); e != nil || len(page.Items) != 0 {
		t.Fatal("valid empty search rejected")
	}
}
func TestJikanSearchEncodingAndAdmissionInterval(t *testing.T) {
	p := NewJikan()
	query := "a&sfw=false#日本?"
	calls := 0
	p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		values := r.URL.Query()
		if values.Get("q") != query || values.Get("sfw") != "true" || values.Get("page") != "2" || len(values["sfw"]) != 1 {
			t.Fatal("query escaped incorrectly")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})}
	start := time.Now()
	if _, e := p.Search(context.Background(), query, 2); e != nil {
		t.Fatal(e)
	}
	if p.next.Sub(start) < time.Second {
		t.Fatal("upstream admission exceeds 60/minute")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := p.Search(ctx, "different", 2); e != ErrUnavailable || calls != 1 {
		t.Fatal("cancelled throttle wait should not reach upstream")
	}
}

func TestJikanCacheIsNotBlockedByUpstreamCalls(t *testing.T) {
	p := NewJikan()
	release := make(chan struct{})
	started := make(chan struct{})
	p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("q") == "slow" {
			close(started)
			<-release
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})}
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

func TestJikanQueueFailsFastWhenFull(t *testing.T) {
	p := NewJikan()
	calls := 0
	p.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})}
	p.next = time.Now().Add(maxQueueWait + time.Second) // backlog already beyond the limit
	start := time.Now()
	if _, err := p.Search(context.Background(), "queued", 1); err != ErrUnavailable || calls != 0 {
		t.Fatalf("expected immediate ErrUnavailable, got %v after %d calls", err, calls)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Fatal("full queue should not wait")
	}
}

func TestJikanCacheEvictsOneEntryWhenFull(t *testing.T) {
	p := NewJikan()
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
