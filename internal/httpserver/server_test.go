package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/sites"
)

type stubDatabase struct {
	err error
}

func (database stubDatabase) Ping(context.Context) error {
	return database.err
}

type stubAuthService struct{}

func (stubAuthService) Register(context.Context, string, string) (auth.Result, error) {
	return auth.Result{}, nil
}

func (stubAuthService) Login(context.Context, string, string) (auth.Result, error) {
	return auth.Result{}, nil
}

func (stubAuthService) CurrentUser(context.Context, string) (auth.User, error) {
	return auth.User{CreatedAt: time.Now()}, nil
}

func (stubAuthService) Logout(context.Context, string) error {
	return nil
}

type stubSiteService struct{}

func (stubSiteService) List(context.Context, string) ([]sites.Site, error) {
	return []sites.Site{}, nil
}

func (stubSiteService) Create(context.Context, string, sites.CreateInput) (sites.Site, error) {
	return sites.Site{}, nil
}

func (stubSiteService) ByID(context.Context, string, string) (sites.Site, error) {
	return sites.Site{}, nil
}

func (stubSiteService) Update(context.Context, string, string, sites.UpdateInput) (sites.Site, error) {
	return sites.Site{}, nil
}

func (stubSiteService) Delete(context.Context, string, string) error {
	return nil
}

func testHandler(database stubDatabase) http.Handler {
	return newHandler(database, stubAuthService{}, stubSiteService{}, Options{})
}

func TestHealthHandler(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	response := httptest.NewRecorder()

	testHandler(stubDatabase{}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}

	if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q, want JSON", contentType)
	}

	var body struct {
		Status string `json:"status"`
	}

	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if body.Status != "ok" {
		t.Fatalf("status body = %q, want %q", body.Status, "ok")
	}
}

func TestHealthHandlerRejectsOtherMethods(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/health", nil)
	response := httptest.NewRecorder()

	testHandler(stubDatabase{}).ServeHTTP(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}

	if allow := response.Header().Get("Allow"); allow != http.MethodGet {
		t.Fatalf("Allow = %q, want %q", allow, http.MethodGet)
	}
}

func TestReadyHandler(t *testing.T) {
	tests := []struct {
		name       string
		database   stubDatabase
		wantStatus int
		wantBody   string
	}{
		{
			name:       "database available",
			database:   stubDatabase{},
			wantStatus: http.StatusOK,
			wantBody:   "ready",
		},
		{
			name:       "database unavailable",
			database:   stubDatabase{err: errors.New("database unavailable")},
			wantStatus: http.StatusServiceUnavailable,
			wantBody:   "unavailable",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/ready", nil)
			response := httptest.NewRecorder()

			testHandler(test.database).ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", response.Code, test.wantStatus)
			}

			var body struct {
				Status string `json:"status"`
			}

			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatalf("decode response: %v", err)
			}

			if body.Status != test.wantBody {
				t.Fatalf("status body = %q, want %q", body.Status, test.wantBody)
			}
		})
	}
}
