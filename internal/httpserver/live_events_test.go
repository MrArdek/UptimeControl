package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLiveEventsSendCurrentSnapshot(t *testing.T) {
	source := newLiveEventSource(stubAuthService{}, &configurableProjectService{}, stubHistoryService{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil).WithContext(ctx)
	addSessionCookie(request)
	response := httptest.NewRecorder()

	source.serve(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if response.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
	body := response.Body.String()
	if !strings.Contains(body, "event: snapshot") || !strings.Contains(body, `"projects":[]`) || !strings.Contains(body, `"incidents":[]`) {
		t.Fatalf("snapshot body = %s", body)
	}
}

func TestLiveEventsRequireSession(t *testing.T) {
	source := newLiveEventSource(stubAuthService{}, &configurableProjectService{}, stubHistoryService{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	response := httptest.NewRecorder()

	source.serve(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestLiveEventsLimitConnections(t *testing.T) {
	source := newLiveEventSource(stubAuthService{}, &configurableProjectService{}, stubHistoryService{})
	for index := 0; index < maximumLiveConnections; index++ {
		source.connections <- struct{}{}
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/events", nil)
	addSessionCookie(request)
	response := httptest.NewRecorder()

	source.serve(response, request)

	if response.Code != http.StatusTooManyRequests || !strings.Contains(response.Body.String(), "stream_limit") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}
