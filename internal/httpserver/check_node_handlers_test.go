package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/MrArdek/UptimeControl/internal/checknodes"
)

type stubCheckNodeService struct {
	node        checknodes.Node
	assignments []checknodes.Assignment
	outcome     checknodes.BatchOutcome
}

func (stub stubCheckNodeService) Create(context.Context, string, string, string) (checknodes.Node, error) {
	return stub.node, nil
}

func (stub stubCheckNodeService) List(context.Context, string) ([]checknodes.Node, error) {
	return []checknodes.Node{stub.node}, nil
}

func (stub stubCheckNodeService) Revoke(context.Context, string, string) error { return nil }

func (stub stubCheckNodeService) Assign(context.Context, string, string, string) (checknodes.Assignment, error) {
	return checknodes.Assignment{}, nil
}

func (stub stubCheckNodeService) RemoveAssignment(context.Context, string, string, string) error {
	return nil
}

func (stub stubCheckNodeService) Authenticate(_ context.Context, secret string) (checknodes.Node, error) {
	if secret != "valid-node-secret" {
		return checknodes.Node{}, checknodes.ErrUnauthorized
	}
	return stub.node, nil
}

func (stub stubCheckNodeService) ClaimAssignments(context.Context, checknodes.Node, int) ([]checknodes.Assignment, error) {
	return stub.assignments, nil
}

func (stub stubCheckNodeService) RecordResults(context.Context, checknodes.Node, []checknodes.Result) (checknodes.BatchOutcome, error) {
	return stub.outcome, nil
}

func TestCheckNodeAssignmentsRequireBearerCredential(t *testing.T) {
	handlers := newCheckNodeHandlers(stubAuthService{}, stubCheckNodeService{}, Options{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/monitoring/assignments", nil)
	response := httptest.NewRecorder()

	handlers.assignments(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if response.Header().Get("WWW-Authenticate") != "Bearer" {
		t.Fatal("missing Bearer authentication challenge")
	}
}

func TestCheckNodeAssignmentsReturnOnlyServiceAssignments(t *testing.T) {
	stub := stubCheckNodeService{
		node: checknodes.Node{ID: "00000000-0000-4000-8000-000000000001", Region: "eu-central"},
		assignments: []checknodes.Assignment{{
			ID:        "00000000-0000-4000-8000-000000000002",
			MonitorID: "00000000-0000-4000-8000-000000000003",
			Region:    "eu-central",
		}},
	}
	handlers := newCheckNodeHandlers(stubAuthService{}, stub, Options{})
	request := httptest.NewRequest(http.MethodGet, "/api/v1/monitoring/assignments?limit=25", nil)
	request.Header.Set("Authorization", "Bearer valid-node-secret")
	response := httptest.NewRecorder()

	handlers.assignments(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); !strings.Contains(body, `"region":"eu-central"`) || !strings.Contains(body, `"monitor_id":"00000000-0000-4000-8000-000000000003"`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}

func TestCheckNodeResultBatchReturnsIdempotencyCounts(t *testing.T) {
	serverTime := time.Date(2026, time.September, 23, 12, 0, 0, 0, time.UTC)
	stub := stubCheckNodeService{
		node:    checknodes.Node{ID: "00000000-0000-4000-8000-000000000001", Region: "eu-central"},
		outcome: checknodes.BatchOutcome{Accepted: 1, Duplicate: 1, Rejected: []checknodes.Rejection{}, ServerTime: serverTime},
	}
	handlers := newCheckNodeHandlers(stubAuthService{}, stub, Options{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/monitoring/results", strings.NewReader(`{"results":[]}`))
	request.Header.Set("Authorization", "Bearer valid-node-secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handlers.results(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if body := response.Body.String(); !strings.Contains(body, `"accepted":1`) || !strings.Contains(body, `"duplicate":1`) || !strings.Contains(body, `"rejected":[]`) {
		t.Fatalf("unexpected response body: %s", body)
	}
}
