package checknodes

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/MrArdek/UptimeControl/internal/identity"
)

const (
	maximumBatchSize = 100
	maximumEventAge  = 30 * 24 * time.Hour
)

var (
	ErrNotFound        = errors.New("check node or assignment not found")
	ErrUnauthorized    = errors.New("check node is unauthorized")
	ErrInvalidInput    = errors.New("invalid check node input")
	ErrInvalidResult   = errors.New("invalid regional result")
	ErrAlreadyAssigned = errors.New("monitor is already assigned to this node")
)

var regionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)

type Node struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Region      string       `json:"region"`
	Enabled     bool         `json:"enabled"`
	Online      bool         `json:"online"`
	LastSeenAt  *time.Time   `json:"last_seen_at"`
	CreatedAt   time.Time    `json:"created_at"`
	RevokedAt   *time.Time   `json:"revoked_at"`
	Secret      string       `json:"secret,omitempty"`
	Assignments []Assignment `json:"assignments"`
}

type Assignment struct {
	ID                   string     `json:"id"`
	NodeID               string     `json:"node_id"`
	MonitorID            string     `json:"monitor_id"`
	ProjectID            string     `json:"project_id"`
	ProjectName          string     `json:"project_name"`
	MonitorName          string     `json:"monitor_name"`
	Type                 string     `json:"type"`
	URL                  string     `json:"url"`
	Target               string     `json:"target"`
	Region               string     `json:"region"`
	CheckIntervalSeconds int        `json:"check_interval_seconds"`
	TimeoutSeconds       int        `json:"timeout_seconds"`
	LeasedUntil          *time.Time `json:"leased_until,omitempty"`
	LastResultAt         *time.Time `json:"last_result_at"`
}

type Result struct {
	ResultID       string    `json:"result_id"`
	AssignmentID   string    `json:"assignment_id"`
	MonitorID      string    `json:"monitor_id"`
	Region         string    `json:"region"`
	StartedAt      time.Time `json:"started_at"`
	FinishedAt     time.Time `json:"finished_at"`
	Available      bool      `json:"available"`
	StatusCode     *int      `json:"status_code"`
	ResponseTimeMS *int64    `json:"response_time_ms"`
	ErrorCode      *string   `json:"error_code"`
}

type BatchOutcome struct {
	Accepted   int         `json:"accepted"`
	Duplicate  int         `json:"duplicate"`
	Rejected   []Rejection `json:"rejected"`
	ServerTime time.Time   `json:"server_time"`
}

type Rejection struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

