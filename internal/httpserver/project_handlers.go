package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/monitoring"
	"github.com/MrArdek/UptimeControl/internal/projects"
)

const maximumProjectBody = 32 * 1024

type projectService interface {
	ListPage(context.Context, string, int, string) (projects.ProjectPage, error)
	ByID(context.Context, string, string) (projects.Project, error)
	Create(context.Context, string, projects.CreateProjectInput) (projects.Project, error)
	Update(context.Context, string, string, projects.UpdateProjectInput) (projects.Project, error)
	Delete(context.Context, string, string) error
	AddMonitor(context.Context, string, string, projects.CreateMonitorInput) (projects.Monitor, error)
	UpdateMonitor(context.Context, string, string, string, projects.UpdateMonitorInput) (projects.Monitor, error)
	DeleteMonitor(context.Context, string, string, string) error
	CreateWebhook(context.Context, string, string, projects.CreateWebhookInput) (projects.Webhook, error)
	UpdateWebhook(context.Context, string, string, string, projects.UpdateWebhookInput) (projects.Webhook, error)
	ListWebhooks(context.Context, string, string) ([]projects.Webhook, error)
	DeleteWebhook(context.Context, string, string, string) error
}

type historyService interface {
	CheckPage(context.Context, string, string, string, monitoring.HistoryQuery) (monitoring.CheckPage, error)
	IncidentPage(context.Context, string, string, string, monitoring.HistoryQuery) (monitoring.IncidentPage, error)
	Summary(context.Context, string, string, string, monitoring.HistoryQuery) (monitoring.MonitorSummary, error)
	ListNotifications(context.Context, string, string, int, string) (monitoring.NotificationPage, error)
	EnqueueTestNotification(context.Context, string, string, time.Time) (monitoring.NotificationEvent, error)
	RetryFailedNotification(context.Context, string, string, string, time.Time) error
}

type projectHandlers struct {
	authentication authService
	projects       projectService
	history        historyService
	options        Options
}

type createProjectRequest struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Monitor     createMonitorRequest `json:"monitor"`
}

type updateProjectRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

type createMonitorRequest struct {
	Type                 string `json:"type"`
	Name                 string `json:"name"`
	URL                  string `json:"url"`
	Target               string `json:"target"`
	CheckIntervalSeconds *int   `json:"check_interval_seconds"`
	TimeoutSeconds       *int   `json:"timeout_seconds"`
}

type updateMonitorRequest struct {
	Name                 *string `json:"name"`
	URL                  *string `json:"url"`
	Target               *string `json:"target"`
	CheckIntervalSeconds *int    `json:"check_interval_seconds"`
	TimeoutSeconds       *int    `json:"timeout_seconds"`
	Enabled              *bool   `json:"enabled"`
}

type projectResponse struct {
	Project projects.Project `json:"project"`
}

type monitorResponse struct {
	Monitor projects.Monitor `json:"monitor"`
}

type createWebhookRequest struct {
	URL    string `json:"url"`
	Events string `json:"events"`
}

type updateWebhookRequest struct {
	Events  *string `json:"events"`
	Enabled *bool   `json:"enabled"`
}

type webhookResponse struct {
	Webhook projects.Webhook `json:"webhook"`
}

type webhookListResponse struct {
	Webhooks []projects.Webhook `json:"webhooks"`
}

func newProjectHandlers(
	authentication authService,
	projectManagement projectService,
	history historyService,
	options Options,
) *projectHandlers {
	return &projectHandlers{
		authentication: authentication,
		projects:       projectManagement,
		history:        history,
		options:        options,
	}
}

func (handlers *projectHandlers) collection(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		handlers.list(response, request)
	case http.MethodPost:
		handlers.create(response, request)
	default:
		methodNotAllowed(response, http.MethodGet+", "+http.MethodPost)
	}
}

