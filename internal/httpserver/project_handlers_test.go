package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/monitoring"
	"github.com/MrArdek/UptimeControl/internal/projects"
)

type configurableProjectService struct {
	project        projects.Project
	projectErr     error
	createdWebhook projects.Webhook
	createHookErr  error
	createHookUser string
	webhooks       []projects.Webhook
	listHooksErr   error
	deleteHookErr  error
	deletedHookID  string
}

func (service *configurableProjectService) ListPage(context.Context, string, int, string) (projects.ProjectPage, error) {
	return projects.ProjectPage{}, nil
}
func (service *configurableProjectService) ByID(context.Context, string, string) (projects.Project, error) {
	return service.project, service.projectErr
}

func TestHistoryRequiresMonitorInOwnedProject(t *testing.T) {
	service := &configurableProjectService{project: projects.Project{Monitors: []projects.Monitor{{
		ID: "22222222-2222-4222-8222-222222222222",
	}}}}
	handlers := authenticatedProjectHandlers(service)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/projects/11111111-1111-4111-8111-111111111111/monitors/33333333-3333-4333-8333-333333333333/checks", nil)
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.item(response, request)

	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), `"code":"monitor_not_found"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestHistoryRejectsInvalidPeriod(t *testing.T) {
	monitorID := "22222222-2222-4222-8222-222222222222"
	service := &configurableProjectService{project: projects.Project{Monitors: []projects.Monitor{{ID: monitorID}}}}
	handlers := authenticatedProjectHandlers(service)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v1/projects/11111111-1111-4111-8111-111111111111/monitors/"+monitorID+"/checks?from=2026-09-08T12:00:00Z&to=2026-09-07T12:00:00Z", nil)
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.item(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"code":"invalid_period"`) {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
func (service *configurableProjectService) Create(context.Context, string, projects.CreateProjectInput) (projects.Project, error) {
	return projects.Project{}, nil
}
func (service *configurableProjectService) Update(context.Context, string, string, projects.UpdateProjectInput) (projects.Project, error) {
	return projects.Project{}, nil
}
func (service *configurableProjectService) Delete(context.Context, string, string) error {
	return nil
}
func (service *configurableProjectService) AddMonitor(context.Context, string, string, projects.CreateMonitorInput) (projects.Monitor, error) {
	return projects.Monitor{}, nil
}
func (service *configurableProjectService) UpdateMonitor(context.Context, string, string, string, projects.UpdateMonitorInput) (projects.Monitor, error) {
	return projects.Monitor{}, nil
}
func (service *configurableProjectService) DeleteMonitor(context.Context, string, string, string) error {
	return nil
}
func (service *configurableProjectService) CreateWebhook(
	_ context.Context,
	userID,
	_ string,
	input projects.CreateWebhookInput,
) (projects.Webhook, error) {
	service.createHookUser = userID + ":" + input.URL + ":" + input.Events
	if service.createHookErr != nil {
		return projects.Webhook{}, service.createHookErr
	}

	return service.createdWebhook, nil
}
func (service *configurableProjectService) ListWebhooks(context.Context, string, string) ([]projects.Webhook, error) {
	return service.webhooks, service.listHooksErr
}
func (service *configurableProjectService) DeleteWebhook(_ context.Context, _, _, webhookID string) error {
	service.deletedHookID = webhookID

	return service.deleteHookErr
}

type stubHistoryService struct{}

func (stubHistoryService) CheckPage(context.Context, string, string, string, monitoring.HistoryQuery) (monitoring.CheckPage, error) {
	return monitoring.CheckPage{Checks: []monitoring.Check{}}, nil
}
func (stubHistoryService) IncidentPage(context.Context, string, string, string, monitoring.HistoryQuery) (monitoring.IncidentPage, error) {
	return monitoring.IncidentPage{Incidents: []monitoring.Incident{}}, nil
}
func (stubHistoryService) Summary(context.Context, string, string, string, monitoring.HistoryQuery) (monitoring.MonitorSummary, error) {
	return monitoring.MonitorSummary{}, nil
}

func authenticatedProjectHandlers(service *configurableProjectService) *projectHandlers {
	authentication := &configurableAuthService{currentUser: auth.User{ID: "owner-id", Email: "owner@example.com"}}
	return newProjectHandlers(authentication, service, stubHistoryService{}, Options{AllowedOrigin: "https://app.example.com"})
}

func TestCreateWebhookHandlerReturnsSecretOnce(t *testing.T) {
	created := projects.Webhook{
		ID:        "22222222-2222-4222-8222-222222222222",
		ProjectID: "11111111-1111-4111-8111-111111111111",
		URL:       "https://hooks.example.com/uptime",
		Events:    "down,recovered",
		Enabled:   true,
		Secret:    "raw-secret-shown-once",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	service := &configurableProjectService{createdWebhook: created}
	handlers := authenticatedProjectHandlers(service)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/projects/11111111-1111-4111-8111-111111111111/webhooks",
		strings.NewReader(`{"url":"https://hooks.example.com/uptime","events":"down,recovered"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://app.example.com")
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.item(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusCreated, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"secret":"raw-secret-shown-once"`) {
		t.Fatalf("creation response hides the one-time secret: %s", response.Body.String())
	}
	if location := response.Header().Get("Location"); !strings.HasSuffix(location, "/webhooks/"+created.ID) {
		t.Fatalf("Location = %q, want created webhook URL", location)
	}
	if !strings.HasPrefix(service.createHookUser, "owner-id:") {
		t.Fatalf("webhook owner = %q, want authenticated user", service.createHookUser)
	}
}

func TestCreateWebhookMapsDomainErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		serviceErr error
		wantStatus int
		wantCode   string
	}{
		{"invalid events", projects.ErrInvalidEvents, http.StatusBadRequest, "invalid_events"},
		{"webhook limit", projects.ErrTooManyWebhooks, http.StatusConflict, "webhook_limit"},
		{"unknown project", projects.ErrNotFound, http.StatusNotFound, "project_not_found"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := &configurableProjectService{createHookErr: test.serviceErr}
			handlers := authenticatedProjectHandlers(service)
			request := httptest.NewRequest(
				http.MethodPost,
				"/api/v1/projects/11111111-1111-4111-8111-111111111111/webhooks",
				strings.NewReader(`{"url":"https://hooks.example.com/uptime","events":"down"}`),
			)
			request.Header.Set("Content-Type", "application/json")
			addSessionCookie(request)
			response := httptest.NewRecorder()

			handlers.item(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), test.wantCode) {
				t.Fatalf("body %s does not contain code %q", response.Body.String(), test.wantCode)
			}
		})
	}
}

