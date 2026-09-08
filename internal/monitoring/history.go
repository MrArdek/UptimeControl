package monitoring

import (
	"errors"
	"strconv"
	"time"
)

const maximumHistoryRange = 31 * 24 * time.Hour

var (
	ErrInvalidCursor = errors.New("invalid history cursor")
	ErrInvalidPeriod = errors.New("invalid history period")
)

type HistoryQuery struct {
	Limit  int
	Cursor string
	From   time.Time
	To     time.Time
}

type CheckPage struct {
	Checks     []Check   `json:"checks"`
	NextCursor *string   `json:"next_cursor"`
	From       time.Time `json:"from"`
	To         time.Time `json:"to"`
}

type IncidentPage struct {
	Incidents  []Incident `json:"incidents"`
	NextCursor *string    `json:"next_cursor"`
	From       time.Time  `json:"from"`
	To         time.Time  `json:"to"`
}

type MonitorSummary struct {
	From                     time.Time `json:"from"`
	To                       time.Time `json:"to"`
	Status                   string    `json:"status"`
	UptimePercent            *float64  `json:"uptime_percent"`
	CoveragePercent          float64   `json:"coverage_percent"`
	ObservedDurationSeconds  int64     `json:"observed_duration_seconds"`
	AvailableDurationSeconds int64     `json:"available_duration_seconds"`
	CheckCount               int       `json:"check_count"`
	AverageResponseTimeMS    *float64  `json:"average_response_time_ms"`
	PeakResponseTimeMS       *int64    `json:"peak_response_time_ms"`
	LastIncident             *Incident `json:"last_incident"`
	GeneratedAt              time.Time `json:"generated_at"`
}

func NewHistoryQuery(limit int, cursor, fromValue, toValue string, now time.Time) (HistoryQuery, error) {
	now = now.UTC()
	to := now
	var err error
	if toValue != "" {
		to, err = time.Parse(time.RFC3339, toValue)
		if err != nil {
			return HistoryQuery{}, ErrInvalidPeriod
		}
		to = to.UTC()
	}
	from := to.Add(-24 * time.Hour)
	if fromValue != "" {
		from, err = time.Parse(time.RFC3339, fromValue)
		if err != nil {
			return HistoryQuery{}, ErrInvalidPeriod
		}
		from = from.UTC()
	}
	if !from.Before(to) || to.Sub(from) > maximumHistoryRange || to.After(now.Add(5*time.Minute)) {
		return HistoryQuery{}, ErrInvalidPeriod
	}
	return HistoryQuery{Limit: normalizeLimit(limit), Cursor: cursor, From: from, To: to}, nil
}

func historyScope(kind, userID, projectID, monitorID string, query HistoryQuery) string {
	return kind + ":" + userID + ":" + projectID + ":" + monitorID + ":" +
		query.From.Format(time.RFC3339Nano) + ":" + query.To.Format(time.RFC3339Nano)
}

func checkCursorID(id int64) string {
	return strconv.FormatInt(id, 10)
}

func staleAfter(checkIntervalSeconds int) time.Duration {
	grace := 2 * time.Duration(checkIntervalSeconds) * time.Second
	if grace < 5*time.Minute {
		return 5 * time.Minute
	}
	return grace
}

func calculateSummary(
	query HistoryQuery,
	now time.Time,
	enabled bool,
	checkIntervalSeconds int,
	lastCheckedAt *time.Time,
	lastAvailable *bool,
	checks []Check,
	lastIncident *Incident,
) MonitorSummary {
	summary := MonitorSummary{
		From:         query.From,
		To:           query.To,
		Status:       "unknown",
		LastIncident: lastIncident,
		GeneratedAt:  now.UTC(),
	}
	if !enabled {
		summary.Status = "paused"
	} else if lastCheckedAt != nil && lastAvailable != nil {
		if now.UTC().After(lastCheckedAt.UTC().Add(staleAfter(checkIntervalSeconds))) {
			summary.Status = "stale"
		} else if *lastAvailable {
			summary.Status = "up"
		} else {
			summary.Status = "down"
		}
	}

	staleGrace := staleAfter(checkIntervalSeconds)
	var responseSum int64
	var responseCount int64
	for index, check := range checks {
		segmentStart := check.CheckedAt.UTC()
		if segmentStart.Before(query.From) {
			segmentStart = query.From
		}
		segmentEnd := check.CheckedAt.UTC().Add(staleGrace)
		if index+1 < len(checks) && checks[index+1].CheckedAt.Before(segmentEnd) {
			segmentEnd = checks[index+1].CheckedAt.UTC()
		}
		if segmentEnd.After(query.To) {
			segmentEnd = query.To
		}
		if segmentEnd.After(segmentStart) {
			duration := segmentEnd.Sub(segmentStart)
			summary.ObservedDurationSeconds += int64(duration / time.Second)
			if check.Available {
				summary.AvailableDurationSeconds += int64(duration / time.Second)
			}
		}
		if !check.CheckedAt.Before(query.From) && check.CheckedAt.Before(query.To) {
			summary.CheckCount++
			if check.ResponseTimeMS != nil {
				responseSum += *check.ResponseTimeMS
				responseCount++
				if summary.PeakResponseTimeMS == nil || *check.ResponseTimeMS > *summary.PeakResponseTimeMS {
					peak := *check.ResponseTimeMS
					summary.PeakResponseTimeMS = &peak
				}
			}
		}
	}
	periodSeconds := int64(query.To.Sub(query.From) / time.Second)
	if periodSeconds > 0 {
		summary.CoveragePercent = 100 * float64(summary.ObservedDurationSeconds) / float64(periodSeconds)
	}
	if summary.ObservedDurationSeconds > 0 {
		uptime := 100 * float64(summary.AvailableDurationSeconds) / float64(summary.ObservedDurationSeconds)
		summary.UptimePercent = &uptime
	}
	if responseCount > 0 {
		average := float64(responseSum) / float64(responseCount)
		summary.AverageResponseTimeMS = &average
	}
	return summary
}
