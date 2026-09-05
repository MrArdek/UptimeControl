package httpserver

import (
	"testing"
	"time"
)

func TestFixedWindowLimiter(t *testing.T) {
	now := time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	limiter := newFixedWindowLimiter(2, time.Minute)
	limiter.now = func() time.Time { return now }

	if !limiter.Allow("client") || !limiter.Allow("client") {
		t.Fatal("limiter rejected a request within the configured limit")
	}

	if limiter.Allow("client") {
		t.Fatal("limiter accepted a request above the configured limit")
	}

	now = now.Add(time.Minute)
	if !limiter.Allow("client") {
		t.Fatal("limiter did not reset after the window elapsed")
	}
}
