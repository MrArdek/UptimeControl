package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/monitoring"
	"github.com/MrArdek/UptimeControl/internal/projects"
)

const maximumProjectBody = 32 * 1024

type projectService interface {
	List(context.Context, string) ([]projects.Project, error)
	ByID(context.Context, string, string) (projects.Project, error)
	Create(context.Context, string, projects.CreateProjectInput) (projects.Project, error)
	Update(context.Context, string, string, projects.UpdateProjectInput) (projects.Project, error)
	Delete(context.Context, string, string) error
	AddMonitor(context.Context, string, string, projects.CreateMonitorInput) (projects.Monitor, error)
	UpdateMonitor(context.Context, string, string, string, projects.UpdateMonitorInput) (projects.Monitor, error)
	DeleteMonitor(context.Context, string, string, string) error
}

type historyService interface {
	Checks(context.Context, string, string, string, int) ([]monitoring.Check, error)
	Incidents(context.Context, string, string, string, int) ([]monitoring.Incident, error)
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
	CheckIntervalSeconds *int   `json:"check_interval_seconds"`
	TimeoutSeconds       *int   `json:"timeout_seconds"`
}

type updateMonitorRequest struct {
	Name                 *string `json:"name"`
	URL                  *string `json:"url"`
	CheckIntervalSeconds *int    `json:"check_interval_seconds"`
	TimeoutSeconds       *int    `json:"timeout_seconds"`
	Enabled              *bool   `json:"enabled"`
}

type projectResponse struct {
	Project projects.Project `json:"project"`
}

type projectListResponse struct {
	Projects []projects.Project `json:"projects"`
}

type monitorResponse struct {
	Monitor projects.Monitor `json:"monitor"`
}

type checkListResponse struct {
	Checks []monitoring.Check `json:"checks"`
}

type incidentListResponse struct {
	Incidents []monitoring.Incident `json:"incidents"`
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
	default:
		writeError(response, http.StatusNotFound, "project_not_found", "project was not found")
	}
}

func (handlers *projectHandlers) list(response http.ResponseWriter, request *http.Request) {
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	projectList, err := handlers.projects.List(request.Context(), user.ID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusOK, projectListResponse{Projects: projectList})
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
			Name: payload.Name, URL: payload.URL, CheckIntervalSeconds: payload.CheckIntervalSeconds,
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

func (handlers *projectHandlers) checks(response http.ResponseWriter, request *http.Request, projectID, monitorID string) {
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
	checks, err := handlers.history.Checks(request.Context(), user.ID, projectID, monitorID, queryLimit(request))
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusOK, checkListResponse{Checks: checks})
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
	if _, err := handlers.projects.ByID(request.Context(), user.ID, projectID); err != nil {
		handlers.writeProjectError(response, err)
		return
	}
	incidents, err := handlers.history.Incidents(request.Context(), user.ID, projectID, monitorID, queryLimit(request))
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusOK, incidentListResponse{Incidents: incidents})
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
		writeError(response, http.StatusBadRequest, "invalid_monitor_type", "monitor type must be http or heartbeat")
	case errors.Is(err, projects.ErrInvalidURL):
		writeError(response, http.StatusBadRequest, "invalid_url", "URL must be a public HTTP or HTTPS address")
	case errors.Is(err, projects.ErrInvalidInterval):
		writeError(response, http.StatusBadRequest, "invalid_interval", "check interval must be between 30 and 86400 seconds")
	case errors.Is(err, projects.ErrInvalidTimeout):
		writeError(response, http.StatusBadRequest, "invalid_timeout", "timeout must be between 1 and 30 seconds")
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
		Type: request.Type, Name: request.Name, URL: request.URL,
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
