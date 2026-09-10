package monitoring

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"testing"
	"time"
)

type webhookTestLoader struct {
	endpoints []WebhookEndpoint
	err       error
}

func (loader webhookTestLoader) ActiveWebhooks(context.Context, string) ([]WebhookEndpoint, error) {
	return loader.endpoints, loader.err
}

type webhookTestRecorder struct {
	webhookID  string
	event      string
	statusCode *int
	attempts   int
	lastError  *string
}

func (recorder *webhookTestRecorder) RecordDelivery(
	_ context.Context,
	webhookID,
	event string,
	statusCode *int,
	attempts int,
	lastError *string,
) error {
	recorder.webhookID = webhookID
	recorder.event = event
	recorder.statusCode = statusCode
	recorder.attempts = attempts
	recorder.lastError = lastError

	return nil
}

func webhookTestTransition() Transition {
	return Transition{
		EventID:     "11111111-1111-4111-8111-111111111111",
		Kind:        "down",
		ProjectID:   "project-id",
		ProjectName: "Public API",
		MonitorID:   "monitor-id",
		MonitorName: "Health",
		URL:         "https://example.com/health",
		Cause:       "connection refused",
		OccurredAt:  time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC),
	}
}

func TestWebhookDispatcherDeliversSignedPayload(t *testing.T) {
	secret := []byte("test-signing-secret-0123456789abcdef")
	var receivedBody []byte
	var receivedEvent string
	var receivedStatus int
	recorder := &webhookTestRecorder{}
	dispatcher := NewWebhookDispatcher(
		webhookTestLoader{endpoints: []WebhookEndpoint{{
			ID:        "webhook-id",
			ProjectID: "project-id",
			URL:       "https://hooks.example.com/uptime",
			Secret:    secret,
			Events:    "down,recovered",
		}}},
		recorder,
		slog.Default(),
	)
	dispatcher.send = func(_ context.Context, endpoint WebhookEndpoint, event string, body []byte) (int, error) {
		receivedBody = append([]byte(nil), body...)
		receivedEvent = event
		receivedStatus = http.StatusOK
		request, err := newWebhookRequest(context.Background(), endpoint, event, body)
		if err != nil {
			return 0, err
		}
		if request.Header.Get("X-UptimeControl-Event") != "down" {
			return 0, errWebhookTest
		}
		if request.Header.Get("X-UptimeControl-Event-ID") != "11111111-1111-4111-8111-111111111111" {
			return 0, errWebhookTest
		}

		return http.StatusOK, nil
	}
	if err := dispatcher.Notify(context.Background(), webhookTestTransition()); err != nil {
		t.Fatalf("Notify() returned an error: %v", err)
	}

	var payload map[string]string
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload["event_id"] == "" || payload["event"] != "down" || payload["project_id"] != "project-id" || payload["monitor_id"] != "monitor-id" {
		t.Fatalf("unexpected payload: %s", receivedBody)
	}
	if receivedEvent != "down" || receivedStatus != http.StatusOK {
		t.Fatalf("delivery = %q/%d, want down/200", receivedEvent, receivedStatus)
	}
	if recorder.webhookID != "webhook-id" || recorder.attempts != 1 || recorder.lastError != nil {
		t.Fatalf("delivery was not recorded: %#v", recorder)
	}
	if recorder.statusCode == nil || *recorder.statusCode != http.StatusOK {
		t.Fatalf("delivery status was not recorded: %#v", recorder)
	}
}

func TestNewWebhookRequestSignsPayload(t *testing.T) {
	secret := []byte("test-signing-secret-0123456789abcdef")
	body := []byte(`{"event":"down"}`)
	request, err := newWebhookRequest(context.Background(), WebhookEndpoint{
		URL:    "https://hooks.example.com/uptime",
		Secret: secret,
	}, "down", body)
	if err != nil {
		t.Fatalf("newWebhookRequest() returned an error: %v", err)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	if got := request.Header.Get("X-UptimeControl-Signature"); got != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("invalid signature: %q", got)
	}
	if _, err := newWebhookRequest(context.Background(), WebhookEndpoint{URL: "http://127.0.0.1/hook"}, "down", body); err == nil {
		t.Fatal("private webhook URL was accepted")
	}
}

