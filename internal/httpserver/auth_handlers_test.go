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
)

type configurableAuthService struct {
	registerResult auth.Result
	registerError  error
	loginResult    auth.Result
	loginError     error
	currentUser    auth.User
	currentError   error
	logoutError    error
	logoutToken    string
}

func (service *configurableAuthService) Register(context.Context, string, string) (auth.Result, error) {
	return service.registerResult, service.registerError
}

func (service *configurableAuthService) Login(context.Context, string, string) (auth.Result, error) {
	return service.loginResult, service.loginError
}

func (service *configurableAuthService) CurrentUser(context.Context, string) (auth.User, error) {
	return service.currentUser, service.currentError
}

func (service *configurableAuthService) Logout(_ context.Context, token string) error {
	service.logoutToken = token
	return service.logoutError
}

func TestRegisterHandlerSetsSecureSessionCookie(t *testing.T) {
	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	service := &configurableAuthService{registerResult: auth.Result{
		User:      auth.User{ID: "user-id", Email: "user@example.com", CreatedAt: time.Now()},
		Token:     "raw-session-token",
		ExpiresAt: expiresAt,
	}}
	handlers := newAuthHandlers(service, Options{
		AllowedOrigin: "https://app.example.com",
		BasePath:      "/uptimec",
		CookieSecure:  true,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		strings.NewReader(`{"email":"user@example.com","password":"correct horse battery staple"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://app.example.com")
	response := httptest.NewRecorder()

	handlers.register(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusCreated, response.Body.String())
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d, want 1", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != sessionCookieName || cookie.Value != "raw-session-token" {
		t.Fatalf("session cookie = %s:%s, want expected token", cookie.Name, cookie.Value)
	}
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie flags are unsafe: %#v", cookie)
	}
	if cookie.Path != "/uptimec" {
		t.Fatalf("session cookie path = %q, want %q", cookie.Path, "/uptimec")
	}
}

func TestRegisterHandlerRejectsInvalidOrigin(t *testing.T) {
	handlers := newAuthHandlers(&configurableAuthService{}, Options{AllowedOrigin: "https://app.example.com"})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		strings.NewReader(`{"email":"user@example.com","password":"correct horse battery staple"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()

	handlers.register(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestLoginHandlerDoesNotRevealCredentialPart(t *testing.T) {
	handlers := newAuthHandlers(
		&configurableAuthService{loginError: auth.ErrInvalidCredentials},
		Options{AllowedOrigin: "http://localhost:8080"},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"email":"missing@example.com","password":"incorrect password value"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handlers.login(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	body := response.Body.String()
	if strings.Contains(body, "missing@example.com") || strings.Contains(body, "not registered") {
		t.Fatalf("response reveals account existence: %s", body)
	}
}

func TestMeHandlerRequiresValidSession(t *testing.T) {
	handlers := newAuthHandlers(
		&configurableAuthService{currentError: auth.ErrUnauthorized},
		Options{CookieSecure: true},
	)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "expired-token"})
	response := httptest.NewRecorder()

	handlers.me(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("expired session cookie was not cleared: %#v", cookies)
	}
}

func TestLogoutHandlerRevokesSessionAndClearsCookie(t *testing.T) {
	service := &configurableAuthService{}
	handlers := newAuthHandlers(service, Options{AllowedOrigin: "http://localhost:8080"})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "session-token"})
	request.Header.Set("Origin", "http://localhost:8080")
	response := httptest.NewRecorder()

	handlers.logout(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNoContent)
	}
	if service.logoutToken != "session-token" {
		t.Fatalf("revoked token = %q, want session token", service.logoutToken)
	}

	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge >= 0 {
		t.Fatalf("session cookie was not cleared: %#v", cookies)
	}
}

func TestRegisterHandlerMapsDuplicateEmail(t *testing.T) {
	handlers := newAuthHandlers(
		&configurableAuthService{registerError: auth.ErrEmailTaken},
		Options{AllowedOrigin: "http://localhost:8080"},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		strings.NewReader(`{"email":"user@example.com","password":"correct horse battery staple"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handlers.register(response, request)

	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusConflict)
	}
}

func TestRegisterHandlerMapsClosedRegistration(t *testing.T) {
	handlers := newAuthHandlers(
		&configurableAuthService{registerError: auth.ErrRegistrationClosed},
		Options{AllowedOrigin: "http://localhost:8080"},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/register",
		strings.NewReader(`{"email":"second@example.com","password":"correct horse battery staple"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handlers.register(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if !strings.Contains(response.Body.String(), "registration_closed") {
		t.Fatalf("response does not explain closed registration: %s", response.Body.String())
	}
}

func TestLoginHandlerMapsInternalErrors(t *testing.T) {
	handlers := newAuthHandlers(
		&configurableAuthService{loginError: errors.New("database error")},
		Options{},
	)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/auth/login",
		strings.NewReader(`{"email":"user@example.com","password":"correct horse battery staple"}`),
	)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handlers.login(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "database") {
		t.Fatalf("internal error leaked to client: %s", response.Body.String())
	}
}
