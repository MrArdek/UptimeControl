package projects

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MrArdek/UptimeControl/internal/identity"
	"github.com/MrArdek/UptimeControl/internal/netpolicy"
)

const maxWebhooksPerProject = 5

var (
	ErrInvalidEvents   = errors.New("invalid webhook events")
	ErrTooManyWebhooks = errors.New("too many webhooks for this project")
)

// Webhook is an outgoing notification endpoint owned through its project.
// Secret carries the raw signing token only in the creation response: it is
// shown once and never returned by list operations.
type Webhook struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	URL       string    `json:"url"`
	Events    string    `json:"events"`
	Enabled   bool      `json:"enabled"`
	Secret    string    `json:"secret,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateWebhookInput struct {
	URL    string
	Events string
}

type UpdateWebhookInput struct {
	Events  *string
	Enabled *bool
}

func (service *Service) CreateWebhook(
	ctx context.Context,
	userID,
	projectID string,
	input CreateWebhookInput,
) (Webhook, error) {
	if !identity.ValidUUID(projectID) {
		return Webhook{}, ErrNotFound
	}
	normalizedURL, err := netpolicy.NormalizeHTTPURL(input.URL)
	if err != nil {
		return Webhook{}, ErrInvalidURL
	}
	events, err := normalizeWebhookEvents(input.Events)
	if err != nil {
		return Webhook{}, err
	}

	existing, err := service.store.ListWebhooks(ctx, userID, projectID)
	if err != nil {
		return Webhook{}, err
	}
	if len(existing) >= maxWebhooksPerProject {
		return Webhook{}, ErrTooManyWebhooks
	}

	webhookID, err := identity.NewUUID()
	if err != nil {
		return Webhook{}, fmt.Errorf("generate webhook ID: %w", err)
	}
	secret, secretHash, err := newWebhookSecret()
	if err != nil {
		return Webhook{}, fmt.Errorf("generate webhook secret: %w", err)
	}

	now := service.now().UTC()
	hook := Webhook{
		ID:        webhookID,
		ProjectID: projectID,
		URL:       normalizedURL,
		Events:    events,
		Enabled:   true,
		CreatedAt: now,
		UpdatedAt: now,
	}
	stored, err := service.store.CreateWebhook(ctx, userID, projectID, hook, secretHash)
	if err != nil {
		return Webhook{}, err
	}
	stored.Secret = secret

	return stored, nil
}

func (service *Service) ListWebhooks(ctx context.Context, userID, projectID string) ([]Webhook, error) {
	if !identity.ValidUUID(projectID) {
		return nil, ErrNotFound
	}

	return service.store.ListWebhooks(ctx, userID, projectID)
}

func (service *Service) DeleteWebhook(ctx context.Context, userID, projectID, webhookID string) error {
	if !identity.ValidUUID(projectID) || !identity.ValidUUID(webhookID) {
		return ErrNotFound
	}

	return service.store.SoftDeleteWebhook(ctx, userID, projectID, webhookID)
}

func (service *Service) UpdateWebhook(
	ctx context.Context,
	userID,
	projectID,
	webhookID string,
	input UpdateWebhookInput,
) (Webhook, error) {
	if !identity.ValidUUID(projectID) || !identity.ValidUUID(webhookID) {
		return Webhook{}, ErrNotFound
	}
	if input.Events == nil && input.Enabled == nil {
		return Webhook{}, ErrEmptyUpdate
	}
	if input.Events != nil {
		events, err := normalizeWebhookEvents(*input.Events)
		if err != nil {
			return Webhook{}, err
		}
		input.Events = &events
	}
	return service.store.UpdateWebhook(ctx, userID, projectID, webhookID, input)
}

func newWebhookSecret() (string, []byte, error) {
	rawSecret := make([]byte, 32)
	if _, err := rand.Read(rawSecret); err != nil {
		return "", nil, err
	}

	return base64.RawURLEncoding.EncodeToString(rawSecret), rawSecret, nil
}

// normalizeWebhookEvents accepts a comma-separated subset of down,recovered
// and returns it in canonical order.
func normalizeWebhookEvents(raw string) (string, error) {
	seen := map[string]bool{}
	ordered := []string{}
	for _, part := range strings.Split(raw, ",") {
		event := strings.ToLower(strings.TrimSpace(part))
		if event == "" {
			continue
		}
		if event != "down" && event != "recovered" {
			return "", ErrInvalidEvents
		}
		if !seen[event] {
			seen[event] = true
			ordered = append(ordered, event)
		}
	}
	if len(ordered) == 0 {
		return "", ErrInvalidEvents
	}
	// Canonical order keeps stored values and signatures stable.
	canonical := []string{}
	for _, event := range []string{"down", "recovered"} {
		if seen[event] {
			canonical = append(canonical, event)
		}
	}

	return strings.Join(canonical, ","), nil
}

// WebhookWantsEvent reports whether a stored events list includes the given
// transition kind.
func WebhookWantsEvent(events, kind string) bool {
	for _, event := range strings.Split(events, ",") {
		if strings.TrimSpace(event) == kind {
			return true
		}
	}

	return false
}
