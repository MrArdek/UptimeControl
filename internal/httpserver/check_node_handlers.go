package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/checknodes"
)

const maximumNodeBody = 256 * 1024

type checkNodeService interface {
	Create(context.Context, string, string, string) (checknodes.Node, error)
	List(context.Context, string) ([]checknodes.Node, error)
	Revoke(context.Context, string, string) error
	Assign(context.Context, string, string, string) (checknodes.Assignment, error)
	RemoveAssignment(context.Context, string, string, string) error
	Authenticate(context.Context, string) (checknodes.Node, error)
	ClaimAssignments(context.Context, checknodes.Node, int) ([]checknodes.Assignment, error)
	RecordResults(context.Context, checknodes.Node, []checknodes.Result) (checknodes.BatchOutcome, error)
}

type checkNodeHandlers struct {
	authentication authService
	nodes          checkNodeService
	options        Options
	nodeLimiter    *fixedWindowLimiter
}

func newCheckNodeHandlers(authentication authService, nodes checkNodeService, options Options) *checkNodeHandlers {
	return &checkNodeHandlers{authentication: authentication, nodes: nodes, options: options, nodeLimiter: newFixedWindowLimiter(240, time.Minute)}
}

func (handlers *checkNodeHandlers) collection(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		user, ok := handlers.authorizeOwner(response, request)
		if !ok {
			return
		}
		nodes, err := handlers.nodes.List(request.Context(), user.ID)
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
			return
		}
		writeJSON(response, http.StatusOK, struct {
			Nodes []checknodes.Node `json:"nodes"`
		}{Nodes: nodes})
	case http.MethodPost:
		if !originAllowed(request, handlers.options.AllowedOrigin) {
			writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
			return
		}
		user, ok := handlers.authorizeOwner(response, request)
		if !ok {
			return
		}
		var payload struct {
			Name   string `json:"name"`
			Region string `json:"region"`
		}
		if decodeJSONRequest(response, request, maximumNodeBody, &payload) != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "node fields must be a valid JSON object")
			return
		}
		node, err := handlers.nodes.Create(request.Context(), user.ID, payload.Name, payload.Region)
		if handlers.writeNodeError(response, err) {
			return
		}
		writeJSON(response, http.StatusCreated, struct {
			Node checknodes.Node `json:"node"`
		}{Node: node})
	default:
		methodNotAllowed(response, http.MethodGet+", "+http.MethodPost)
	}
}

func (handlers *checkNodeHandlers) item(response http.ResponseWriter, request *http.Request) {
	remainder := strings.Trim(strings.TrimPrefix(request.URL.Path, routePath(handlers.options.BasePath, "/api/v1/check-nodes/")), "/")
	parts := strings.Split(remainder, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeError(response, http.StatusNotFound, "node_not_found", "check node was not found")
		return
	}
	if !originAllowed(request, handlers.options.AllowedOrigin) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	user, ok := handlers.authorizeOwner(response, request)
	if !ok {
		return
	}
	switch {
	case len(parts) == 1 && request.Method == http.MethodDelete:
		if request.Header.Get("X-Confirm-Revoke") != "true" {
			writeError(response, http.StatusBadRequest, "confirmation_required", "node revocation must be explicitly confirmed")
			return
		}
		if handlers.writeNodeError(response, handlers.nodes.Revoke(request.Context(), user.ID, parts[0])) {
			return
		}
		response.WriteHeader(http.StatusNoContent)
	case len(parts) == 2 && parts[1] == "assignments" && request.Method == http.MethodPost:
		var payload struct {
			MonitorID string `json:"monitor_id"`
		}
		if decodeJSONRequest(response, request, maximumNodeBody, &payload) != nil {
			writeError(response, http.StatusBadRequest, "invalid_request", "assignment fields must be a valid JSON object")
			return
		}
		assignment, err := handlers.nodes.Assign(request.Context(), user.ID, parts[0], payload.MonitorID)
		if handlers.writeNodeError(response, err) {
			return
		}
		writeJSON(response, http.StatusCreated, struct {
			Assignment checknodes.Assignment `json:"assignment"`
		}{Assignment: assignment})
	case len(parts) == 3 && parts[1] == "assignments" && request.Method == http.MethodDelete:
		if request.Header.Get("X-Confirm-Delete") != "true" {
			writeError(response, http.StatusBadRequest, "confirmation_required", "assignment deletion must be explicitly confirmed")
			return
		}
		if handlers.writeNodeError(response, handlers.nodes.RemoveAssignment(request.Context(), user.ID, parts[0], parts[2])) {
			return
		}
		response.WriteHeader(http.StatusNoContent)
	default:
		writeError(response, http.StatusNotFound, "node_not_found", "check node route was not found")
	}
}

