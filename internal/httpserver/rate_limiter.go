package httpserver

import (
	"net"
	"net/http"
	"sync"
	"time"
)

const maximumLimiterEntries = 10_000

type fixedWindowEntry struct {
	count       int
	windowStart time.Time
}

type fixedWindowLimiter struct {
	mutex   sync.Mutex
	entries map[string]fixedWindowEntry
	limit   int
	window  time.Duration
	now     func() time.Time
}

func newFixedWindowLimiter(limit int, window time.Duration) *fixedWindowLimiter {
	return &fixedWindowLimiter{
		entries: make(map[string]fixedWindowEntry),
		limit:   limit,
		window:  window,
		now:     time.Now,
	}
}

func (limiter *fixedWindowLimiter) Allow(key string) bool {
	limiter.mutex.Lock()
	defer limiter.mutex.Unlock()

	now := limiter.now()
	entry, exists := limiter.entries[key]
	if !exists || now.Sub(entry.windowStart) >= limiter.window {
		if len(limiter.entries) >= maximumLimiterEntries {
			limiter.deleteExpired(now)
			if len(limiter.entries) >= maximumLimiterEntries {
				return false
			}
		}

		limiter.entries[key] = fixedWindowEntry{count: 1, windowStart: now}
		return true
	}

	if entry.count >= limiter.limit {
		return false
	}

	entry.count++
	limiter.entries[key] = entry
	return true
}

func (limiter *fixedWindowLimiter) deleteExpired(now time.Time) {
	for key, entry := range limiter.entries {
		if now.Sub(entry.windowStart) >= limiter.window {
			delete(limiter.entries, key)
		}
	}
}

func clientIP(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err == nil {
		return host
	}

	return request.RemoteAddr
}
