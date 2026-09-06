package projects

import (
	"context"
	"testing"
)

func TestNormalizeWebhookEvents(t *testing.T) {
	events, err := normalizeWebhookEvents("recovered, down,down")
	if err != nil {
		t.Fatalf("normalizeWebhookEvents() returned an error: %v", err)
	}
	if events != "down,recovered" {
		t.Fatalf("events = %q, want canonical order", events)
	}
	for _, raw := range []string{"", "sms", "down,email"} {
		if _, err := normalizeWebhookEvents(raw); err != ErrInvalidEvents {
			t.Fatalf("normalizeWebhookEvents(%q) error = %v, want ErrInvalidEvents", raw, err)
		}
	}
}

func TestCreateWebhookReturnsSecretOnce(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store)
	hook, err := service.CreateWebhook(context.Background(), "owner-id", "11111111-1111-4111-8111-111111111111", CreateWebhookInput{
		URL:    "https://hooks.example.com/uptime",
		Events: "down",
	})
	if err != nil {
		t.Fatalf("CreateWebhook() returned an error: %v", err)
	}
	if len(hook.Secret) < 40 || hook.Events != "down" || hook.URL != "https://hooks.example.com/uptime" {
		t.Fatalf("webhook is invalid: %#v", hook)
	}
}

func TestCreateWebhookRejectsUnsafeURL(t *testing.T) {
	service := NewService(&memoryStore{})
	_, err := service.CreateWebhook(context.Background(), "owner-id", "11111111-1111-4111-8111-111111111111", CreateWebhookInput{
		URL:    "http://127.0.0.1/hook",
		Events: "down",
	})
	if err != ErrInvalidURL {
		t.Fatalf("CreateWebhook() error = %v, want ErrInvalidURL", err)
	}
}

func TestCreateWebhookEnforcesPerProjectLimit(t *testing.T) {
	store := &memoryStore{}
	for i := 0; i < maxWebhooksPerProject; i++ {
		store.webhooks = append(store.webhooks, Webhook{ID: "existing"})
	}
	service := NewService(store)
	_, err := service.CreateWebhook(context.Background(), "owner-id", "11111111-1111-4111-8111-111111111111", CreateWebhookInput{
		URL:    "https://hooks.example.com/extra",
		Events: "down,recovered",
	})
	if err != ErrTooManyWebhooks {
		t.Fatalf("CreateWebhook() error = %v, want ErrTooManyWebhooks", err)
	}
}

func TestWebhookWantsEvent(t *testing.T) {
	if !WebhookWantsEvent("down,recovered", "down") || WebhookWantsEvent("recovered", "down") {
		t.Fatal("WebhookWantsEvent() filters events incorrectly")
	}
}
