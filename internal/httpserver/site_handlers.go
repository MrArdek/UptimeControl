package httpserver

import (
	"errors"
	"net/http"
	"strings"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/sites"
)

const maximumSiteBody = 16 * 1024

type siteHandlers struct {
	authentication authService
	sites          siteService
	options        Options
}

type createSiteRequest struct {
	Name                 string `json:"name"`
	URL                  string `json:"url"`
	CheckIntervalSeconds *int   `json:"check_interval_seconds"`
}

type updateSiteRequest struct {
	Name                 *string `json:"name"`
	URL                  *string `json:"url"`
	CheckIntervalSeconds *int    `json:"check_interval_seconds"`
	Enabled              *bool   `json:"enabled"`
}

type siteResponse struct {
	Site sites.Site `json:"site"`
}

type siteListResponse struct {
	Sites []sites.Site `json:"sites"`
}

func newSiteHandlers(authentication authService, siteManagement siteService, options Options) *siteHandlers {
	return &siteHandlers{
		authentication: authentication,
		sites:          siteManagement,
		options:        options,
	}
}

func (handlers *siteHandlers) collection(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		handlers.list(response, request)
	case http.MethodPost:
		handlers.create(response, request)
	default:
		methodNotAllowed(response, http.MethodGet+", "+http.MethodPost)
	}
}

func (handlers *siteHandlers) item(response http.ResponseWriter, request *http.Request) {
	siteID := strings.TrimPrefix(request.URL.Path, "/api/v1/sites/")
	if siteID == "" || strings.Contains(siteID, "/") {
		writeError(response, http.StatusNotFound, "site_not_found", "site was not found")
		return
	}

	switch request.Method {
	case http.MethodGet:
		handlers.get(response, request, siteID)
	case http.MethodPatch:
		handlers.update(response, request, siteID)
	case http.MethodDelete:
		handlers.delete(response, request, siteID)
	default:
		methodNotAllowed(response, http.MethodGet+", "+http.MethodPatch+", "+http.MethodDelete)
	}
}

func (handlers *siteHandlers) list(response http.ResponseWriter, request *http.Request) {
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}

	siteList, err := handlers.sites.List(request.Context(), user.ID)
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}

	writeJSON(response, http.StatusOK, siteListResponse{Sites: siteList})
}

func (handlers *siteHandlers) create(response http.ResponseWriter, request *http.Request) {
	if !originAllowed(request, handlers.options.AllowedOrigin) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}

	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}

	var payload createSiteRequest
	if err := decodeJSONRequest(response, request, maximumSiteBody, &payload); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "site fields must be a valid JSON object")
		return
	}

	site, err := handlers.sites.Create(request.Context(), user.ID, sites.CreateInput{
		Name:                 payload.Name,
		URL:                  payload.URL,
		CheckIntervalSeconds: payload.CheckIntervalSeconds,
	})
	if handlers.writeSiteError(response, err) {
		return
	}

	response.Header().Set("Location", "/api/v1/sites/"+site.ID)
	writeJSON(response, http.StatusCreated, siteResponse{Site: site})
}

func (handlers *siteHandlers) get(response http.ResponseWriter, request *http.Request, siteID string) {
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}

	site, err := handlers.sites.ByID(request.Context(), user.ID, siteID)
	if handlers.writeSiteError(response, err) {
		return
	}

	writeJSON(response, http.StatusOK, siteResponse{Site: site})
}

func (handlers *siteHandlers) update(response http.ResponseWriter, request *http.Request, siteID string) {
	if !originAllowed(request, handlers.options.AllowedOrigin) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}

	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}

	var payload updateSiteRequest
	if err := decodeJSONRequest(response, request, maximumSiteBody, &payload); err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "site fields must be a valid JSON object")
		return
	}

	site, err := handlers.sites.Update(request.Context(), user.ID, siteID, sites.UpdateInput{
		Name:                 payload.Name,
		URL:                  payload.URL,
		CheckIntervalSeconds: payload.CheckIntervalSeconds,
		Enabled:              payload.Enabled,
	})
	if handlers.writeSiteError(response, err) {
		return
	}

	writeJSON(response, http.StatusOK, siteResponse{Site: site})
}

func (handlers *siteHandlers) delete(response http.ResponseWriter, request *http.Request, siteID string) {
	if !originAllowed(request, handlers.options.AllowedOrigin) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}
	user, ok := handlers.authorize(response, request)
	if !ok {
		return
	}
	if request.Header.Get("X-Confirm-Delete") != "true" {
		writeError(response, http.StatusBadRequest, "confirmation_required", "site deletion must be explicitly confirmed")
		return
	}

	if err := handlers.sites.Delete(request.Context(), user.ID, siteID); err != nil {
		if errors.Is(err, sites.ErrNotFound) {
			writeError(response, http.StatusNotFound, "site_not_found", "site was not found")
			return
		}

		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}

	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (handlers *siteHandlers) authorize(
	response http.ResponseWriter,
	request *http.Request,
) (auth.User, bool) {
	token, err := sessionToken(request)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return auth.User{}, false
	}

	user, err := handlers.authentication.CurrentUser(request.Context(), token)
	if errors.Is(err, auth.ErrUnauthorized) {
		clearSessionCookie(response, handlers.options.CookieSecure)
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return auth.User{}, false
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return auth.User{}, false
	}

	return user, true
}

func (handlers *siteHandlers) writeSiteError(response http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, sites.ErrInvalidName):
		writeError(response, http.StatusBadRequest, "invalid_name", "name must contain 1 to 100 characters")
	case errors.Is(err, sites.ErrInvalidURL):
		writeError(response, http.StatusBadRequest, "invalid_url", "URL must be a public HTTP or HTTPS address")
	case errors.Is(err, sites.ErrInvalidInterval):
		writeError(response, http.StatusBadRequest, "invalid_interval", "check interval must be between 30 and 86400 seconds")
	case errors.Is(err, sites.ErrEmptyUpdate):
		writeError(response, http.StatusBadRequest, "empty_update", "at least one field must be provided")
	case errors.Is(err, sites.ErrAlreadyExists):
		writeError(response, http.StatusConflict, "site_exists", "this site is already registered")
	case errors.Is(err, sites.ErrNotFound):
		writeError(response, http.StatusNotFound, "site_not_found", "site was not found")
	default:
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
	}

	return true
}

func originAllowed(request *http.Request, allowedOrigin string) bool {
	origin := request.Header.Get("Origin")
	return origin == "" || origin == allowedOrigin
}
