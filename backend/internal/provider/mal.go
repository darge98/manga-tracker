// Package provider reads manga metadata from the official MyAnimeList API v2
// (https://myanimelist.net/apiconfig/references/api/v2).
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"manga-track/backend/internal/repository"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Page struct {
	Items       []repository.Manga `json:"items"`
	Page        int                `json:"page"`
	HasNextPage bool               `json:"hasNextPage"`
}

type Catalog interface {
	Search(context.Context, string, int) (Page, error)
	Popular(context.Context, int) (Page, error)
	Detail(context.Context, int) (repository.Manga, error)
	// Recommendations returns what MyAnimeList users recommend to readers of
	// the manga, safe-for-work titles only.
	Recommendations(context.Context, int) ([]Recommendation, error)
}

// Recommendation is a manga MyAnimeList users suggest, with how many did.
type Recommendation struct {
	Manga repository.Manga
	Votes int
}

var (
	ErrNotFound     = errors.New("manga not found")
	ErrUnavailable  = errors.New("catalog temporarily unavailable")
	ErrInvalidQuery = errors.New("catalog rejected the search")
)

type entry struct {
	data    []byte
	expires time.Time
}

const (
	pageSize     = 24
	cacheTTL     = 30 * time.Minute
	cacheSize    = 500
	interval     = time.Second + 50*time.Millisecond // MAL publishes no limit: stay well under 60/minute
	maxQueueWait = 10 * time.Second
	// fields lists the manga properties requested from MAL; it omits nothing the app shows.
	fields = "alternative_titles,synopsis,main_picture,num_chapters,status,genres,mean,authors{first_name,last_name}"
	// The detail request also returns recommendations with full card data,
	// so the detail page and recommendations share one cached call.
	detailFields = fields + ",recommendations{alternative_titles,main_picture,num_chapters,status,genres,mean,nsfw}"
)

type MAL struct {
	BaseURL  string
	ClientID string
	Client   *http.Client

	cacheMu sync.RWMutex
	cache   map[string]entry

	slotMu sync.Mutex
	next   time.Time // earliest start of the next upstream request
}

func NewMAL(clientID string) *MAL {
	return &MAL{BaseURL: "https://api.myanimelist.net/v2", ClientID: clientID, Client: &http.Client{Timeout: 15 * time.Second}, cache: map[string]entry{}}
}

func (p *MAL) cached(path string) ([]byte, bool) {
	p.cacheMu.RLock()
	defer p.cacheMu.RUnlock()
	c, ok := p.cache[path]
	return c.data, ok && time.Now().Before(c.expires)
}

// store evicts expired entries when the cache is full, then the entry closest
// to expiry, instead of dropping the whole cache.
func (p *MAL) store(path string, data []byte) {
	p.cacheMu.Lock()
	defer p.cacheMu.Unlock()
	now := time.Now()
	if _, exists := p.cache[path]; !exists && len(p.cache) >= cacheSize {
		oldest := ""
		for key, c := range p.cache {
			if now.After(c.expires) {
				delete(p.cache, key)
			} else if oldest == "" || c.expires.Before(p.cache[oldest].expires) {
				oldest = key
			}
		}
		if len(p.cache) >= cacheSize {
			delete(p.cache, oldest)
		}
	}
	p.cache[path] = entry{data, now.Add(cacheTTL)}
}

// reserve books the next upstream slot and waits for it. Requests that would
// queue longer than maxQueueWait fail fast instead of piling up.
func (p *MAL) reserve(ctx context.Context) error {
	p.slotMu.Lock()
	now := time.Now()
	slot := p.next
	if slot.Before(now) {
		slot = now
	}
	if slot.Sub(now) > maxQueueWait {
		p.slotMu.Unlock()
		return ErrUnavailable
	}
	p.next = slot.Add(interval)
	p.slotMu.Unlock()

	if delay := time.Until(slot); delay > 0 {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ErrUnavailable
		case <-t.C:
		}
	}
	return nil
}

// get serves cached responses without waiting; only uncached requests are
// throttled, and no lock is held during the upstream HTTP call.
func (p *MAL) get(ctx context.Context, path string, out any) error {
	if data, ok := p.cached(path); ok {
		return json.Unmarshal(data, out)
	}
	if p.ClientID == "" {
		return ErrUnavailable
	}
	if err := p.reserve(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", p.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-MAL-CLIENT-ID", p.ClientID)
	resp, err := p.Client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusBadRequest:
		// MAL also answers 400 for a wrong client ID: {"error":"bad_request","message":"Invalid client id"}.
		var body struct {
			Message string `json:"message"`
		}
		_ = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 64<<10)).Decode(&body)
		if strings.Contains(strings.ToLower(body.Message), "client id") {
			log.Print("MyAnimeList rejected MAL_CLIENT_ID: check the client ID")
			return ErrUnavailable
		}
		return ErrInvalidQuery
	default:
		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			log.Printf("MyAnimeList refused the request (HTTP %d): check MAL_CLIENT_ID", resp.StatusCode)
		}
		return ErrUnavailable
	}
	var raw json.RawMessage
	if err = json.NewDecoder(http.MaxBytesReader(nil, resp.Body, 8<<20)).Decode(&raw); err != nil {
		return ErrUnavailable
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return ErrUnavailable
	}
	if response, ok := out.(interface{ valid() bool }); ok && !response.valid() {
		return ErrUnavailable
	}
	p.store(path, raw)
	return nil
}