func (handlers *checkNodeHandlers) assignments(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response, http.MethodGet)
		return
	}
	node, ok := handlers.authorizeNode(response, request)
	if !ok {
		return
	}
	if !handlers.nodeLimiter.Allow(node.ID) {
		rateLimited(response)
		return
	}
	limit, _ := strconv.Atoi(request.URL.Query().Get("limit"))
	assignments, err := handlers.nodes.ClaimAssignments(request.Context(), node, limit)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	writeJSON(response, http.StatusOK, struct {
		Assignments []checknodes.Assignment `json:"assignments"`
	}{Assignments: assignments})
}

func (handlers *checkNodeHandlers) results(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response, http.MethodPost)
		return
	}
	node, ok := handlers.authorizeNode(response, request)
	if !ok {
		return
	}
	if !handlers.nodeLimiter.Allow(node.ID) {
		rateLimited(response)
		return
	}
	var payload struct {
		Results []checknodes.Result `json:"results"`
	}
	if decodeJSONRequest(response, request, maximumNodeBody, &payload) != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "results must be a valid JSON batch")
		return
	}
	outcome, err := handlers.nodes.RecordResults(request.Context(), node, payload.Results)
	if handlers.writeNodeError(response, err) {
		return
	}
	writeJSON(response, http.StatusAccepted, outcome)
}

func (handlers *checkNodeHandlers) authorizeOwner(response http.ResponseWriter, request *http.Request) (auth.User, bool) {
	token, err := sessionToken(request)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return auth.User{}, false
	}
	user, err := handlers.authentication.CurrentUser(request.Context(), token)
	if errors.Is(err, auth.ErrUnauthorized) {
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return auth.User{}, false
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return auth.User{}, false
	}
	return user, true
}

func (handlers *checkNodeHandlers) authorizeNode(response http.ResponseWriter, request *http.Request) (checknodes.Node, bool) {
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, "Bearer ") || strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")) == "" {
		response.Header().Set("WWW-Authenticate", "Bearer")
		writeError(response, http.StatusUnauthorized, "node_unauthorized", "valid node credentials are required")
		return checknodes.Node{}, false
	}
	node, err := handlers.nodes.Authenticate(request.Context(), strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")))
	if errors.Is(err, checknodes.ErrUnauthorized) {
		response.Header().Set("WWW-Authenticate", "Bearer")
		writeError(response, http.StatusUnauthorized, "node_unauthorized", "valid node credentials are required")
		return checknodes.Node{}, false
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return checknodes.Node{}, false
	}
	return node, true
}

func (handlers *checkNodeHandlers) writeNodeError(response http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, checknodes.ErrNotFound):
		writeError(response, http.StatusNotFound, "node_not_found", "check node or assignment was not found")
	case errors.Is(err, checknodes.ErrInvalidInput):
		writeError(response, http.StatusBadRequest, "invalid_node", "name or region is invalid")
	case errors.Is(err, checknodes.ErrInvalidResult):
		writeError(response, http.StatusUnprocessableEntity, "invalid_result", "result batch violates the monitoring contract")
	case errors.Is(err, checknodes.ErrAlreadyAssigned):
		writeError(response, http.StatusConflict, "already_assigned", "monitor is already assigned to this node")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
	}
	return true
}
