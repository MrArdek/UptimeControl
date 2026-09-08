package projects

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MrArdek/UptimeControl/internal/identity"
	"github.com/MrArdek/UptimeControl/internal/netpolicy"
)

const (
	maximumNameCharacters        = 100
	maximumDescriptionCharacters = 2000
	minimumCheckInterval         = 30
	maximumCheckInterval         = 86400
	defaultCheckInterval         = 60
	minimumTimeout               = 1
	maximumTimeout               = 30
	defaultTimeout               = 10
)

var (
	ErrInvalidName        = errors.New("invalid name")
	ErrInvalidDescription = errors.New("invalid description")
	ErrInvalidURL         = errors.New("invalid monitor URL")
	ErrInvalidTarget      = errors.New("invalid monitor target")
	ErrInvalidType        = errors.New("invalid monitor type")
	ErrInvalidInterval    = errors.New("invalid check interval")
	ErrInvalidTimeout     = errors.New("invalid timeout")
	ErrEmptyUpdate        = errors.New("update has no fields")
	ErrNotFound           = errors.New("project or monitor not found")
	ErrAlreadyExists      = errors.New("monitor already exists")
)

type Project struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Monitors    []Monitor `json:"monitors"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type Monitor struct {
	ID                   string     `json:"id"`
	ProjectID            string     `json:"project_id"`
	Type                 string     `json:"type"`
	Name                 string     `json:"name"`
	URL                  string     `json:"url"`
	Target               string     `json:"target,omitempty"`
	CheckIntervalSeconds int        `json:"check_interval_seconds"`
	TimeoutSeconds       int        `json:"timeout_seconds"`
	Enabled              bool       `json:"enabled"`
	LastCheckedAt        *time.Time `json:"last_checked_at"`
	LastAvailable        *bool      `json:"last_available"`
	LastStatusCode       *int       `json:"last_status_code"`
	LastResponseTimeMS   *int64     `json:"last_response_time_ms"`
	LastError            *string    `json:"last_error"`
	HeartbeatToken       string     `json:"heartbeat_token,omitempty"`
	HeartbeatTokenHash   []byte     `json:"-"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type CreateProjectInput struct {
	Name        string
	Description string
	Monitor     CreateMonitorInput
}

type UpdateProjectInput struct {
	Name        *string
	Description *string
}

type CreateMonitorInput struct {
	Type                 string
	Name                 string
	URL                  string
	Target               string
	CheckIntervalSeconds *int
	TimeoutSeconds       *int
}

type UpdateMonitorInput struct {
	Name                 *string
	URL                  *string
	Target               *string
	CheckIntervalSeconds *int
	TimeoutSeconds       *int
	Enabled              *bool
}

