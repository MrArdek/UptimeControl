package sites

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/MrArdek/UptimeControl/internal/identity"
	"github.com/MrArdek/UptimeControl/internal/netpolicy"
)

const (
	maximumNameCharacters = 100
	minimumCheckInterval  = 30
	maximumCheckInterval  = 86400
	defaultCheckInterval  = 60
)

var (
	ErrInvalidName     = errors.New("invalid site name")
	ErrInvalidURL      = errors.New("invalid site URL")
	ErrInvalidInterval = errors.New("invalid check interval")
	ErrEmptyUpdate     = errors.New("update has no fields")
	ErrNotFound        = errors.New("site not found")
	ErrAlreadyExists   = errors.New("site already exists")
)

// Site is a monitored website owned by one user.
type Site struct {
	ID                   string    `json:"id"`
	Name                 string    `json:"name"`
	URL                  string    `json:"url"`
	CheckIntervalSeconds int       `json:"check_interval_seconds"`
	Enabled              bool      `json:"enabled"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type CreateInput struct {
	Name                 string
	URL                  string
	CheckIntervalSeconds *int
}

type UpdateInput struct {
	Name                 *string
	URL                  *string
	CheckIntervalSeconds *int
	Enabled              *bool
}

type Store interface {
	List(context.Context, string) ([]Site, error)
	Create(context.Context, string, Site) error
	ByID(context.Context, string, string) (Site, error)
	Update(context.Context, string, string, UpdateInput) (Site, error)
	SoftDelete(context.Context, string, string) error
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (service *Service) List(ctx context.Context, userID string) ([]Site, error) {
	return service.store.List(ctx, userID)
}

func (service *Service) Create(ctx context.Context, userID string, input CreateInput) (Site, error) {
	name, err := normalizeName(input.Name)
	if err != nil {
		return Site{}, err
	}

	normalizedURL, err := normalizeURL(input.URL)
	if err != nil {
		return Site{}, err
	}

	interval := defaultCheckInterval
	if input.CheckIntervalSeconds != nil {
		interval = *input.CheckIntervalSeconds
	}
	if err := validateInterval(interval); err != nil {
		return Site{}, err
	}

	identifier, err := identity.NewUUID()
	if err != nil {
		return Site{}, fmt.Errorf("generate site ID: %w", err)
	}

	now := service.now().UTC()
	site := Site{
		ID:                   identifier,
		Name:                 name,
		URL:                  normalizedURL,
		CheckIntervalSeconds: interval,
		Enabled:              true,
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if err := service.store.Create(ctx, userID, site); err != nil {
		return Site{}, err
	}

	return site, nil
}

func (service *Service) ByID(ctx context.Context, userID, siteID string) (Site, error) {
	if !identity.ValidUUID(siteID) {
		return Site{}, ErrNotFound
	}

	return service.store.ByID(ctx, userID, siteID)
}

func (service *Service) Update(
	ctx context.Context,
	userID string,
	siteID string,
	input UpdateInput,
) (Site, error) {
	if !identity.ValidUUID(siteID) {
		return Site{}, ErrNotFound
	}
	if input.Name == nil && input.URL == nil && input.CheckIntervalSeconds == nil && input.Enabled == nil {
		return Site{}, ErrEmptyUpdate
	}

	if input.Name != nil {
		name, err := normalizeName(*input.Name)
		if err != nil {
			return Site{}, err
		}
		input.Name = &name
	}

	if input.URL != nil {
		normalizedURL, err := normalizeURL(*input.URL)
		if err != nil {
			return Site{}, err
		}
		input.URL = &normalizedURL
	}

	if input.CheckIntervalSeconds != nil {
		if err := validateInterval(*input.CheckIntervalSeconds); err != nil {
			return Site{}, err
		}
	}

	return service.store.Update(ctx, userID, siteID, input)
}

func (service *Service) Delete(ctx context.Context, userID, siteID string) error {
	if !identity.ValidUUID(siteID) {
		return ErrNotFound
	}

	return service.store.SoftDelete(ctx, userID, siteID)
}

func normalizeName(name string) (string, error) {
	normalized := strings.TrimSpace(name)
	if normalized == "" || !utf8.ValidString(normalized) || utf8.RuneCountInString(normalized) > maximumNameCharacters {
		return "", ErrInvalidName
	}

	return normalized, nil
}

func validateInterval(interval int) error {
	if interval < minimumCheckInterval || interval > maximumCheckInterval {
		return ErrInvalidInterval
	}

	return nil
}

func normalizeURL(rawURL string) (string, error) {
	normalized, err := netpolicy.NormalizeHTTPURL(rawURL)
	if err != nil {
		return "", ErrInvalidURL
	}

	return normalized, nil
}
