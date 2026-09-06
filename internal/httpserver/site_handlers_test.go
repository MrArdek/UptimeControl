package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/sites"
)

type configurableSiteService struct {
	list          []sites.Site
	listError     error
	created       sites.Site
	createError   error
	createUserID  string
	createInput   sites.CreateInput
	selected      sites.Site
	selectedError error
	selectedUser  string
	selectedID    string
	updated       sites.Site
	updateError   error
	deletedUser   string
	deletedID     string
	deleteError   error
}

func (service *configurableSiteService) List(context.Context, string) ([]sites.Site, error) {
	return service.list, service.listError
}

func (service *configurableSiteService) Create(_ context.Context, userID string, input sites.CreateInput) (sites.Site, error) {
	service.createUserID = userID
	service.createInput = input
	return service.created, service.createError
}

func (service *configurableSiteService) ByID(_ context.Context, userID, siteID string) (sites.Site, error) {
	service.selectedUser = userID
	service.selectedID = siteID
	return service.selected, service.selectedError
}

func (service *configurableSiteService) Update(context.Context, string, string, sites.UpdateInput) (sites.Site, error) {
	return service.updated, service.updateError
}

func (service *configurableSiteService) Delete(_ context.Context, userID, siteID string) error {
	service.deletedUser = userID
	service.deletedID = siteID
	return service.deleteError
}

func authenticatedSiteHandlers(siteManagement siteService) *siteHandlers {
	authentication := &configurableAuthService{currentUser: auth.User{ID: "owner-id", Email: "owner@example.com"}}
	return newSiteHandlers(authentication, siteManagement, Options{AllowedOrigin: "https://app.example.com"})
}

func addSessionCookie(request *http.Request) {
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
}

func TestCreateSiteHandler(t *testing.T) {
	createdSite := sites.Site{
		ID:                   "00000000-0000-4000-8000-000000000001",
		Name:                 "Example",
		URL:                  "https://example.com",
		CheckIntervalSeconds: 60,
		Enabled:              true,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}
	service := &configurableSiteService{created: createdSite}
	handlers := authenticatedSiteHandlers(service)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sites",
		strings.NewReader(`{"name":"Example","url":"https://example.com"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://app.example.com")
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.collection(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if service.createUserID != "owner-id" {
		t.Fatalf("create owner = %q, want authenticated user", service.createUserID)
	}
	if location := response.Header().Get("Location"); location != "/api/v1/sites/"+createdSite.ID {
		t.Fatalf("Location = %q, want created site URL", location)
	}
}

func TestCreateSiteLocationIncludesBasePath(t *testing.T) {
	createdSite := sites.Site{
		ID:                   "00000000-0000-4000-8000-000000000001",
		Name:                 "API",
		URL:                  "https://example.com/health",
		CheckIntervalSeconds: 60,
		Enabled:              true,
		CreatedAt:            time.Now(),
		UpdatedAt:            time.Now(),
	}
	service := &configurableSiteService{created: createdSite}
	authentication := &configurableAuthService{currentUser: auth.User{ID: "owner-id"}}
	handlers := newSiteHandlers(authentication, service, Options{
		AllowedOrigin: "https://example.com",
		BasePath:      "/uptimec",
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/uptimec/api/v1/sites",
		strings.NewReader(`{"name":"API","url":"https://example.com/health"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://example.com")
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.collection(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	wantLocation := "/uptimec/api/v1/sites/" + createdSite.ID
	if location := response.Header().Get("Location"); location != wantLocation {
		t.Fatalf("Location = %q, want %q", location, wantLocation)
	}
}

func TestSiteHandlersRequireAuthentication(t *testing.T) {
	authentication := &configurableAuthService{currentError: auth.ErrUnauthorized}
	handlers := newSiteHandlers(authentication, &configurableSiteService{}, Options{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sites", nil)
	response := httptest.NewRecorder()

	handlers.collection(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestGetSiteUsesAuthenticatedOwner(t *testing.T) {
	service := &configurableSiteService{selected: sites.Site{ID: "site-id"}}
	handlers := authenticatedSiteHandlers(service)
	siteID := "00000000-0000-4000-8000-000000000001"
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sites/"+siteID, nil)
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.item(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if service.selectedUser != "owner-id" || service.selectedID != siteID {
		t.Fatalf("site lookup did not include owner and ID: %q %q", service.selectedUser, service.selectedID)
	}
}

func TestGetSiteHidesMissingOrForeignSite(t *testing.T) {
	service := &configurableSiteService{selectedError: sites.ErrNotFound}
	handlers := authenticatedSiteHandlers(service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sites/00000000-0000-4000-8000-000000000001", nil)
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.item(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestCreateSiteRejectsForeignOrigin(t *testing.T) {
	handlers := authenticatedSiteHandlers(&configurableSiteService{})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/sites",
		strings.NewReader(`{"name":"Example","url":"https://example.com"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.collection(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestDeleteSite(t *testing.T) {
	service := &configurableSiteService{}
	handlers := authenticatedSiteHandlers(service)
	siteID := "00000000-0000-4000-8000-000000000001"
	request := httptest.NewRequest(http.MethodDelete, "/api/v1/sites/"+siteID, nil)
	request.Header.Set("Origin", "https://app.example.com")
	request.Header.Set("X-Confirm-Delete", "true")
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.item(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if service.deletedUser != "owner-id" || service.deletedID != siteID {
		t.Fatal("delete did not include the authenticated owner and site ID")
	}
}

func TestDeleteSiteRequiresExplicitConfirmation(t *testing.T) {
	handlers := authenticatedSiteHandlers(&configurableSiteService{})
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/sites/00000000-0000-4000-8000-000000000001",
		nil,
	)
	request.Header.Set("Origin", "https://app.example.com")
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.item(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if !strings.Contains(response.Body.String(), "confirmation_required") {
		t.Fatalf("response does not explain required confirmation: %s", response.Body.String())
	}
}

func TestSiteErrorDoesNotLeakInternalDetails(t *testing.T) {
	service := &configurableSiteService{listError: errors.New("database detail")}
	handlers := authenticatedSiteHandlers(service)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/sites", nil)
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.collection(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "database detail") {
		t.Fatalf("internal error leaked to client: %s", response.Body.String())
	}
}