type Store interface {
	Create(context.Context, string, Node, []byte) (Node, error)
	List(context.Context, string, time.Time) ([]Node, error)
	Revoke(context.Context, string, string, time.Time) error
	Assign(context.Context, string, string, Assignment, time.Time) (Assignment, error)
	RemoveAssignment(context.Context, string, string, string, time.Time) error
	Authenticate(context.Context, []byte, time.Time) (Node, error)
	ClaimAssignments(context.Context, string, time.Time, int) ([]Assignment, error)
	RecordResults(context.Context, Node, []Result, time.Time) (BatchOutcome, error)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service { return &Service{store: store, now: time.Now} }

func (service *Service) Create(ctx context.Context, userID, name, region string) (Node, error) {
	name = strings.TrimSpace(name)
	region = strings.ToLower(strings.TrimSpace(region))
	if name == "" || len([]rune(name)) > 100 || !regionPattern.MatchString(region) {
		return Node{}, ErrInvalidInput
	}
	id, err := identity.NewUUID()
	if err != nil {
		return Node{}, fmt.Errorf("generate node ID: %w", err)
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return Node{}, fmt.Errorf("generate node secret: %w", err)
	}
	secret := base64.RawURLEncoding.EncodeToString(raw)
	hash := sha256.Sum256([]byte(secret))
	now := service.now().UTC()
	node, err := service.store.Create(ctx, userID, Node{ID: id, Name: name, Region: region, Enabled: true, CreatedAt: now}, hash[:])
	if err != nil {
		return Node{}, err
	}
	node.Secret = secret
	return node, nil
}

func (service *Service) List(ctx context.Context, userID string) ([]Node, error) {
	return service.store.List(ctx, userID, service.now().UTC())
}

func (service *Service) Revoke(ctx context.Context, userID, nodeID string) error {
	if !identity.ValidUUID(nodeID) {
		return ErrNotFound
	}
	return service.store.Revoke(ctx, userID, nodeID, service.now().UTC())
}

func (service *Service) Assign(ctx context.Context, userID, nodeID, monitorID string) (Assignment, error) {
	if !identity.ValidUUID(nodeID) || !identity.ValidUUID(monitorID) {
		return Assignment{}, ErrNotFound
	}
	id, err := identity.NewUUID()
	if err != nil {
		return Assignment{}, fmt.Errorf("generate assignment ID: %w", err)
	}
	return service.store.Assign(ctx, userID, nodeID, Assignment{ID: id, NodeID: nodeID, MonitorID: monitorID}, service.now().UTC())
}

func (service *Service) RemoveAssignment(ctx context.Context, userID, nodeID, assignmentID string) error {
	if !identity.ValidUUID(nodeID) || !identity.ValidUUID(assignmentID) {
		return ErrNotFound
	}
	return service.store.RemoveAssignment(ctx, userID, nodeID, assignmentID, service.now().UTC())
}

func (service *Service) Authenticate(ctx context.Context, secret string) (Node, error) {
	if len(secret) < 32 || len(secret) > 128 {
		return Node{}, ErrUnauthorized
	}
	hash := sha256.Sum256([]byte(secret))
	return service.store.Authenticate(ctx, hash[:], service.now().UTC())
}

func (service *Service) ClaimAssignments(ctx context.Context, node Node, limit int) ([]Assignment, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	return service.store.ClaimAssignments(ctx, node.ID, service.now().UTC(), limit)
}

func (service *Service) RecordResults(ctx context.Context, node Node, results []Result) (BatchOutcome, error) {
	now := service.now().UTC()
	if len(results) == 0 || len(results) > maximumBatchSize {
		return BatchOutcome{}, ErrInvalidResult
	}
	seen := make(map[string]struct{}, len(results))
	valid := make([]Result, 0, len(results))
	outcome := BatchOutcome{Rejected: []Rejection{}, ServerTime: now}
	for index := range results {
		result := &results[index]
		result.Region = strings.ToLower(strings.TrimSpace(result.Region))
		if _, duplicate := seen[result.ResultID]; duplicate {
			outcome.Duplicate++
			continue
		}
		seen[result.ResultID] = struct{}{}
		if !validResult(node, *result, now) {
			outcome.Rejected = append(outcome.Rejected, Rejection{ID: result.ResultID, Code: "invalid_result"})
			continue
		}
		result.StartedAt = result.StartedAt.UTC()
		result.FinishedAt = result.FinishedAt.UTC()
		if result.ErrorCode != nil {
			value := strings.ToLower(strings.TrimSpace(*result.ErrorCode))
			result.ErrorCode = &value
		}
		valid = append(valid, *result)
	}
	if len(valid) > 0 {
		stored, err := service.store.RecordResults(ctx, node, valid, now)
		if err != nil {
			return BatchOutcome{}, err
		}
		outcome.Accepted += stored.Accepted
		outcome.Duplicate += stored.Duplicate
		outcome.Rejected = append(outcome.Rejected, stored.Rejected...)
	}
	return outcome, nil
}

func validResult(node Node, result Result, now time.Time) bool {
	if !identity.ValidUUID(result.ResultID) || !identity.ValidUUID(result.AssignmentID) || !identity.ValidUUID(result.MonitorID) || result.Region != node.Region {
		return false
	}
	if result.StartedAt.IsZero() || result.FinishedAt.Before(result.StartedAt) || result.FinishedAt.After(now.Add(5*time.Minute)) || result.FinishedAt.Before(now.Add(-maximumEventAge)) {
		return false
	}
	if result.StatusCode != nil && (*result.StatusCode < 100 || *result.StatusCode > 599) {
		return false
	}
	if result.ResponseTimeMS != nil && (*result.ResponseTimeMS < 0 || *result.ResponseTimeMS > 120_000) {
		return false
	}
	if result.Available && result.ErrorCode != nil && *result.ErrorCode != "" {
		return false
	}
	if result.ErrorCode != nil && !validErrorCode(*result.ErrorCode) {
		return false
	}
	return true
}

func validErrorCode(value string) bool {
	switch value {
	case "", "request_timeout", "dns_failure", "connection_refused", "tls_failure", "blocked_target", "connection_failed", "unexpected_status":
		return true
	default:
		return false
	}
}
