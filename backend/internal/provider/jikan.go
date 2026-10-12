package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
}

var ErrNotFound = errors.New("manga not found")
var ErrUnavailable = errors.New("catalog temporarily unavailable")

type entry struct {
	data    []byte
	expires time.Time
}
type Jikan struct {
	BaseURL string
	Client  *http.Client
	mu      sync.Mutex
	cache   map[string]entry
	next    time.Time
}

func NewJikan() *Jikan {
	return &Jikan{BaseURL: "https://api.jikan.moe/v4", Client: &http.Client{Timeout: 15 * time.Second}, cache: map[string]entry{}}
}

// Serialize uncached upstream requests at <= 60/minute. Cache avoids repeated requests.
func (p *Jikan) get(ctx context.Context, path string, out interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.cache[path]; ok && time.Now().Before(c.expires) {
		return json.Unmarshal(c.data, out)
	}
	if delay := time.Until(p.next); delay > 0 {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ErrUnavailable
		case <-t.C:
		}
	}
	p.next = time.Now().Add(time.Second + 50*time.Millisecond)
	req, err := http.NewRequestWithContext(ctx, "GET", p.BaseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.Client.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return ErrNotFound
	}
	if resp.StatusCode != 200 {
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
	if len(p.cache) > 500 {
		p.cache = map[string]entry{}
	}
	p.cache[path] = entry{raw, time.Now().Add(30 * time.Minute)}
	return nil
}

type wire struct {
	ID           int      `json:"mal_id"`
	Title        string   `json:"title"`
	TitleEnglish string   `json:"title_english"`
	Synopsis     string   `json:"synopsis"`
	Chapters     *int     `json:"chapters"`
	Status       string   `json:"status"`
	Score        *float64 `json:"score"`
	URL          string   `json:"url"`
	Images       struct {
		JPG struct {
			URL   string `json:"large_image_url"`
			Small string `json:"image_url"`
		} `json:"jpg"`
	} `json:"images"`
	Genres []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Authors []struct {
		Name string `json:"name"`
	} `json:"authors"`
}

func (w wire) manga() repository.Manga {
	title := strings.TrimSpace(w.TitleEnglish)
	if title == "" {
		title = strings.TrimSpace(w.Title)
	}
	cover := w.Images.JPG.URL
	if cover == "" {
		cover = w.Images.JPG.Small
	}
	g := []string{}
	for _, x := range w.Genres {
		g = append(g, x.Name)
	}
	a := []string{}
	for _, x := range w.Authors {
		a = append(a, x.Name)
	}
	return repository.Manga{ID: w.ID, Title: title, Description: w.Synopsis, CoverURL: cover, Chapters: w.Chapters, Status: w.Status, Genres: g, Score: w.Score, Author: strings.Join(a, ", "), URL: w.URL}
}

// A successful HTTP status is insufficient: don't cache or persist missing/mismatched metadata.
type pageResponse struct {
	Data       []wire `json:"data"`
	Pagination struct {
		HasNext bool `json:"has_next_page"`
	} `json:"pagination"`
}

func validManga(w wire) bool {
	return w.ID > 0 && (strings.TrimSpace(w.Title) != "" || strings.TrimSpace(w.TitleEnglish) != "")
}
func (b *pageResponse) valid() bool {
	if b.Data == nil {
		return false
	}
	for _, w := range b.Data {
		if !validManga(w) {
			return false
		}
	}
	return true
}

type detailResponse struct {
	Data       wire `json:"data"`
	expectedID int
}

func (b *detailResponse) valid() bool { return validManga(b.Data) && b.Data.ID == b.expectedID }

func (p *Jikan) page(ctx context.Context, path string, page int) (Page, error) {
	var body pageResponse
	if err := p.get(ctx, path, &body); err != nil {
		return Page{}, err
	}
	items := []repository.Manga{}
	for _, w := range body.Data {
		items = append(items, w.manga())
	}
	return Page{items, page, body.Pagination.HasNext}, nil
}
func (p *Jikan) Search(ctx context.Context, q string, page int) (Page, error) {
	return p.page(ctx, "/manga?limit=24&sfw=true&page="+strconv.Itoa(page)+"&q="+url.QueryEscape(q), page)
}
func (p *Jikan) Popular(ctx context.Context, page int) (Page, error) {
	return p.page(ctx, "/top/manga?filter=bypopularity&limit=24&page="+strconv.Itoa(page), page)
}
func (p *Jikan) Detail(ctx context.Context, id int) (repository.Manga, error) {
	b := detailResponse{expectedID: id}
	if err := p.get(ctx, fmt.Sprintf("/manga/%d/full", id), &b); err != nil {
		return repository.Manga{}, err
	}
	return b.Data.manga(), nil
}