func (handlers *projectHandlers) item(response http.ResponseWriter, request *http.Request) {
	remainder := strings.TrimPrefix(request.URL.Path, routePath(handlers.options.BasePath, "/api/v1/projects/"))
	parts := strings.Split(strings.Trim(remainder, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(response, http.StatusNotFound, "project_not_found", "project was not found")
		return
	}
	projectID := parts[0]

	switch {
	case len(parts) == 1:
		handlers.projectItem(response, request, projectID)
	case len(parts) == 2 && parts[1] == "monitors":
		handlers.monitorCollection(response, request, projectID)
	case len(parts) == 3 && parts[1] == "monitors":
		handlers.monitorItem(response, request, projectID, parts[2])
	case len(parts) == 4 && parts[1] == "monitors" && parts[3] == "checks":
		handlers.checks(response, request, projectID, parts[2])
	case len(parts) == 4 && parts[1] == "monitors" && parts[3] == "incidents":
		handlers.incidents(response, request, projectID, parts[2])
	case len(parts) == 4 && parts[1] == "monitors" && parts[3] == "summary":
		handlers.summary(response, request, projectID, parts[2])
	case len(parts) == 2 && parts[1] == "webhooks":
		handlers.webhookCollection(response, request, projectID)
	case len(parts) == 3 && parts[1] == "webhooks":
		handlers.webhookItem(response, request, projectID, parts[2])
	case len(parts) == 2 && parts[1] == "notifications":
		handlers.notifications(response, request, projectID)
	case len(parts) == 3 && parts[1] == "notifications" && parts[2] == "test":
		handlers.testNotification(response, request, projectID)
	case len(parts) == 4 && parts[1] == "notifications" && parts[3] == "retry":
		handlers.retryNotification(response, request, projectID, parts[2])
	default:
		writeError(response, http.StatusNotFound, "project_not_found", "project was not found")
	}
}

func (handlers *projectHandlers) notifications(response http.ResponseWriter, request *http.Request, projectID string) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response, http.MethodGet)
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	if _, err := handlers.projects.ByID(request.Context(), user.ID, projectID); err != nil {
		handlers.writeProjectError(response, err)
		return
	}
	page, err := handlers.history.ListNotifications(
		request.Context(), user.ID, projectID, queryLimit(request), request.URL.Query().Get("cursor"),
	)
	if errors.Is(err, monitoring.ErrInvalidCursor) {
		writeError(response, http.StatusBadRequest, "invalid_cursor", "cursor is invalid for this notification list")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (handlers *projectHandlers) testNotification(response http.ResponseWriter, request *http.Request, projectID string) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response, http.MethodPost)
		return
	}
	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	event, err := handlers.history.EnqueueTestNotification(request.Context(), user.ID, projectID, time.Now().UTC())
	if errors.Is(err, monitoring.ErrNotFound) {
		writeError(response, http.StatusNotFound, "project_not_found", "project was not found")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusAccepted, struct {
		Notification monitoring.NotificationEvent `json:"notification"`
	}{Notification: event})
}

func (handlers *projectHandlers) retryNotification(response http.ResponseWriter, request *http.Request, projectID, notificationID string) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response, http.MethodPost)
		return
	}
	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	if request.Header.Get("X-Confirm-Retry") != "true" {
		writeError(response, http.StatusBadRequest, "confirmation_required", "notification retry must be explicitly confirmed")
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	err := handlers.history.RetryFailedNotification(request.Context(), user.ID, projectID, notificationID, time.Now().UTC())
	if errors.Is(err, monitoring.ErrNotFound) {
		writeError(response, http.StatusNotFound, "notification_not_found", "failed notification was not found")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handlers *projectHandlers) list(response http.ResponseWriter, request *http.Request) {
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	projectPage, err := handlers.projects.ListPage(request.Context(), user.ID, queryLimit(request), request.URL.Query().Get("cursor"))
	if errors.Is(err, projects.ErrInvalidCursor) {
		writeError(response, http.StatusBadRequest, "invalid_cursor", "cursor is invalid for this project list")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusOK, projectPage)
}

func (handlers *projectHandlers) create(response http.ResponseWriter, request *http.Request) {
	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	var payload createProjectRequest
	if err := decodeJSONRequest(response, request, maximumProjectBody, &payload); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "project fields must be a valid JSON object")
		return
	}
	project, err := handlers.projects.Create(request.Context(), user.ID, projects.CreateProjectInput{
		Name:        payload.Name,
		Description: payload.Description,
		Monitor:     payload.Monitor.input(),
	})
	if handlers.writeProjectError(response, err) {
		return
	}
	response.Header().Set("Location", routePath(handlers.options.BasePath, "/api/v1/projects/"+project.ID))
	writeJSON(response, http.StatusCreated, projectResponse{Project: project})
}

