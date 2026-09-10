package monitoring

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"
)

type queueTestStore struct {
	notifications []QueuedNotification
	finishedID    string
	finishedTry   int
	finishedOK    bool
	errorCode     string
	retryDelay    time.Duration
}

func (store *queueTestStore) ClaimNotifications(context.Context, time.Time, int, time.Duration) ([]QueuedNotification, error) {
	return store.notifications, nil
}

func (store *queueTestStore) FinishNotification(
	_ context.Context,
	id string,
	attempt int,
	_, _ time.Time,
	success bool,
	errorCode string,
	retryDelay time.Duration,
) error {
	store.finishedID = id
	store.finishedTry = attempt
	store.finishedOK = success
	store.errorCode = errorCode
	store.retryDelay = retryDelay
	return nil
}

func TestNotificationWorkerCompletesClaimedEvent(t *testing.T) {
	store := &queueTestStore{notifications: []QueuedNotification{{
		Transition: Transition{EventID: "event-id", Kind: "down"}, Attempt: 1,
	}}}
	notifier := &stubNotifier{}
	worker := NewNotificationWorker(store, notifier, slog.Default())
	worker.runBatch(context.Background())
	if notifier.calls != 1 || store.finishedID != "event-id" || store.finishedTry != 1 || !store.finishedOK {
		t.Fatalf("delivery = calls %d, store %#v", notifier.calls, store)
	}
}

func TestNotificationWorkerSchedulesSafeRetry(t *testing.T) {
	store := &queueTestStore{notifications: []QueuedNotification{{
		Transition: Transition{EventID: "event-id", Kind: "down"}, Attempt: 3,
	}}}
	worker := NewNotificationWorker(store, &stubNotifier{err: errors.New("secret destination details")}, slog.Default())
	worker.runBatch(context.Background())
	if store.finishedOK || store.errorCode != "channel_delivery_failed" || store.retryDelay != 2*time.Minute {
		t.Fatalf("failure outcome = %#v", store)
	}
}

func TestNotificationRetryDelayIsBounded(t *testing.T) {
	if notificationRetryDelay(1) != 5*time.Second || notificationRetryDelay(100) != 2*time.Hour {
		t.Fatal("notification retry delay is outside the configured bounds")
	}
}
