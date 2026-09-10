package monitoring

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type transitionExecutor struct {
	statements []string
	results    []pgconn.CommandTag
}

func (executor *transitionExecutor) Exec(_ context.Context, statement string, _ ...any) (pgconn.CommandTag, error) {
	executor.statements = append(executor.statements, statement)
	result := pgconn.NewCommandTag("")
	if len(executor.results) > 0 {
		result = executor.results[0]
		executor.results = executor.results[1:]
	}
	return result, nil
}

func TestRecordResultEnqueuesTransitionWithIncident(t *testing.T) {
	executor := &transitionExecutor{results: []pgconn.CommandTag{
		pgconn.NewCommandTag("UPDATE 1"),
		pgconn.NewCommandTag("INSERT 0 1"),
		pgconn.NewCommandTag("INSERT 0 1"),
		pgconn.NewCommandTag("INSERT 0 1"),
	}}
	errorMessage := "connection refused"
	transition, err := recordResult(context.Background(), executor, DueMonitor{
		ID: "22222222-2222-4222-8222-222222222222", ProjectID: "11111111-1111-4111-8111-111111111111",
		ProjectName: "API", Name: "Health", URL: "https://example.com/health",
	}, Result{CheckedAt: time.Now().UTC(), Available: false, Error: &errorMessage})
	if err != nil {
		t.Fatalf("recordResult(): %v", err)
	}
	if transition.Kind != "down" || transition.EventID == "" {
		t.Fatalf("transition = %#v", transition)
	}
	if len(executor.statements) != 4 || !strings.Contains(executor.statements[2], "INSERT INTO incidents") || !strings.Contains(executor.statements[3], "INSERT INTO notification_events") {
		t.Fatalf("transaction statements = %#v", executor.statements)
	}
}

func TestRecordResultDoesNotEnqueueDuplicateDown(t *testing.T) {
	executor := &transitionExecutor{results: []pgconn.CommandTag{
		pgconn.NewCommandTag("UPDATE 1"),
		pgconn.NewCommandTag("INSERT 0 1"),
		pgconn.NewCommandTag("INSERT 0 0"),
	}}
	transition, err := recordResult(context.Background(), executor, DueMonitor{
		ID: "22222222-2222-4222-8222-222222222222", ProjectID: "11111111-1111-4111-8111-111111111111",
	}, Result{CheckedAt: time.Now().UTC(), Available: false})
	if err != nil {
		t.Fatalf("recordResult(): %v", err)
	}
	if !transition.Empty() || len(executor.statements) != 3 {
		t.Fatalf("duplicate transition = %#v, statements = %d", transition, len(executor.statements))
	}
}