func (handlers *projectHandlers) projectItem(response http.ResponseWriter, request *http.Request, projectID string) {
	switch request.Method {
	case http.MethodGet:
		user, ok := handlers.authorize(response, request)
		if !ok {
			return
		}
		project, err := handlers.projects.ByID(request.Context(), user.ID, projectID)
		if handlers.writeProjectError(response, err) {
			return
		}
		writeJSON(response, http.StatusOK, projectResponse{Project: project})
	case http.MethodPatch:
		if !handlers.validOrigin(request) {
			writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
			return
		}
		user, ok := handlers.authorize(response, request)
		if !ok {
			return
		}
		var payload updateProjectRequest
		if err := decodeJSONRequest(response, request, maximumProjectBody, &payload); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "project fields must be a valid JSON object")
			return
		}
		project, err := handlers.projects.Update(request.Context(), user.ID, projectID, projects.UpdateProjectInput{
			Name: payload.Name, Description: payload.Description,
		})
		if handlers.writeProjectError(response, err) {
			return
		}
		writeJSON(response, http.StatusOK, projectResponse{Project: project})
	case http.MethodDelete:
		if !handlers.validOrigin(request) {
			writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
			return
		}
		user, ok := handlers.authorize(response, request)
		if !ok {
			return
		}
		if request.Header.Get("X-Confirm-Delete") != "true" {
			writeError(response, http.StatusBadRequest, "confirmation_required", "project deletion must be explicitly confirmed")
			return
		}
		if handlers.writeProjectError(response, handlers.projects.Delete(request.Context(), user.ID, projectID)) {
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(response, http.MethodGet+", "+http.MethodPatch+", "+http.MethodDelete)
	}
}

func (handlers *projectHandlers) monitorCollection(response http.ResponseWriter, request *http.Request, projectID string) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response, http.MethodPost)
		return
	}
	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	var payload createMonitorRequest
	if err := decodeJSONRequest(response, request, maximumProjectBody, &payload); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "monitor fields must be a valid JSON object")
		return
	}
	monitor, err := handlers.projects.AddMonitor(request.Context(), user.ID, projectID, payload.input())
	if handlers.writeProjectError(response, err) {
		return
	}
	response.Header().Set("Location", routePath(handlers.options.BasePath, "/api/v1/projects/"+projectID+"/monitors/"+monitor.ID))
	writeJSON(response, http.StatusCreated, monitorResponse{Monitor: monitor})
}

func (handlers *projectHandlers) monitorItem(response http.ResponseWriter, request *http.Request, projectID, monitorID string) {
	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodPatch:
		var payload updateMonitorRequest
		if err := decodeJSONRequest(response, request, maximumProjectBody, &payload); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "monitor fields must be a valid JSON object")
			return
		}
		monitor, err := handlers.projects.UpdateMonitor(request.Context(), user.ID, projectID, monitorID, projects.UpdateMonitorInput{
			Name: payload.Name, URL: payload.URL, Target: payload.Target, CheckIntervalSeconds: payload.CheckIntervalSeconds,
			TimeoutSeconds: payload.TimeoutSeconds, Enabled: payload.Enabled,
		})
		if handlers.writeProjectError(response, err) {
			return
		}
		writeJSON(response, http.StatusOK, monitorResponse{Monitor: monitor})
	case http.MethodDelete:
		if request.Header.Get("X-Confirm-Delete") != "true" {
			writeError(response, http.StatusBadRequest, "confirmation_required", "monitor deletion must be explicitly confirmed")
			return
		}
		if handlers.writeProjectError(response, handlers.projects.DeleteMonitor(request.Context(), user.ID, projectID, monitorID)) {
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(response, http.MethodPatch+", "+http.MethodDelete)
	}
}

func (handlers *projectHandlers) webhookCollection(response http.ResponseWriter, request *http.Request, projectID string) {
	switch request.Method {
	case http.MethodGet:
		handlers.listWebhooks(response, request, projectID)
	case http.MethodPost:
		handlers.createWebhook(response, request, projectID)
	default:
		methodNotAllowed(response, http.MethodGet+", "+http.MethodPost)
	}
}

