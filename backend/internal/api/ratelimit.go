package api

import (
	"sync"
	"time"
)

// rateLimiter allows up to limit requests per key in each fixed window.
type rateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	entries map[string]rateEntry
}

type rateEntry struct {
	count int
	start time.Time
}

const maxTrackedClients = 10000

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, entries: map[string]rateEntry{}}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	e := l.entries[key]
	if now.Sub(e.start) > l.window {
		e = rateEntry{start: now}
	}
	e.count++
	l.entries[key] = e
	if len(l.entries) > maxTrackedClients {
		for k, v := range l.entries {
			if now.Sub(v.start) > l.window {
				delete(l.entries, k)
			}
		}
	}
	return e.count <= l.limit
}
