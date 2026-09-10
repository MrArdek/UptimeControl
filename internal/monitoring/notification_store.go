package monitoring

import (
	"context"
	"fmt"
	"time"

	"github.com/MrArdek/UptimeControl/internal/identity"
)

func enqueueTransition(ctx context.Context, executor resultExecutor, transition *Transition) error {
	if transition == nil || transition.Empty() {
		return nil
	}
	eventID, err := identity.NewUUID()
	if err != nil {
		return fmt.Errorf("generate notification ID: %w", err)
	}
	if _, err := executor.Exec(ctx, `
		INSERT INTO notification_events (
			id, project_id, monitor_id, kind, project_name, monitor_name,
			url, target, cause, occurred_at, available_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10)
	`, eventID, transition.ProjectID, transition.MonitorID, transition.Kind,
		transition.ProjectName, transition.MonitorName, transition.URL,
		transition.Target, transition.Cause, transition.OccurredAt); err != nil {
		return fmt.Errorf("enqueue notification: %w", err)
	}
	transition.EventID = eventID
	return nil
}

func (store *PostgresStore) ClaimNotifications(
	ctx context.Context,
	now time.Time,
	limit int,
	lease time.Duration,
) ([]QueuedNotification, error) {
	if limit < 1 {
		return []QueuedNotification{}, nil
	}
	rows, err := store.database.Query(ctx, `
		WITH ready AS (
			SELECT id
			FROM notification_events
			WHERE attempts < $3
			  AND (
				(status = 'pending' AND available_at <= $1)
				OR (status = 'processing' AND locked_at <= $1 - make_interval(secs => $4))
			  )
			ORDER BY available_at, created_at, id
			FOR UPDATE SKIP LOCKED
			LIMIT $2
		)
		UPDATE notification_events
		SET status = 'processing', locked_at = $1, attempts = attempts + 1, updated_at = $1
		FROM ready
		WHERE notification_events.id = ready.id
		RETURNING notification_events.id, notification_events.kind,
		          notification_events.project_id, notification_events.project_name,
		          notification_events.monitor_id, notification_events.monitor_name,
		          notification_events.url, notification_events.target,
		          notification_events.cause, notification_events.occurred_at,
		          notification_events.attempts
	`, now, limit, maximumNotificationAttempts, int64(lease/time.Second))
	if err != nil {
		return nil, fmt.Errorf("claim notification queue: %w", err)
	}
	defer rows.Close()

	notifications := make([]QueuedNotification, 0, limit)
	for rows.Next() {
		var notification QueuedNotification
		if err := rows.Scan(
			&notification.EventID,
			&notification.Kind,
			&notification.ProjectID,
			&notification.ProjectName,
			&notification.MonitorID,
			&notification.MonitorName,
			&notification.URL,
			&notification.Target,
			&notification.Cause,
			&notification.OccurredAt,
			&notification.Attempt,
		); err != nil {
			return nil, fmt.Errorf("scan queued notification: %w", err)
		}
		notification.OccurredAt = notification.OccurredAt.UTC()
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification queue: %w", err)
	}
	return notifications, nil
}

func (store *PostgresStore) FinishNotification(
	ctx context.Context,
	notificationID string,
	attempt int,
	startedAt time.Time,
	completedAt time.Time,
	success bool,
	errorCode string,
	retryDelay time.Duration,
) error {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin notification completion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	status := "delivered"
	availableAt := completedAt
	var deliveredAt any = completedAt
	var storedError any
	if !success {
		status = "pending"
		availableAt = completedAt.Add(retryDelay)
		deliveredAt = nil
		storedError = errorCode
		if attempt >= maximumNotificationAttempts {
			status = "failed"
		}
	}
	commandTag, err := transaction.Exec(ctx, `
		UPDATE notification_events
		SET status = $3, available_at = $4, locked_at = NULL,
		    delivered_at = $5, last_error = $6, updated_at = $2
		WHERE id = $1 AND status = 'processing' AND attempts = $7
	`, notificationID, completedAt, status, availableAt, deliveredAt, storedError, attempt)
	if err != nil {
		return fmt.Errorf("update notification outcome: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return nil
	}
	if _, err := transaction.Exec(ctx, `
		INSERT INTO notification_attempts (
			notification_id, attempt, success, error_code, started_at, completed_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`, notificationID, attempt, success, nullableErrorCode(errorCode), startedAt, completedAt); err != nil {
		return fmt.Errorf("record notification attempt: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit notification completion: %w", err)
	}
	return nil
}

func nullableErrorCode(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (store *PostgresStore) RetryFailedNotification(ctx context.Context, userID, projectID, notificationID string, now time.Time) error {
	commandTag, err := store.database.Exec(ctx, `
		UPDATE notification_events
		SET status = 'pending', attempts = 0, available_at = $4,
		    locked_at = NULL, delivered_at = NULL, last_error = NULL, updated_at = $4
		WHERE id = $1 AND project_id = $2 AND status = 'failed'
		  AND EXISTS (
			SELECT 1 FROM projects
			WHERE projects.id = $2 AND projects.user_id = $3 AND projects.deleted_at IS NULL
		  )
	`, notificationID, projectID, userID, now)
	if err != nil {
		return fmt.Errorf("retry notification: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