type Store interface {
	List(context.Context, string) ([]Project, error)
	ByID(context.Context, string, string) (Project, error)
	Create(context.Context, string, Project, Monitor) error
	UpdateProject(context.Context, string, string, UpdateProjectInput) (Project, error)
	SoftDeleteProject(context.Context, string, string) error
	AddMonitor(context.Context, string, string, Monitor) (Monitor, error)
	UpdateMonitor(context.Context, string, string, string, UpdateMonitorInput) (Monitor, error)
	SoftDeleteMonitor(context.Context, string, string, string) error
	ListWebhooks(context.Context, string, string) ([]Webhook, error)
	CreateWebhook(context.Context, string, string, Webhook, []byte) (Webhook, error)
	SoftDeleteWebhook(context.Context, string, string, string) error
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (service *Service) List(ctx context.Context, userID string) ([]Project, error) {
	return service.store.List(ctx, userID)
}

func (service *Service) ByID(ctx context.Context, userID, projectID string) (Project, error) {
	if !identity.ValidUUID(projectID) {
		return Project{}, ErrNotFound
	}
	return service.store.ByID(ctx, userID, projectID)
}

func (service *Service) Create(ctx context.Context, userID string, input CreateProjectInput) (Project, error) {
	name, err := normalizeName(input.Name)
	if err != nil {
		return Project{}, err
	}
	description, err := normalizeDescription(input.Description)
	if err != nil {
		return Project{}, err
	}
	if strings.TrimSpace(input.Monitor.Name) == "" {
		input.Monitor.Name = name
	}

	projectID, err := identity.NewUUID()
	if err != nil {
		return Project{}, fmt.Errorf("generate project ID: %w", err)
	}
	monitor, err := service.newMonitor(projectID, input.Monitor)
	if err != nil {
		return Project{}, err
	}

	now := service.now().UTC()
	project := Project{
		ID:          projectID,
		Name:        name,
		Description: description,
		Monitors:    []Monitor{monitor},
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	monitor.CreatedAt = now
	monitor.UpdatedAt = now
	project.Monitors[0] = monitor

	if err := service.store.Create(ctx, userID, project, monitor); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (service *Service) Update(
	ctx context.Context,
	userID,
	projectID string,
	input UpdateProjectInput,
) (Project, error) {
	if !identity.ValidUUID(projectID) {
		return Project{}, ErrNotFound
	}
	if input.Name == nil && input.Description == nil {
		return Project{}, ErrEmptyUpdate
	}
	if input.Name != nil {
		name, err := normalizeName(*input.Name)
		if err != nil {
			return Project{}, err
		}
		input.Name = &name
	}
	if input.Description != nil {
		description, err := normalizeDescription(*input.Description)
		if err != nil {
			return Project{}, err
		}
		input.Description = &description
	}
	return service.store.UpdateProject(ctx, userID, projectID, input)
}

func (service *Service) Delete(ctx context.Context, userID, projectID string) error {
	if !identity.ValidUUID(projectID) {
		return ErrNotFound
	}
	return service.store.SoftDeleteProject(ctx, userID, projectID)
}

func (service *Service) AddMonitor(
	ctx context.Context,
	userID,
	projectID string,
	input CreateMonitorInput,
) (Monitor, error) {
	if !identity.ValidUUID(projectID) {
		return Monitor{}, ErrNotFound
	}
	monitor, err := service.newMonitor(projectID, input)
	if err != nil {
		return Monitor{}, err
	}
	now := service.now().UTC()
	monitor.CreatedAt = now
	monitor.UpdatedAt = now
	return service.store.AddMonitor(ctx, userID, projectID, monitor)
}

func (service *Service) UpdateMonitor(
	ctx context.Context,
	userID,
	projectID,
	monitorID string,
	input UpdateMonitorInput,
) (Monitor, error) {
	if !identity.ValidUUID(projectID) || !identity.ValidUUID(monitorID) {
		return Monitor{}, ErrNotFound
	}
	if input.Name == nil && input.URL == nil && input.Target == nil && input.CheckIntervalSeconds == nil && input.TimeoutSeconds == nil && input.Enabled == nil {
		return Monitor{}, ErrEmptyUpdate
	}
	project, err := service.store.ByID(ctx, userID, projectID)
	if err != nil {
		return Monitor{}, err
	}
	var current *Monitor
	for index := range project.Monitors {
		if project.Monitors[index].ID == monitorID {
			current = &project.Monitors[index]
			break
		}
	}
	if current == nil {
		return Monitor{}, ErrNotFound
	}
	if input.Name != nil {
		name, err := normalizeName(*input.Name)
		if err != nil {
			return Monitor{}, err
		}
		input.Name = &name
	}
	if input.URL != nil {
		if current.Type != "http" {
			return Monitor{}, ErrInvalidURL
		}
		normalizedURL, err := netpolicy.NormalizeHTTPURL(*input.URL)
		if err != nil {
			return Monitor{}, ErrInvalidURL
		}
		input.URL = &normalizedURL
	}
	if input.Target != nil {
		if current.Type != "tcp" {
			return Monitor{}, ErrInvalidTarget
		}
		normalizedTarget, err := netpolicy.NormalizeTCPAddress(*input.Target)
		if err != nil {
			return Monitor{}, ErrInvalidTarget
		}
		input.Target = &normalizedTarget
	}
	if input.CheckIntervalSeconds != nil && !validInterval(*input.CheckIntervalSeconds) {
		return Monitor{}, ErrInvalidInterval
	}
	if input.TimeoutSeconds != nil && !validTimeout(*input.TimeoutSeconds) {
		return Monitor{}, ErrInvalidTimeout
	}
	return service.store.UpdateMonitor(ctx, userID, projectID, monitorID, input)
}

func (service *Service) DeleteMonitor(ctx context.Context, userID, projectID, monitorID string) error {
	if !identity.ValidUUID(projectID) || !identity.ValidUUID(monitorID) {
		return ErrNotFound
	}
	return service.store.SoftDeleteMonitor(ctx, userID, projectID, monitorID)
}

func (service *Service) newMonitor(projectID string, input CreateMonitorInput) (Monitor, error) {
	name, err := normalizeName(input.Name)
	if err != nil {
		return Monitor{}, err
	}
	monitorType := strings.ToLower(strings.TrimSpace(input.Type))
	if monitorType == "" {
		monitorType = "http"
	}
	var normalizedURL string
	var normalizedTarget string
	var heartbeatToken string
	var heartbeatTokenHash []byte
	switch monitorType {
	case "http":
		if strings.TrimSpace(input.Target) != "" {
			return Monitor{}, ErrInvalidTarget
		}
		normalizedURL, err = netpolicy.NormalizeHTTPURL(input.URL)
		if err != nil {
			return Monitor{}, ErrInvalidURL
		}
	case "heartbeat":
		if strings.TrimSpace(input.URL) != "" {
			return Monitor{}, ErrInvalidURL
		}
		if strings.TrimSpace(input.Target) != "" {
			return Monitor{}, ErrInvalidTarget
		}
		heartbeatToken, heartbeatTokenHash, err = newHeartbeatToken()
		if err != nil {
			return Monitor{}, fmt.Errorf("generate heartbeat token: %w", err)
		}
	case "tcp":
		if strings.TrimSpace(input.URL) != "" {
			return Monitor{}, ErrInvalidURL
		}
		normalizedTarget, err = netpolicy.NormalizeTCPAddress(input.Target)
		if err != nil {
			return Monitor{}, ErrInvalidTarget
		}
	default:
		return Monitor{}, ErrInvalidType
	}
	interval := defaultCheckInterval
	if input.CheckIntervalSeconds != nil {
		interval = *input.CheckIntervalSeconds
	}
	if !validInterval(interval) {
		return Monitor{}, ErrInvalidInterval
	}
	timeout := defaultTimeout
	if input.TimeoutSeconds != nil {
		timeout = *input.TimeoutSeconds
	}
	if !validTimeout(timeout) {
		return Monitor{}, ErrInvalidTimeout
	}
	monitorID, err := identity.NewUUID()
	if err != nil {
		return Monitor{}, fmt.Errorf("generate monitor ID: %w", err)
	}
	return Monitor{
		ID:                   monitorID,
		ProjectID:            projectID,
		Type:                 monitorType,
		Name:                 name,
		URL:                  normalizedURL,
		Target:               normalizedTarget,
		CheckIntervalSeconds: interval,
		TimeoutSeconds:       timeout,
		Enabled:              true,
		HeartbeatToken:       heartbeatToken,
		HeartbeatTokenHash:   heartbeatTokenHash,
	}, nil
}

func newHeartbeatToken() (string, []byte, error) {
	rawToken := make([]byte, 32)
	if _, err := rand.Read(rawToken); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(rawToken)
	hash := sha256.Sum256([]byte(token))
	return token, hash[:], nil
}

func normalizeName(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" || !utf8.ValidString(normalized) || utf8.RuneCountInString(normalized) > maximumNameCharacters {
		return "", ErrInvalidName
	}
	return normalized, nil
}

func normalizeDescription(value string) (string, error) {
	normalized := strings.TrimSpace(value)
	if !utf8.ValidString(normalized) || utf8.RuneCountInString(normalized) > maximumDescriptionCharacters {
		return "", ErrInvalidDescription
	}
	return normalized, nil
}

func validInterval(value int) bool {
	return value >= minimumCheckInterval && value <= maximumCheckInterval
}

func validTimeout(value int) bool {
	return value >= minimumTimeout && value <= maximumTimeout
}
