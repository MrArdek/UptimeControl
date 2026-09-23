package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestRoutesCanBeMountedUnderBasePath(t *testing.T) {
	handler := newHandler(
		stubDatabase{},
		stubAuthService{},
		stubSiteService{},
		Options{BasePath: "/uptimec"},
	)

	prefixedRequest := httptest.NewRequest(http.MethodGet, "/uptimec/health", nil)
	prefixedResponse := httptest.NewRecorder()
	handler.ServeHTTP(prefixedResponse, prefixedRequest)
	if prefixedResponse.Code != http.StatusOK {
		t.Fatalf("prefixed status = %d, want %d", prefixedResponse.Code, http.StatusOK)
	}

	rootRequest := httptest.NewRequest(http.MethodGet, "/health", nil)
	rootResponse := httptest.NewRecorder()
	handler.ServeHTTP(rootResponse, rootRequest)
	if rootResponse.Code != http.StatusNotFound {
		t.Fatalf("root status = %d, want %d", rootResponse.Code, http.StatusNotFound)
	}
}

func TestDashboardIsServedUnderBasePath(t *testing.T) {
	handler := newHandler(stubDatabase{}, stubAuthService{}, stubSiteService{}, Options{BasePath: "/uptimec"})
	request := httptest.NewRequest(http.MethodGet, "/uptimec/", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("dashboard response has no Content-Security-Policy")
	}
	body := response.Body.String()
	if !strings.Contains(body, `content="/uptimec"`) || !strings.Contains(body, `href="/uptimec/"`) {
		t.Fatalf("dashboard does not contain runtime base path: %s", body)
	}
	if strings.Contains(response.Header().Get("Content-Security-Policy"), "unsafe-inline") {
		t.Fatal("dashboard CSP permits inline scripts or styles")
	}
}

func TestDashboardSupportsNestedRoutesAndAssets(t *testing.T) {
	handler := newHandler(stubDatabase{}, stubAuthService{}, stubSiteService{}, Options{BasePath: "/uptimec"})

	nested := httptest.NewRecorder()
	handler.ServeHTTP(nested, httptest.NewRequest(http.MethodGet, "/uptimec/projects/00000000-0000-4000-8000-000000000001/monitors/00000000-0000-4000-8000-000000000002", nil))
	if nested.Code != http.StatusOK || !strings.Contains(nested.Body.String(), `<div id="root"></div>`) {
		t.Fatalf("nested route status = %d, body = %q", nested.Code, nested.Body.String())
	}

	index := nested.Body.String()
	assetStart := strings.Index(index, `src="./assets/`)
	if assetStart < 0 {
		t.Fatal("dashboard bundle asset was not found")
	}
	assetStart += len(`src=".`)
	assetEnd := strings.Index(index[assetStart:], `"`)
	assetPath := "/uptimec" + index[assetStart:assetStart+assetEnd]
	asset := httptest.NewRecorder()
	handler.ServeHTTP(asset, httptest.NewRequest(http.MethodGet, assetPath, nil))
	if asset.Code != http.StatusOK || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset status = %d, cache = %q", asset.Code, asset.Header().Get("Cache-Control"))
	}
	assetBody, err := io.ReadAll(asset.Body)
	if err != nil || len(assetBody) == 0 {
		t.Fatal("asset body is empty")
	}

	missingAPI := httptest.NewRecorder()
	handler.ServeHTTP(missingAPI, httptest.NewRequest(http.MethodGet, "/uptimec/api/v1/missing", nil))
	if missingAPI.Code != http.StatusNotFound {
		t.Fatalf("missing API status = %d, want 404", missingAPI.Code)
	}
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