func (handlers *projectHandlers) webhookItem(response http.ResponseWriter, request *http.Request, projectID, webhookID string) {
	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	switch request.Method {
	case http.MethodPatch:
		var payload updateWebhookRequest
		if err := decodeJSONRequest(response, request, maximumProjectBody, &payload); err != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "webhook fields must be a valid JSON object")
			return
		}
		hook, err := handlers.projects.UpdateWebhook(request.Context(), user.ID, projectID, webhookID, projects.UpdateWebhookInput{
			Events: payload.Events, Enabled: payload.Enabled,
		})
		if handlers.writeProjectError(response, err) {
			return
		}
		writeJSON(response, http.StatusOK, webhookResponse{Webhook: hook})
	case http.MethodDelete:
		if request.Header.Get("X-Confirm-Delete") != "true" {
			writeError(response, http.StatusBadRequest, "confirmation_required", "webhook deletion must be explicitly confirmed")
			return
		}
		if handlers.writeProjectError(response, handlers.projects.DeleteWebhook(request.Context(), user.ID, projectID, webhookID)) {
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNoContent)
	default:
		methodNotAllowed(response, http.MethodPatch+", "+http.MethodDelete)
	}
}

func (handlers *projectHandlers) listWebhooks(response http.ResponseWriter, request *http.Request, projectID string) {
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	hooks, err := handlers.projects.ListWebhooks(request.Context(), user.ID, projectID)
	if handlers.writeProjectError(response, err) {
		return
	}
	writeJSON(response, http.StatusOK, webhookListResponse{Webhooks: hooks})
}

func (handlers *projectHandlers) createWebhook(response http.ResponseWriter, request *http.Request, projectID string) {
	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	var payload createWebhookRequest
	if err := decodeJSONRequest(response, request, maximumProjectBody, &payload); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "webhook fields must be a valid JSON object")
		return
	}
	hook, err := handlers.projects.CreateWebhook(request.Context(), user.ID, projectID, projects.CreateWebhookInput{
		URL:    payload.URL,
		Events: payload.Events,
	})
	if handlers.writeProjectError(response, err) {
		return
	}
	response.Header().Set("Location", routePath(handlers.options.BasePath, "/api/v1/projects/"+projectID+"/webhooks/"+hook.ID))
	writeJSON(response, http.StatusCreated, webhookResponse{Webhook: hook})
}

func (handlers *projectHandlers) checks(response http.ResponseWriter, request *http.Request, projectID, monitorID string) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response, http.MethodGet)
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	project, err := handlers.projects.ByID(request.Context(), user.ID, projectID)
	if err != nil {
		handlers.writeProjectError(response, err)
		return
	}
	if !projectHasMonitor(project, monitorID) {
		writeError(response, http.StatusNotFound, "monitor_not_found", "monitor was not found")
		return
	}
	query, ok := handlers.historyQuery(response, request)
	if !ok {
		return
	}
	checks, err := handlers.history.CheckPage(request.Context(), user.ID, projectID, monitorID, query)
	if errors.Is(err, monitoring.ErrInvalidCursor) {
		writeError(response, http.StatusBadRequest, "invalid_cursor", "cursor is invalid for this check history")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusOK, checks)
}

func (handlers *projectHandlers) incidents(response http.ResponseWriter, request *http.Request, projectID, monitorID string) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response, http.MethodGet)
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	project, err := handlers.projects.ByID(request.Context(), user.ID, projectID)
	if err != nil {
		handlers.writeProjectError(response, err)
		return
	}
	if !projectHasMonitor(project, monitorID) {
		writeError(response, http.StatusNotFound, "monitor_not_found", "monitor was not found")
		return
	}
	query, ok := handlers.historyQuery(response, request)
	if !ok {
		return
	}
	incidents, err := handlers.history.IncidentPage(request.Context(), user.ID, projectID, monitorID, query)
	if errors.Is(err, monitoring.ErrInvalidCursor) {
		writeError(response, http.StatusBadRequest, "invalid_cursor", "cursor is invalid for this incident history")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusOK, incidents)
}

func (handlers *projectHandlers) summary(response http.ResponseWriter, request *http.Request, projectID, monitorID string) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response, http.MethodGet)
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	query, ok := handlers.historyQuery(response, request)
	if !ok {
		return
	}
	summary, err := handlers.history.Summary(request.Context(), user.ID, projectID, monitorID, query)
	if errors.Is(err, monitoring.ErrNotFound) {
		writeError(response, http.StatusNotFound, "monitor_not_found", "monitor was not found")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Summary monitoring.MonitorSummary `json:"summary"`
	}{Summary: summary})
}

