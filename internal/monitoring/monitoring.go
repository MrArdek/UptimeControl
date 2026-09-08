package monitoring

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("monitor not found")

type DueMonitor struct {
	ID             string
	ProjectID      string
	ProjectName    string
	Name           string
	Type           string
	URL            string
	Target         string
	TimeoutSeconds int
}

type Result struct {
	CheckedAt      time.Time `json:"checked_at"`
	Available      bool      `json:"available"`
	StatusCode     *int      `json:"status_code"`
	ResponseTimeMS *int64    `json:"response_time_ms"`
	Error          *string   `json:"error"`
}

type Check struct {
	ID             int64     `json:"id"`
	CheckedAt      time.Time `json:"checked_at"`
	Available      bool      `json:"available"`
	StatusCode     *int      `json:"status_code"`
	ResponseTimeMS *int64    `json:"response_time_ms"`
	Error          *string   `json:"error"`
}

type Incident struct {
	ID         string     `json:"id"`
	StartedAt  time.Time  `json:"started_at"`
	ResolvedAt *time.Time `json:"resolved_at"`
	Cause      *string    `json:"cause"`
}

type Transition struct {
	Kind        string
	ProjectID   string
	ProjectName string
	MonitorID   string
	MonitorName string
	URL         string
	Target      string
	OccurredAt  time.Time
	Cause       string
}

func (transition Transition) Empty() bool {
	return transition.Kind == ""
}

type Store interface {
	ClaimDue(context.Context, time.Time, int) ([]DueMonitor, error)
	RecordResult(context.Context, DueMonitor, Result) (Transition, error)
	RecordHeartbeat(context.Context, []byte, time.Time) (Transition, error)
	Checks(context.Context, string, string, string, int) ([]Check, error)
	Incidents(context.Context, string, string, string, int) ([]Incident, error)
}

type Checker interface {
	Check(context.Context, DueMonitor) Result
}

type Notifier interface {
	Notify(context.Context, Transition) error
}
