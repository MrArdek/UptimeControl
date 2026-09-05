package sites

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryStore struct {
	createdUserID string
	createdSite   Site
	updatedInput  UpdateInput
	deletedUserID string
	deletedSiteID string
	byIDCalls     int
}

func (store *memoryStore) List(context.Context, string) ([]Site, error) {
	return []Site{}, nil
}

func (store *memoryStore) Create(_ context.Context, userID string, site Site) error {
	store.createdUserID = userID
	store.createdSite = site
	return nil
}

func (store *memoryStore) ByID(context.Context, string, string) (Site, error) {
	store.byIDCalls++
	return Site{}, nil
}

func (store *memoryStore) Update(_ context.Context, _ string, _ string, input UpdateInput) (Site, error) {
	store.updatedInput = input
	return Site{}, nil
}

func (store *memoryStore) SoftDelete(_ context.Context, userID, siteID string) error {
	store.deletedUserID = userID
	store.deletedSiteID = siteID
	return nil
}

func TestCreateNormalizesSiteAndUsesDefaults(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store)
	service.now = func() time.Time {
		return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	}

	site, err := service.Create(context.Background(), "user-id", CreateInput{
		Name: "  Example site  ",
		URL:  "HTTPS://Example.COM./status?full=true",
	})
	if err != nil {
		t.Fatalf("Create() returned an error: %v", err)
	}

	if site.Name != "Example site" {
		t.Fatalf("name = %q, want normalized name", site.Name)
	}
	if site.URL != "https://example.com/status?full=true" {
		t.Fatalf("URL = %q, want normalized URL", site.URL)
	}
	if site.CheckIntervalSeconds != defaultCheckInterval {
		t.Fatalf("interval = %d, want %d", site.CheckIntervalSeconds, defaultCheckInterval)
	}
	if !site.Enabled {
		t.Fatal("new site is disabled")
	}
	if store.createdUserID != "user-id" || store.createdSite.ID != site.ID {
		t.Fatal("site was not stored for the expected owner")
	}
}

func TestCreateRejectsUnsafeURLs(t *testing.T) {
	unsafeURLs := []string{
		"ftp://example.com",
		"http://localhost:8080",
		"http://service.internal",
		"http://intranet",
		"http://127.0.0.1",
		"http://10.0.0.1",
		"http://169.254.169.254/latest/meta-data",
		"http://100.64.0.1",
		"http://[::1]",
		"https://user:password@example.com",
	}

	service := NewService(&memoryStore{})
	for _, unsafeURL := range unsafeURLs {
		t.Run(unsafeURL, func(t *testing.T) {
			_, err := service.Create(context.Background(), "user-id", CreateInput{
				Name: "Example",
				URL:  unsafeURL,
			})
			if !errors.Is(err, ErrInvalidURL) {
				t.Fatalf("Create() error = %v, want invalid URL", err)
			}
		})
	}
}

func TestCreateValidatesInterval(t *testing.T) {
	invalidInterval := 10
	service := NewService(&memoryStore{})

	_, err := service.Create(context.Background(), "user-id", CreateInput{
		Name:                 "Example",
		URL:                  "https://example.com",
		CheckIntervalSeconds: &invalidInterval,
	})
	if !errors.Is(err, ErrInvalidInterval) {
		t.Fatalf("Create() error = %v, want invalid interval", err)
	}
}

func TestUpdateRejectsEmptyInputAndNormalizesFields(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store)
	siteID := "00000000-0000-4000-8000-000000000000"

	if _, err := service.Update(context.Background(), "user-id", siteID, UpdateInput{}); !errors.Is(err, ErrEmptyUpdate) {
		t.Fatalf("Update() error = %v, want empty update", err)
	}

	name := "  New name "
	rawURL := "https://EXAMPLE.com/new"
	interval := 300
	if _, err := service.Update(context.Background(), "user-id", siteID, UpdateInput{
		Name:                 &name,
		URL:                  &rawURL,
		CheckIntervalSeconds: &interval,
	}); err != nil {
		t.Fatalf("Update() returned an error: %v", err)
	}

	if store.updatedInput.Name == nil || *store.updatedInput.Name != "New name" {
		t.Fatalf("updated name was not normalized: %#v", store.updatedInput.Name)
	}
	if store.updatedInput.URL == nil || *store.updatedInput.URL != "https://example.com/new" {
		t.Fatalf("updated URL was not normalized: %#v", store.updatedInput.URL)
	}
}

func TestInvalidIDIsHiddenAsNotFound(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store)

	if _, err := service.ByID(context.Background(), "user-id", "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ByID() error = %v, want not found", err)
	}
	if store.byIDCalls != 0 {
		t.Fatal("store was queried with an invalid UUID")
	}
}