func (handlers *projectHandlers) historyQuery(response http.ResponseWriter, request *http.Request) (monitoring.HistoryQuery, bool) {
	query, err := monitoring.NewHistoryQuery(
		queryLimit(request),
		request.URL.Query().Get("cursor"),
		request.URL.Query().Get("from"),
		request.URL.Query().Get("to"),
		time.Now().UTC(),
	)
	if errors.Is(err, monitoring.ErrInvalidPeriod) {
		writeError(response, http.StatusBadRequest, "invalid_period", "from and to must define a valid period of at most 31 days")
		return monitoring.HistoryQuery{}, false
	}
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "history query is invalid")
		return monitoring.HistoryQuery{}, false
	}
	return query, true
}

func projectHasMonitor(project projects.Project, monitorID string) bool {
	for _, monitor := range project.Monitors {
		if monitor.ID == monitorID {
			return true
		}
	}
	return false
}

func (handlers *projectHandlers) authorize(response http.ResponseWriter, request *http.Request) (auth.User, bool) {
	token, err := sessionToken(request)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return auth.User{}, false
	}
	user, err := handlers.authentication.CurrentUser(request.Context(), token)
	if errors.Is(err, auth.ErrUnauthorized) {
		clearSessionCookie(response, handlers.options)
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return auth.User{}, false
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return auth.User{}, false
	}
	return user, true
}

func (handlers *projectHandlers) validOrigin(request *http.Request) bool {
	return originAllowed(request, handlers.options.AllowedOrigin)
}

func (handlers *projectHandlers) writeProjectError(response http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, projects.ErrInvalidName):
		writeError(response, http.StatusBadRequest, "invalid_name", "name must contain 1 to 100 characters")
	case errors.Is(err, projects.ErrInvalidDescription):
		writeError(response, http.StatusBadRequest, "invalid_description", "description is too long")
	case errors.Is(err, projects.ErrInvalidType):
		writeError(response, http.StatusBadRequest, "invalid_monitor_type", "monitor type must be http, tcp or heartbeat")
	case errors.Is(err, projects.ErrInvalidURL):
		writeError(response, http.StatusBadRequest, "invalid_url", "URL must be a public HTTP or HTTPS address")
	case errors.Is(err, projects.ErrInvalidTarget):
		writeError(response, http.StatusBadRequest, "invalid_target", "target must be a public host and port")
	case errors.Is(err, projects.ErrInvalidInterval):
		writeError(response, http.StatusBadRequest, "invalid_interval", "check interval must be between 30 and 86400 seconds")
	case errors.Is(err, projects.ErrInvalidTimeout):
		writeError(response, http.StatusBadRequest, "invalid_timeout", "timeout must be between 1 and 30 seconds")
	case errors.Is(err, projects.ErrInvalidEvents):
		writeError(response, http.StatusBadRequest, "invalid_events", "events must be a subset of down,recovered")
	case errors.Is(err, projects.ErrTooManyWebhooks):
		writeError(response, http.StatusConflict, "webhook_limit", "this project already has the maximum number of webhooks")
	case errors.Is(err, projects.ErrEmptyUpdate):
		writeError(response, http.StatusBadRequest, "empty_update", "at least one field must be provided")
	case errors.Is(err, projects.ErrAlreadyExists):
		writeError(response, http.StatusConflict, "monitor_exists", "this monitor is already registered")
	case errors.Is(err, projects.ErrNotFound):
		writeError(response, http.StatusNotFound, "project_not_found", "project or monitor was not found")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
	}
	return true
}

func (request createMonitorRequest) input() projects.CreateMonitorInput {
	return projects.CreateMonitorInput{
		Type: request.Type, Name: request.Name, URL: request.URL, Target: request.Target,
		CheckIntervalSeconds: request.CheckIntervalSeconds, TimeoutSeconds: request.TimeoutSeconds,
	}
}

func queryLimit(request *http.Request) int {
	limit, err := strconv.Atoi(request.URL.Query().Get("limit"))
	if err != nil {
		return 100
	}
	return limit
}