func TestListWebhooksHandlerOmitsSecrets(t *testing.T) {
	service := &configurableProjectService{webhooks: []projects.Webhook{{
		ID:        "22222222-2222-4222-8222-222222222222",
		ProjectID: "11111111-1111-4111-8111-111111111111",
		URL:       "https://hooks.example.com/uptime",
		Events:    "down",
		Enabled:   true,
	}}}
	handlers := authenticatedProjectHandlers(service)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/projects/11111111-1111-4111-8111-111111111111/webhooks",
		nil,
	)
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.item(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "secret") {
		t.Fatalf("list response leaks secrets: %s", response.Body.String())
	}
}

func TestDeleteWebhookHandlerRequiresConfirmation(t *testing.T) {
	service := &configurableProjectService{}
	handlers := authenticatedProjectHandlers(service)
	request := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/projects/11111111-1111-4111-8111-111111111111/webhooks/22222222-2222-4222-8222-222222222222",
		nil,
	)
	addSessionCookie(request)
	response := httptest.NewRecorder()

	handlers.item(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if service.deletedHookID != "" {
		t.Fatal("webhook was deleted without explicit confirmation")
	}

	confirmed := httptest.NewRequest(
		http.MethodDelete,
		"/api/v1/projects/11111111-1111-4111-8111-111111111111/webhooks/22222222-2222-4222-8222-222222222222",
		nil,
	)
	confirmed.Header.Set("X-Confirm-Delete", "true")
	addSessionCookie(confirmed)
	confirmedResponse := httptest.NewRecorder()

	handlers.item(confirmedResponse, confirmed)

	if confirmedResponse.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", confirmedResponse.Code, http.StatusNoContent)
	}
	if service.deletedHookID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("deleted webhook = %q, want requested ID", service.deletedHookID)
	}
}
