package checknodes

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"
)

type stubStore struct {
	createdHash []byte
	createdNode Node
	recorded    []Result
	outcome     BatchOutcome
}

func (store *stubStore) Create(_ context.Context, _ string, node Node, hash []byte) (Node, error) {
	store.createdNode = node
	store.createdHash = append([]byte(nil), hash...)
	node.Assignments = []Assignment{}
	return node, nil
}
func (*stubStore) List(context.Context, string, time.Time) ([]Node, error) { return []Node{}, nil }
func (*stubStore) Revoke(context.Context, string, string, time.Time) error { return nil }
func (*stubStore) Assign(context.Context, string, string, Assignment, time.Time) (Assignment, error) {
	return Assignment{}, nil
}
func (*stubStore) RemoveAssignment(context.Context, string, string, string, time.Time) error {
	return nil
}
func (*stubStore) Authenticate(context.Context, []byte, time.Time) (Node, error) { return Node{}, nil }
func (*stubStore) ClaimAssignments(context.Context, string, time.Time, int) ([]Assignment, error) {
	return []Assignment{}, nil
}
func (store *stubStore) RecordResults(_ context.Context, _ Node, results []Result, _ time.Time) (BatchOutcome, error) {
	store.recorded = append([]Result(nil), results...)
	return store.outcome, nil
}

func TestCreateReturnsSecretOnceAndStoresHash(t *testing.T) {
	store := &stubStore{}
	service := NewService(store)
	service.now = func() time.Time { return time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC) }
	node, err := service.Create(context.Background(), "user-id", " Frankfurt 1 ", "EU-CENTRAL")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if node.Secret == "" || node.Region != "eu-central" || node.Name != "Frankfurt 1" {
		t.Fatalf("created node = %+v", node)
	}
	hash := sha256.Sum256([]byte(node.Secret))
	if string(store.createdHash) != string(hash[:]) {
		t.Fatal("stored secret hash does not match returned secret")
	}
	if string(store.createdHash) == node.Secret {
		t.Fatal("raw secret was passed to the store")
	}
}

func TestRecordResultsValidatesRegionTimestampAndDeduplicationKey(t *testing.T) {
	store := &stubStore{outcome: BatchOutcome{Accepted: 1}}
	service := NewService(store)
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	node := Node{ID: "4da6a741-c96d-4f3d-9283-0cb9ca54ae70", Region: "eu-central"}
	valid := Result{
		ResultID: "554f1140-21d8-4f83-aa18-e1243b3afce1", AssignmentID: "20628d61-787e-4ed9-8a02-785e90259e10",
		MonitorID: "a3443497-a012-40a3-a3a3-74e7171d43ec", Region: "EU-CENTRAL",
		StartedAt: now.Add(-time.Second), FinishedAt: now, Available: true,
	}
	outcome, err := service.RecordResults(context.Background(), node, []Result{valid})
	if err != nil || outcome.Accepted != 1 || len(store.recorded) != 1 {
		t.Fatalf("RecordResults() = %+v, %v", outcome, err)
	}
	if store.recorded[0].Region != "eu-central" {
		t.Fatalf("normalized region = %q", store.recorded[0].Region)
	}

	invalid := valid
	invalid.Region = "us-east"
	if outcome, err := service.RecordResults(context.Background(), node, []Result{invalid}); err != nil || len(outcome.Rejected) != 1 || outcome.Rejected[0].Code != "invalid_result" {
		t.Fatalf("wrong-region outcome = %+v, %v", outcome, err)
	}
	invalid = valid
	invalid.FinishedAt = now.Add(6 * time.Minute)
	if outcome, err := service.RecordResults(context.Background(), node, []Result{invalid}); err != nil || len(outcome.Rejected) != 1 {
		t.Fatalf("future timestamp outcome = %+v, %v", outcome, err)
	}
	if outcome, err := service.RecordResults(context.Background(), node, []Result{valid, valid}); err != nil || outcome.Accepted != 1 || outcome.Duplicate != 1 {
		t.Fatalf("duplicate batch outcome = %+v, %v", outcome, err)
	}
}

func TestRecordResultsRejectsEmptyBatch(t *testing.T) {
	service := NewService(&stubStore{})
	if _, err := service.RecordResults(context.Background(), Node{}, nil); !errors.Is(err, ErrInvalidResult) {
		t.Fatalf("RecordResults() error = %v", err)
	}
}

func TestCreateRejectsInvalidRegion(t *testing.T) {
	service := NewService(&stubStore{})
	if _, err := service.Create(context.Background(), "user", "Node", "Europe West"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("Create() error = %v", err)
	}
}
