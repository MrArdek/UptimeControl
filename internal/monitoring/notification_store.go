package monitoring

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MrArdek/UptimeControl/internal/identity"
	"github.com/MrArdek/UptimeControl/internal/pagination"
	"github.com/jackc/pgx/v5"
)

type NotificationAttempt struct {
	Attempt     int       `json:"attempt"`
	Success     bool      `json:"success"`
	ErrorCode   *string   `json:"error_code"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
}

type NotificationEvent struct {
	ID          string                `json:"id"`
	ProjectID   string                `json:"project_id"`
	MonitorID   string                `json:"monitor_id"`
	Kind        string                `json:"kind"`
	MonitorName string                `json:"monitor_name"`
	Status      string                `json:"status"`
	Attempts    int                   `json:"attempts"`
	LastError   *string               `json:"last_error"`
	OccurredAt  time.Time             `json:"occurred_at"`
	AvailableAt time.Time             `json:"available_at"`
	DeliveredAt *time.Time            `json:"delivered_at"`
	CreatedAt   time.Time             `json:"created_at"`
	History     []NotificationAttempt `json:"attempt_history"`
}

type NotificationPage struct {
	Notifications []NotificationEvent `json:"notifications"`
	NextCursor    *string             `json:"next_cursor"`
}

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
	if !identity.ValidUUID(projectID) || !identity.ValidUUID(notificationID) {
		return ErrNotFound
	}
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

func (store *PostgresStore) EnqueueTestNotification(
	ctx context.Context,
	userID,
	projectID string,
	now time.Time,
) (NotificationEvent, error) {
	if !identity.ValidUUID(projectID) {
		return NotificationEvent{}, ErrNotFound
	}
	var transition Transition
	err := store.database.QueryRow(ctx, `
		SELECT projects.id, projects.name, monitors.id, monitors.name, monitors.url, monitors.target
		FROM projects
		JOIN monitors ON monitors.project_id = projects.id
		WHERE projects.id = $1 AND projects.user_id = $2
		  AND projects.deleted_at IS NULL AND monitors.deleted_at IS NULL
		ORDER BY monitors.created_at, monitors.id
		LIMIT 1
	`, projectID, userID).Scan(
		&transition.ProjectID, &transition.ProjectName, &transition.MonitorID,
		&transition.MonitorName, &transition.URL, &transition.Target,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationEvent{}, ErrNotFound
	}
	if err != nil {
		return NotificationEvent{}, fmt.Errorf("select test notification target: %w", err)
	}
	transition.Kind = "test"
	transition.OccurredAt = now.UTC()
	if err := enqueueTransition(ctx, store.database, &transition); err != nil {
		return NotificationEvent{}, err
	}
	return NotificationEvent{
		ID: transition.EventID, ProjectID: transition.ProjectID, MonitorID: transition.MonitorID,
		Kind: transition.Kind, MonitorName: transition.MonitorName, Status: "pending",
		OccurredAt: transition.OccurredAt, AvailableAt: transition.OccurredAt,
		CreatedAt: transition.OccurredAt, History: []NotificationAttempt{},
	}, nil
}

func (store *PostgresStore) ListNotifications(
	ctx context.Context,
	userID,
	projectID string,
	limit int,
	cursor string,
) (NotificationPage, error) {
	limit = normalizeLimit(limit)
	arguments := []any{userID, projectID}
	statement := `
		SELECT notification_events.id, notification_events.project_id,
		       notification_events.monitor_id, notification_events.kind,
		       notification_events.monitor_name, notification_events.status,
		       notification_events.attempts, notification_events.last_error,
		       notification_events.occurred_at, notification_events.available_at,
		       notification_events.delivered_at, notification_events.created_at
		FROM notification_events
		JOIN projects ON projects.id = notification_events.project_id
		WHERE projects.user_id = $1 AND projects.id = $2 AND projects.deleted_at IS NULL`
	scope := "notifications:" + userID + ":" + projectID
	if cursor != "" {
		beforeTime, beforeID, err := pagination.Decode(cursor, scope)
		if err != nil || !identity.ValidUUID(beforeID) {
			return NotificationPage{}, ErrInvalidCursor
		}
		statement += ` AND (notification_events.created_at, notification_events.id) < ($3, $4::uuid)`
		arguments = append(arguments, beforeTime, beforeID)
	}
	statement += ` ORDER BY notification_events.created_at DESC, notification_events.id DESC LIMIT $` + fmt.Sprint(len(arguments)+1)
	arguments = append(arguments, limit+1)
	rows, err := store.database.Query(ctx, statement, arguments...)
	if err != nil {
		return NotificationPage{}, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()
	events := make([]NotificationEvent, 0, limit+1)
	for rows.Next() {
		var event NotificationEvent
		var deliveredAt *time.Time
		if err := rows.Scan(
			&event.ID, &event.ProjectID, &event.MonitorID, &event.Kind,
			&event.MonitorName, &event.Status, &event.Attempts, &event.LastError,
			&event.OccurredAt, &event.AvailableAt, &deliveredAt, &event.CreatedAt,
		); err != nil {
			return NotificationPage{}, fmt.Errorf("scan notification: %w", err)
		}
		event.OccurredAt = event.OccurredAt.UTC()
		event.AvailableAt = event.AvailableAt.UTC()
		event.CreatedAt = event.CreatedAt.UTC()
		if deliveredAt != nil {
			value := deliveredAt.UTC()
			event.DeliveredAt = &value
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return NotificationPage{}, fmt.Errorf("iterate notifications: %w", err)
	}
	page := NotificationPage{Notifications: events}
	if len(events) > limit {
		page.Notifications = events[:limit]
		last := page.Notifications[len(page.Notifications)-1]
		next, err := pagination.Encode(last.CreatedAt, last.ID, scope)
		if err != nil {
			return NotificationPage{}, fmt.Errorf("encode notification cursor: %w", err)
		}
		page.NextCursor = &next
	}
	for index := range page.Notifications {
		history, err := store.notificationAttempts(ctx, page.Notifications[index].ID)
		if err != nil {
			return NotificationPage{}, err
		}
		page.Notifications[index].History = history
	}
	return page, nil
}

func (store *PostgresStore) notificationAttempts(ctx context.Context, notificationID string) ([]NotificationAttempt, error) {
	rows, err := store.database.Query(ctx, `
		SELECT attempt, success, error_code, started_at, completed_at
		FROM notification_attempts
		WHERE notification_id = $1
		ORDER BY id DESC
	`, notificationID)
	if err != nil {
		return nil, fmt.Errorf("list notification attempts: %w", err)
	}
	defer rows.Close()
	history := make([]NotificationAttempt, 0)
	for rows.Next() {
		var attempt NotificationAttempt
		if err := rows.Scan(&attempt.Attempt, &attempt.Success, &attempt.ErrorCode, &attempt.StartedAt, &attempt.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan notification attempt: %w", err)
		}
		attempt.StartedAt = attempt.StartedAt.UTC()
		attempt.CompletedAt = attempt.CompletedAt.UTC()
		history = append(history, attempt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification attempts: %w", err)
	}
	return history, nil
}
