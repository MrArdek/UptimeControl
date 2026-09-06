package httpserver

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
)

const (
	sessionCookieName = "uptime_session"
	maximumAuthBody   = 16 * 1024
)

type authHandlers struct {
	service         authService
	options         Options
	registerLimiter *fixedWindowLimiter
	loginLimiter    *fixedWindowLimiter
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userResponse struct {
	User auth.User `json:"user"`
}

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func newAuthHandlers(service authService, options Options) *authHandlers {
	return &authHandlers{
		service:         service,
		options:         options,
		registerLimiter: newFixedWindowLimiter(5, time.Minute),
		loginLimiter:    newFixedWindowLimiter(10, time.Minute),
	}
}

func (handlers *authHandlers) register(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response, http.MethodPost)
		return
	}

	if !handlers.registerLimiter.Allow(clientIP(request)) {
		rateLimited(response)
		return
	}

	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}

	credentials, err := decodeCredentials(response, request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "email and password must be valid JSON fields")
		return
	}

	result, err := handlers.service.Register(request.Context(), credentials.Email, credentials.Password)
	switch {
	case errors.Is(err, auth.ErrInvalidEmail):
		writeError(response, http.StatusBadRequest, "invalid_email", "email is invalid")
		return
	case errors.Is(err, auth.ErrInvalidPassword):
		writeError(response, http.StatusBadRequest, "invalid_password", "password must contain 12 to 128 characters without surrounding spaces")
		return
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(response, http.StatusConflict, "email_taken", "email is already registered")
		return
	case err != nil:
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}

	setSessionCookie(response, result, handlers.options)
	writeJSON(response, http.StatusCreated, userResponse{User: result.User})
}

func (handlers *authHandlers) login(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response, http.MethodPost)
		return
	}

	if !handlers.loginLimiter.Allow(clientIP(request)) {
		rateLimited(response)
		return
	}

	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}

	credentials, err := decodeCredentials(response, request)
	if err != nil {
		writeError(response, http.StatusBadRequest, "invalid_request", "email and password must be valid JSON fields")
		return
	}

	result, err := handlers.service.Login(request.Context(), credentials.Email, credentials.Password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		writeError(response, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}

	setSessionCookie(response, result, handlers.options)
	writeJSON(response, http.StatusOK, userResponse{User: result.User})
}

func (handlers *authHandlers) logout(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(response, http.MethodPost)
		return
	}

	if !handlers.validOrigin(request) {
		writeError(response, http.StatusForbidden, "invalid_origin", "request origin is not allowed")
		return
	}

	token, err := sessionToken(request)
	if err != nil || handlers.service.Logout(request.Context(), token) != nil {
		clearSessionCookie(response, handlers.options)
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return
	}

	clearSessionCookie(response, handlers.options)
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(http.StatusNoContent)
}

func (handlers *authHandlers) me(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response, http.MethodGet)
		return
	}

	token, err := sessionToken(request)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return
	}

	user, err := handlers.service.CurrentUser(request.Context(), token)
	if errors.Is(err, auth.ErrUnauthorized) {
		clearSessionCookie(response, handlers.options)
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}

	writeJSON(response, http.StatusOK, userResponse{User: user})
}

func (handlers *authHandlers) validOrigin(request *http.Request) bool {
	return originAllowed(request, handlers.options.AllowedOrigin)
}

func decodeCredentials(response http.ResponseWriter, request *http.Request) (credentialsRequest, error) {
	var credentials credentialsRequest
	if err := decodeJSONRequest(response, request, maximumAuthBody, &credentials); err != nil {
		return credentialsRequest{}, err
	}

	return credentials, nil
}

func decodeJSONRequest(response http.ResponseWriter, request *http.Request, maximumBytes int64, destination any) error {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return errors.New("content type must be application/json")
	}

	request.Body = http.MaxBytesReader(response, request.Body, maximumBytes)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return err
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON object")
	}

	return nil
}

func sessionToken(request *http.Request) (string, error) {
	cookie, err := request.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return "", auth.ErrUnauthorized
	}

	return cookie.Value, nil
}

func setSessionCookie(response http.ResponseWriter, result auth.Result, options Options) {
	maxAge := int(time.Until(result.ExpiresAt).Seconds())
	if maxAge < 1 {
		maxAge = 1
	}

	http.SetCookie(response, &http.Cookie{
		Name:     sessionCookieName,
		Value:    result.Token,
		Path:     cookiePath(options.BasePath),
		Expires:  result.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   options.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(response http.ResponseWriter, options Options) {
	http.SetCookie(response, &http.Cookie{
		Name:     sessionCookieName,
		Path:     cookiePath(options.BasePath),
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   options.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func cookiePath(basePath string) string {
	if basePath == "" {
		return "/"
	}

	return basePath
}

func methodNotAllowed(response http.ResponseWriter, allowedMethod string) {
	response.Header().Set("Allow", allowedMethod)
	writeError(response, http.StatusMethodNotAllowed, "method_not_allowed", "method is not allowed")
}

func rateLimited(response http.ResponseWriter) {
	response.Header().Set("Retry-After", "60")
	writeError(response, http.StatusTooManyRequests, "rate_limited", "too many requests; try again later")
}

func writeError(response http.ResponseWriter, statusCode int, code, message string) {
	payload := errorResponse{}
	payload.Error.Code = code
	payload.Error.Message = message
	writeJSON(response, statusCode, payload)
}

func writeJSON(response http.ResponseWriter, statusCode int, payload any) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(payload)
}