func TestWebhookDispatcherSkipsUnsubscribedEvents(t *testing.T) {
	calls := 0
	recorder := &webhookTestRecorder{}
	dispatcher := NewWebhookDispatcher(
		webhookTestLoader{endpoints: []WebhookEndpoint{{
			ID:        "webhook-id",
			ProjectID: "project-id",
			URL:       "https://hooks.example.com/uptime",
			Secret:    []byte("secret"),
			Events:    "recovered",
		}}},
		recorder,
		slog.Default(),
	)
	dispatcher.send = func(context.Context, WebhookEndpoint, string, []byte) (int, error) {
		calls++

		return http.StatusOK, nil
	}
	if err := dispatcher.Notify(context.Background(), webhookTestTransition()); err != nil {
		t.Fatalf("Notify() returned an error: %v", err)
	}
	if calls != 0 {
		t.Fatalf("dispatcher sent %d requests for an unsubscribed event", calls)
	}
}

func TestWebhookDispatcherRecordsQueueAttemptFailure(t *testing.T) {
	recorder := &webhookTestRecorder{}
	dispatcher := NewWebhookDispatcher(
		webhookTestLoader{endpoints: []WebhookEndpoint{{
			ID:        "webhook-id",
			ProjectID: "project-id",
			URL:       "https://hooks.example.com/uptime",
			Secret:    []byte("secret"),
			Events:    "down",
		}}},
		recorder,
		slog.Default(),
	)
	dispatcher.send = func(context.Context, WebhookEndpoint, string, []byte) (int, error) {
		return 0, errWebhookTest
	}
	if err := dispatcher.Notify(context.Background(), webhookTestTransition()); err == nil {
		t.Fatal("Notify() succeeded against a failing endpoint")
	}
	if recorder.attempts != 1 || recorder.lastError == nil {
		t.Fatalf("failure was not recorded: %#v", recorder)
	}
}

func TestWebhookWantsEvent(t *testing.T) {
	if !webhookWantsEvent("down,recovered", "down") || !webhookWantsEvent("recovered", "recovered") {
		t.Fatal("subscribed event was rejected")
	}
	if webhookWantsEvent("recovered", "down") || webhookWantsEvent("", "down") {
		t.Fatal("unsubscribed event was accepted")
	}
}

func TestWebhookSignatureIsStable(t *testing.T) {
	first := webhookSignature([]byte("secret"), []byte(`{"event":"down"}`))
	second := webhookSignature([]byte("secret"), []byte(`{"event":"down"}`))
	if first == "" || first != second {
		t.Fatal("webhook signature is not deterministic")
	}
	if first == webhookSignature([]byte("other"), []byte(`{"event":"down"}`)) {
		t.Fatal("webhook signature ignores the secret")
	}
}

type stubNotifier struct {
	calls int
	err   error
}

func (notifier *stubNotifier) Notify(context.Context, Transition) error {
	notifier.calls++

	return notifier.err
}

var errWebhookTest = errors.New("webhook test failure")

func TestMultiNotifierDeliversToAll(t *testing.T) {
	first := &stubNotifier{}
	second := &stubNotifier{}
	failing := &stubNotifier{err: errWebhookTest}

	notifier := NewMultiNotifier(first, failing, second, nil)
	if err := notifier.Notify(context.Background(), Transition{Kind: "down"}); err == nil {
		t.Fatal("MultiNotifier swallowed a nested failure")
	}
	for name, got := range map[string]int{"first": first.calls, "failing": failing.calls, "second": second.calls} {
		if got != 1 {
			t.Fatalf("notifier %q called %d times, want 1", name, got)
		}
	}
	if err := NewMultiNotifier().Notify(context.Background(), Transition{Kind: "down"}); err != nil {
		t.Fatalf("empty MultiNotifier returned an error: %v", err)
	}
}