type wire struct {
	ID                int    `json:"id"`
	Title             string `json:"title"`
	AlternativeTitles struct {
		English string `json:"en"`
	} `json:"alternative_titles"`
	Synopsis    string `json:"synopsis"`
	MainPicture struct {
		Medium string `json:"medium"`
		Large  string `json:"large"`
	} `json:"main_picture"`
	NumChapters int      `json:"num_chapters"` // 0 when unknown
	NSFW        string   `json:"nsfw"`         // white, gray or black
	Status      string   `json:"status"`
	Mean        *float64 `json:"mean"`
	Genres      []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Authors []struct {
		Node struct {
			FirstName string `json:"first_name"`
			LastName  string `json:"last_name"`
		} `json:"node"`
	} `json:"authors"`
}

var statusLabels = map[string]string{
	"currently_publishing": "Publishing",
	"finished":             "Finished",
	"not_yet_published":    "Not yet published",
	"on_hiatus":            "On hiatus",
	"discontinued":         "Discontinued",
}

func (w wire) manga() repository.Manga {
	title := strings.TrimSpace(w.AlternativeTitles.English)
	if title == "" {
		title = strings.TrimSpace(w.Title)
	}
	cover := w.MainPicture.Large
	if cover == "" {
		cover = w.MainPicture.Medium
	}
	var chapters *int
	if w.NumChapters > 0 {
		n := w.NumChapters
		chapters = &n
	}
	status, ok := statusLabels[w.Status]
	if !ok {
		status = strings.ReplaceAll(w.Status, "_", " ")
	}
	genres := []string{}
	for _, g := range w.Genres {
		genres = append(genres, g.Name)
	}
	authors := []string{}
	for _, a := range w.Authors {
		if name := strings.TrimSpace(a.Node.FirstName + " " + a.Node.LastName); name != "" {
			authors = append(authors, name)
		}
	}
	return repository.Manga{
		ID: w.ID, Title: title, Description: w.Synopsis, CoverURL: cover, Chapters: chapters,
		Status: status, Genres: genres, Score: w.Mean, Author: strings.Join(authors, ", "),
		URL: fmt.Sprintf("https://myanimelist.net/manga/%d", w.ID),
	}
}

// A successful HTTP status is insufficient: don't cache or persist missing/mismatched metadata.
func validManga(w wire) bool {
	return w.ID > 0 && (strings.TrimSpace(w.Title) != "" || strings.TrimSpace(w.AlternativeTitles.English) != "")
}

type pageResponse struct {
	Data []struct {
		Node wire `json:"node"`
	} `json:"data"`
	Paging struct {
		Next string `json:"next"`
	} `json:"paging"`
}

func (b *pageResponse) valid() bool {
	if b.Data == nil {
		return false
	}
	for _, d := range b.Data {
		if !validManga(d.Node) {
			return false
		}
	}
	return true
}

type detailResponse struct {
	wire
	Recommendations []struct {
		Node  wire `json:"node"`
		Votes int  `json:"num_recommendations"`
	} `json:"recommendations"`
	expectedID int
}

func (b *detailResponse) valid() bool { return validManga(b.wire) && b.ID == b.expectedID }

func (p *MAL) page(ctx context.Context, path string, query url.Values, page int) (Page, error) {
	query.Set("limit", strconv.Itoa(pageSize))
	query.Set("offset", strconv.Itoa((page-1)*pageSize))
	query.Set("fields", fields)
	var body pageResponse
	if err := p.get(ctx, path+"?"+query.Encode(), &body); err != nil {
		return Page{}, err
	}
	items := []repository.Manga{}
	for _, d := range body.Data {
		items = append(items, d.Node.manga())
	}
	return Page{items, page, body.Paging.Next != ""}, nil
}

// Search excludes NSFW titles (MAL's default for nsfw=false).
func (p *MAL) Search(ctx context.Context, q string, page int) (Page, error) {
	return p.page(ctx, "/manga", url.Values{"q": {q}, "nsfw": {"false"}}, page)
}

func (p *MAL) Popular(ctx context.Context, page int) (Page, error) {
	return p.page(ctx, "/manga/ranking", url.Values{"ranking_type": {"bypopularity"}, "nsfw": {"false"}}, page)
}

func (p *MAL) detail(ctx context.Context, id int) (detailResponse, error) {
	b := detailResponse{expectedID: id}
	err := p.get(ctx, fmt.Sprintf("/manga/%d?fields=%s", id, url.QueryEscape(detailFields)), &b)
	return b, err
}

func (p *MAL) Detail(ctx context.Context, id int) (repository.Manga, error) {
	b, err := p.detail(ctx, id)
	if err != nil {
		return repository.Manga{}, err
	}
	return b.manga(), nil
}

func (p *MAL) Recommendations(ctx context.Context, id int) ([]Recommendation, error) {
	b, err := p.detail(ctx, id)
	if err != nil {
		return nil, err
	}
	recs := []Recommendation{}
	for _, r := range b.Recommendations {
		if r.Node.NSFW == "white" && validManga(r.Node) {
			recs = append(recs, Recommendation{r.Node.manga(), r.Votes})
		}
	}
	return recs, nil
}
