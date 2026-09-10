package monitoring

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	notificationBatchSize       = 8
	notificationConcurrency     = 4
	notificationPollInterval    = time.Second
	notificationLease           = time.Minute
	notificationTimeout         = 20 * time.Second
	maximumNotificationAttempts = 8
)

type QueuedNotification struct {
	Transition
	Attempt int
}

type NotificationQueueStore interface {
	ClaimNotifications(context.Context, time.Time, int, time.Duration) ([]QueuedNotification, error)
	FinishNotification(context.Context, string, int, time.Time, time.Time, bool, string, time.Duration) error
}

type NotificationWorker struct {
	store    NotificationQueueStore
	notifier Notifier
	logger   *slog.Logger
	now      func() time.Time
}

func NewNotificationWorker(store NotificationQueueStore, notifier Notifier, logger *slog.Logger) *NotificationWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &NotificationWorker{store: store, notifier: notifier, logger: logger, now: time.Now}
}

func (worker *NotificationWorker) Run(ctx context.Context) {
	ticker := time.NewTicker(notificationPollInterval)
	defer ticker.Stop()
	for {
		worker.runBatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (worker *NotificationWorker) runBatch(ctx context.Context) {
	notifications, err := worker.store.ClaimNotifications(ctx, worker.now().UTC(), notificationBatchSize, notificationLease)
	if err != nil {
		if ctx.Err() == nil {
			worker.logger.Error("claim notifications", "error", err)
		}
		return
	}

	semaphore := make(chan struct{}, notificationConcurrency)
	var workers sync.WaitGroup
	for _, notification := range notifications {
		notification := notification
		workers.Add(1)
		go func() {
			defer workers.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				return
			}
			worker.deliver(ctx, notification)
		}()
	}
	workers.Wait()
}

func (worker *NotificationWorker) deliver(ctx context.Context, notification QueuedNotification) {
	startedAt := worker.now().UTC()
	deliveryContext, cancel := context.WithTimeout(ctx, notificationTimeout)
	defer cancel()

	errorCode := ""
	success := true
	if worker.notifier != nil {
		if err := worker.notifier.Notify(deliveryContext, notification.Transition); err != nil {
			success = false
			errorCode = notificationErrorCode(err, deliveryContext)
		}
	}
	finishedAt := worker.now().UTC()
	if err := worker.store.FinishNotification(
		ctx,
		notification.EventID,
		notification.Attempt,
		startedAt,
		finishedAt,
		success,
		errorCode,
		notificationRetryDelay(notification.Attempt),
	); err != nil && ctx.Err() == nil {
		worker.logger.Error("finish notification", "notification_id", notification.EventID, "error", err)
		return
	}
	if !success && ctx.Err() == nil {
		worker.logger.Warn("notification attempt failed", "notification_id", notification.EventID, "attempt", notification.Attempt, "error_code", errorCode, "duration", finishedAt.Sub(startedAt))
	}
}

func notificationRetryDelay(attempt int) time.Duration {
	delays := [...]time.Duration{5 * time.Second, 30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, time.Hour, 2 * time.Hour}
	if attempt < 1 {
		return delays[0]
	}
	if attempt > len(delays) {
		return delays[len(delays)-1]
	}
	return delays[attempt-1]
}

func notificationErrorCode(err error, ctx context.Context) string {
	if ctx.Err() != nil {
		return "delivery_timeout"
	}
	if err == nil {
		return ""
	}
	return "channel_delivery_failed"
}
