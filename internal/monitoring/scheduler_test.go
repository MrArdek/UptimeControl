package monitoring

import (
	"context"
	"crypto/sha256"
	"log/slog"
	"testing"
	"time"
)

type schedulerStore struct {
	due                []DueMonitor
	recordedMonitor    DueMonitor
	recordedResult     Result
	heartbeatTokenHash []byte
}

func (store *schedulerStore) ClaimDue(context.Context, time.Time, int) ([]DueMonitor, error) {
	return store.due, nil
}
func (store *schedulerStore) RecordResult(_ context.Context, monitor DueMonitor, result Result) (Transition, error) {
	store.recordedMonitor = monitor
	store.recordedResult = result
	return Transition{}, nil
}
func (store *schedulerStore) RecordHeartbeat(_ context.Context, tokenHash []byte, _ time.Time) (Transition, error) {
	store.heartbeatTokenHash = append([]byte(nil), tokenHash...)
	return Transition{}, nil
}
func (store *schedulerStore) Checks(context.Context, string, string, string, int) ([]Check, error) {
	return nil, nil
}
func (store *schedulerStore) Incidents(context.Context, string, string, string, int) ([]Incident, error) {
	return nil, nil
}

type schedulerChecker struct{}

func (schedulerChecker) Check(context.Context, DueMonitor) Result {
	return Result{Available: true, CheckedAt: time.Now()}
}

func TestSchedulerMarksOverdueHeartbeatUnavailable(t *testing.T) {
	store := &schedulerStore{due: []DueMonitor{{ID: "monitor-id", Type: "heartbeat"}}}
	scheduler := NewScheduler(store, schedulerChecker{}, nil, slog.Default())
	scheduler.runBatch(context.Background())
	if store.recordedMonitor.ID != "monitor-id" || store.recordedResult.Available {
		t.Fatalf("overdue heartbeat result = %#v", store.recordedResult)
	}
	if store.recordedResult.Error == nil || *store.recordedResult.Error != "heartbeat overdue" {
		t.Fatalf("overdue heartbeat error = %#v", store.recordedResult.Error)
	}
}

func TestRecordHeartbeatHashesToken(t *testing.T) {
	store := &schedulerStore{}
	scheduler := NewScheduler(store, schedulerChecker{}, nil, slog.Default())
	if err := scheduler.RecordHeartbeat(context.Background(), "raw-secret-token"); err != nil {
		t.Fatalf("RecordHeartbeat() returned an error: %v", err)
	}
	want := sha256.Sum256([]byte("raw-secret-token"))
	if string(store.heartbeatTokenHash) != string(want[:]) {
		t.Fatal("heartbeat token was not hashed before storage lookup")
	}
}
