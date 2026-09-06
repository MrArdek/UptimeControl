package monitoring

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/MrArdek/UptimeControl/internal/netpolicy"
)

const (
	webhookRequestTimeout = 10 * time.Second
	webhookMaxRedirects   = 3
	webhookMaxAttempts    = 3
	webhookMaxBodyBytes   = 1 << 20
)

// WebhookEndpoint is a single outgoing notification target loaded from storage.
// The secret signs every payload and never leaves the backend process.
type WebhookEndpoint struct {
	ID        string
	ProjectID string
	URL       string
	Secret    []byte
	Events    string
}

// WebhookLoader returns the enabled endpoints of one project.
type WebhookLoader interface {
	ActiveWebhooks(context.Context, string) ([]WebhookEndpoint, error)
}

// DeliveryRecorder persists the final outcome of one webhook delivery.
// Recording is best-effort: a recorder failure is logged, never fatal.
type DeliveryRecorder interface {
	RecordDelivery(ctx context.Context, webhookID, event string, statusCode *int, attempts int, lastError *string) error
}

type webhookPayload struct {
	Event       string `json:"event"`
	ProjectID   string `json:"project_id"`
	ProjectName string `json:"project_name"`
	MonitorID   string `json:"monitor_id"`
	MonitorName string `json:"monitor_name"`
	URL         string `json:"url"`
	Cause       string `json:"cause"`
	OccurredAt  string `json:"occurred_at"`
}

// WebhookDispatcher delivers transitions to per-project webhook endpoints.
// It implements Notifier, so the scheduler keeps calling Telegram exactly as
// before while webhooks fan out alongside it.
type WebhookDispatcher struct {
	loader   WebhookLoader
	recorder DeliveryRecorder
	client   *http.Client
	send     func(ctx context.Context, endpoint WebhookEndpoint, event string, body []byte) (int, error)
	logger   *slog.Logger
	now      func() time.Time
}

func NewWebhookDispatcher(loader WebhookLoader, recorder DeliveryRecorder, logger *slog.Logger) *WebhookDispatcher {
	transport := netpolicy.NewSafeTransport(webhookRequestTimeout)
	if logger == nil {
		logger = slog.Default()
	}

	dispatcher := &WebhookDispatcher{
		loader:   loader,
		recorder: recorder,
		client: &http.Client{
			Transport:     transport,
			Timeout:       webhookRequestTimeout,
			CheckRedirect: netpolicy.PublicRedirectValidator(webhookMaxRedirects),
		},
		logger: logger,
		now:    time.Now,
	}
	dispatcher.send = dispatcher.sendRequest

	return dispatcher
}

func (dispatcher *WebhookDispatcher) Notify(ctx context.Context, transition Transition) error {
	if transition.Empty() || transition.Kind == "" {
		return nil
	}
	if dispatcher.loader == nil || transition.ProjectID == "" {
		return nil
	}

	endpoints, err := dispatcher.loader.ActiveWebhooks(ctx, transition.ProjectID)
	if err != nil {
		if ctx.Err() == nil {
			dispatcher.logger.Error("load webhooks", "project_id", transition.ProjectID, "error", err)
		}
		return err
	}

	var lastError error
	for _, endpoint := range endpoints {
		if !webhookWantsEvent(endpoint.Events, transition.Kind) {
			continue
		}
		if err := dispatcher.deliver(ctx, endpoint, transition); err != nil {
			lastError = err
		}
	}

	return lastError
}

func webhookWantsEvent(events, kind string) bool {
	for _, part := range strings.Split(events, ",") {
		if strings.ToLower(strings.TrimSpace(part)) == kind && kind != "" {
			return true
		}
	}

	return false
}

func (dispatcher *WebhookDispatcher) deliver(ctx context.Context, endpoint WebhookEndpoint, transition Transition) error {
	body, err := json.Marshal(webhookPayload{
		Event:       transition.Kind,
		ProjectID:   transition.ProjectID,
		ProjectName: transition.ProjectName,
		MonitorID:   transition.MonitorID,
		MonitorName: transition.MonitorName,
		URL:         transition.URL,
		Cause:       transition.Cause,
		OccurredAt:  transition.OccurredAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("encode webhook payload")
	}

	var statusCode *int
	var lastError *string
	attempts := 0
	backoffs := []time.Duration{0, time.Second, 4 * time.Second}
	for attempt := 0; attempt < webhookMaxAttempts; attempt++ {
		if backoffs[attempt] > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoffs[attempt]):
			}
		}
		attempts = attempt + 1
		status, sendErr := dispatcher.send(ctx, endpoint, transition.Kind, body)
		if sendErr == nil {
			statusCode = &status
			lastError = nil
			break
		}
		message := sendErr.Error()
		lastError = &message
	}

	if dispatcher.recorder != nil {
		if err := dispatcher.recorder.RecordDelivery(ctx, endpoint.ID, transition.Kind, statusCode, attempts, lastError); err != nil && ctx.Err() == nil {
			dispatcher.logger.Error("record webhook delivery", "webhook_id", endpoint.ID, "error", err)
		}
	}
	if lastError != nil {
		return fmt.Errorf("webhook delivery failed")
	}

	return nil
}

func (dispatcher *WebhookDispatcher) sendRequest(
	ctx context.Context,
	endpoint WebhookEndpoint,
	event string,
	body []byte,
) (int, error) {
	request, err := newWebhookRequest(ctx, endpoint, event, body)
	if err != nil {
		return 0, err
	}

	response, err := dispatcher.client.Do(request)
	if err != nil {
		// Never include the error text: net/http errors may echo the request
		// URL, and delivery errors are stored next to the endpoint.
		return 0, fmt.Errorf("webhook request failed")
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, webhookMaxBodyBytes))

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return 0, fmt.Errorf("webhook returned status %d", response.StatusCode)
	}

	return response.StatusCode, nil
}

func newWebhookRequest(
	ctx context.Context,
	endpoint WebhookEndpoint,
	event string,
	body []byte,
) (*http.Request, error) {
	if _, err := netpolicy.NormalizeHTTPURL(endpoint.URL); err != nil {
		return nil, fmt.Errorf("unsafe webhook URL")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create webhook request")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "UptimeControl/1.0")
	request.Header.Set("X-UptimeControl-Event", event)
	request.Header.Set("X-UptimeControl-Signature", "sha256="+webhookSignature(endpoint.Secret, body))

	return request, nil
}

func webhookSignature(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)

	return hex.EncodeToString(mac.Sum(nil))
}
