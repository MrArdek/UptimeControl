package monitoring

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

const (
	claimBatchSize = 10
	pollInterval   = time.Second
)

type Scheduler struct {
	store    Store
	checker  Checker
	notifier Notifier
	logger   *slog.Logger
	now      func() time.Time
}

func NewScheduler(store Store, checker Checker, notifier Notifier, logger *slog.Logger) *Scheduler {
	return &Scheduler{
		store:    store,
		checker:  checker,
		notifier: notifier,
		logger:   logger,
		now:      time.Now,
	}
}

func (scheduler *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		scheduler.runBatch(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (scheduler *Scheduler) runBatch(ctx context.Context) {
	monitors, err := scheduler.store.ClaimDue(ctx, scheduler.now().UTC(), claimBatchSize)
	if err != nil {
		if ctx.Err() == nil {
			scheduler.logger.Error("claim due monitors", "error", err)
		}
		return
	}

	var workers sync.WaitGroup
	for _, monitor := range monitors {
		monitor := monitor
		workers.Add(1)
		go func() {
			defer workers.Done()
			result := Result{CheckedAt: scheduler.now().UTC()}
			if monitor.Type == "heartbeat" {
				message := "heartbeat overdue"
				result.Error = &message
			} else {
				result = scheduler.checker.Check(ctx, monitor)
			}
			transition, err := scheduler.store.RecordResult(ctx, monitor, result)
			if err != nil {
				if ctx.Err() == nil {
					scheduler.logger.Error("record monitor result", "monitor_id", monitor.ID, "error", err)
				}
				return
			}
			scheduler.notify(ctx, transition)
		}()
	}
	workers.Wait()
}

func (scheduler *Scheduler) RecordHeartbeat(ctx context.Context, token string) error {
	if token == "" || len(token) > 128 {
		return fmt.Errorf("invalid heartbeat token")
	}
	tokenHash := sha256.Sum256([]byte(token))
	transition, err := scheduler.store.RecordHeartbeat(ctx, tokenHash[:], scheduler.now().UTC())
	if err != nil {
		return err
	}
	scheduler.notify(ctx, transition)
	return nil
}

func (scheduler *Scheduler) notify(ctx context.Context, transition Transition) {
	if transition.Empty() || scheduler.notifier == nil {
		return
	}
	if err := scheduler.notifier.Notify(ctx, transition); err != nil && ctx.Err() == nil {
		scheduler.logger.Error("send monitor notification", "kind", transition.Kind, "error", err)
	}
}
