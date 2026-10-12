package api

import (
	"manga-track/backend/internal/repository"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (s *Server) libraryItems(w http.ResponseWriter, r *http.Request) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	u, e := s.user(r)
	if e != nil {
		return e
	}
	kind := r.URL.Query().Get("kind")
	if kind != "favorites" && kind != "to-read" {
		return fail(400, "kind must be favorites or to-read")
	}
	items := []repository.Manga{}
	if viewErr := s.Store.View(func(st repository.State) {
		for id, l := range st.Libraries[u.ID] {
			if kind == "favorites" && l.Favorite || kind == "to-read" && l.ToRead {
				if m, ok := st.Manga[id]; ok {
					items = append(items, m)
				}
			}
		}
	}); viewErr != nil {
		return viewErr
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Title < items[j].Title })
	write(w, 200, map[string]interface{}{"items": items})
	return nil
}
func (s *Server) library(w http.ResponseWriter, r *http.Request, p []string) error {
	u, e := s.user(r)
	if e != nil {
		return e
	}
	id, e := integer(p[1])
	if e != nil {
		return e
	}
	if len(p) == 2 && r.Method == "GET" {
		l := repository.Library{MangaID: id, Chapters: []repository.Chapter{}}
		if viewErr := s.Store.View(func(st repository.State) {
			if stored, ok := st.Libraries[u.ID][id]; ok {
				l = stored
			}
		}); viewErr != nil {
			return viewErr
		}
		write(w, 200, l)
		return nil
	}
	if r.Method != "PUT" {
		return fail(405, "Method not allowed")
	}
	var b struct {
		Read     *bool             `json:"read"`
		Rating   *int              `json:"rating"`
		Notes    string            `json:"notes"`
		ToRead   *bool             `json:"toRead"`
		Favorite *bool             `json:"favorite"`
		Manga    *repository.Manga `json:"manga"`
	}
	if e = decode(w, r, &b); e != nil {
		return e
	}
	isChapter := len(p) == 4 && p[2] == "chapters"
	if len(p) != 2 && !isChapter {
		return fail(404, "Endpoint not found")
	}
	if isChapter {
		n, err := strconv.ParseFloat(p[3], 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n <= 0 || n > 100000 || strings.ContainsAny(p[3], "eE+-") || len(p[3]) > 12 {
			return fail(400, "Chapter must be a positive number, including decimals")
		}
		p[3] = strconv.FormatFloat(n, 'f', -1, 64)
		if b.Read == nil || len(b.Notes) > 5000 || b.Rating != nil && (*b.Rating < 1 || *b.Rating > 5) {
			return fail(400, "Provide read, optional rating 1–5, and notes up to 5000 characters")
		}
	}
	if e = s.ensureManga(r, id, b.Manga); e != nil {
		return e
	}
	var out repository.Library
	e = s.Store.Update(func(st *repository.State) error {
		if st.Libraries[u.ID] == nil {
			st.Libraries[u.ID] = map[int]repository.Library{}
		}
		l, ok := st.Libraries[u.ID][id]
		if !ok {
			l = repository.Library{MangaID: id, Chapters: []repository.Chapter{}}
		}
		if isChapter {
			index := -1
			for i, c := range l.Chapters {
				if c.Chapter == p[3] {
					index = i
					break
				}
			}
			c := repository.Chapter{Chapter: p[3], Read: *b.Read, Rating: b.Rating, Notes: b.Notes}
			if c.Read {
				now := time.Now().UTC()
				c.ReadAt = &now
				if index >= 0 && l.Chapters[index].ReadAt != nil {
					c.ReadAt = l.Chapters[index].ReadAt
				}
			}
			if index < 0 {
				l.Chapters = append(l.Chapters, c)
			} else {
				l.Chapters[index] = c
			}
			sort.Slice(l.Chapters, func(i, j int) bool {
				a, _ := strconv.ParseFloat(l.Chapters[i].Chapter, 64)
				b, _ := strconv.ParseFloat(l.Chapters[j].Chapter, 64)
				return a < b
			})
		} else {
			if b.ToRead != nil {
				l.ToRead = *b.ToRead
			}
			if b.Favorite != nil {
				l.Favorite = *b.Favorite
			}
		}
		st.Libraries[u.ID][id] = l
		out = l
		return nil
	})
	if e != nil {
		return e
	}
	write(w, 200, out)
	return nil
}

type recent struct {
	Manga   repository.Manga `json:"manga"`
	Chapter string           `json:"chapter"`
	ReadAt  *time.Time       `json:"readAt"`
	Rating  *int             `json:"rating"`
	Notes   string           `json:"notes"`
}

func (s *Server) dashboard(w http.ResponseWriter, r *http.Request) error {
	if e := method(r, "GET"); e != nil {
		return e
	}
	u, e := s.user(r)
	if e != nil {
		return e
	}
	rec := []recent{}
	if viewErr := s.Store.View(func(st repository.State) {
		for id, l := range st.Libraries[u.ID] {
			for _, c := range l.Chapters {
				if c.Read && c.ReadAt != nil {
					rec = append(rec, recent{st.Manga[id], c.Chapter, c.ReadAt, c.Rating, c.Notes})
				}
			}
		}
	}); viewErr != nil {
		return viewErr
	}
	sort.Slice(rec, func(i, j int) bool { return rec[i].ReadAt.After(*rec[j].ReadAt) })
	// The dashboard resumes manga, rather than showing one card per chapter.
	// Keep the latest reading event for each manga; chapter history remains in Library.
	distinct := make([]recent, 0, 12)
	seen := map[int]bool{}
	for _, entry := range rec {
		if !seen[entry.Manga.ID] {
			distinct = append(distinct, entry)
			seen[entry.Manga.ID] = true
			if len(distinct) == 12 {
				break
			}
		}
	}
	rec = distinct
	pop, e := s.Catalog.Popular(r.Context(), 1)
	popular := []repository.Manga{}
	warning := ""
	if e == nil {
		popular = pop.Items
		if e = s.remember(popular); e != nil {
			return e
		}
	} else {
		warning = "Catalog temporarily unavailable. Your reading history is available."
	}
	write(w, 200, map[string]interface{}{"recent": rec, "popular": popular, "catalogWarning": warning})
	return nil
}
