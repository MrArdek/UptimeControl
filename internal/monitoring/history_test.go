package monitoring

import (
	"errors"
	"testing"
	"time"
)

func TestNewHistoryQueryValidatesRange(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	query, err := NewHistoryQuery(1000, "", "2026-09-07T12:00:00Z", "2026-09-08T12:00:00Z", now)
	if err != nil {
		t.Fatalf("NewHistoryQuery(): %v", err)
	}
	if query.Limit != maximumHistoryItems {
		t.Fatalf("limit = %d, want %d", query.Limit, maximumHistoryItems)
	}
	if _, err := NewHistoryQuery(10, "", "2026-01-01T00:00:00Z", "2026-09-08T12:00:00Z", now); !errors.Is(err, ErrInvalidPeriod) {
		t.Fatalf("long range error = %v", err)
	}
}

func TestCalculateSummaryMatchesControlSequence(t *testing.T) {
	from := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	to := from.Add(20 * time.Minute)
	response100 := int64(100)
	response300 := int64(300)
	available := true
	lastChecked := from.Add(10 * time.Minute)
	checks := []Check{
		{ID: 1, CheckedAt: from, Available: true, ResponseTimeMS: &response100},
		{ID: 2, CheckedAt: from.Add(5 * time.Minute), Available: false, ResponseTimeMS: &response300},
		{ID: 3, CheckedAt: lastChecked, Available: true, ResponseTimeMS: &response300},
	}
	summary := calculateSummary(
		HistoryQuery{From: from, To: to}, to, true, 30, &lastChecked, &available, checks, nil,
	)
	if summary.ObservedDurationSeconds != 15*60 || summary.AvailableDurationSeconds != 10*60 {
		t.Fatalf("durations = observed %d available %d", summary.ObservedDurationSeconds, summary.AvailableDurationSeconds)
	}
	if summary.UptimePercent == nil || *summary.UptimePercent < 66.66 || *summary.UptimePercent > 66.67 {
		t.Fatalf("uptime = %v", summary.UptimePercent)
	}
	if summary.CoveragePercent != 75 || summary.Status != "stale" {
		t.Fatalf("coverage/status = %.2f/%s", summary.CoveragePercent, summary.Status)
	}
	if summary.AverageResponseTimeMS == nil || *summary.AverageResponseTimeMS != 700.0/3.0 || summary.PeakResponseTimeMS == nil || *summary.PeakResponseTimeMS != 300 {
		t.Fatalf("response summary = average %v peak %v", summary.AverageResponseTimeMS, summary.PeakResponseTimeMS)
	}
}
