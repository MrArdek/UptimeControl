package monitoring

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/MrArdek/UptimeControl/internal/identity"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const maximumHistoryItems = 500

type PostgresStore struct {
	database *pgxpool.Pool
}

func NewPostgresStore(database *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{database: database}
}

func (store *PostgresStore) ClaimDue(ctx context.Context, now time.Time, limit int) ([]DueMonitor, error) {
	if limit < 1 {
		return []DueMonitor{}, nil
	}
	rows, err := store.database.Query(ctx, `
		WITH due AS (
			SELECT monitors.id
			FROM monitors
			JOIN projects ON projects.id = monitors.project_id
			WHERE monitors.enabled = true
			  AND monitors.deleted_at IS NULL
			  AND projects.deleted_at IS NULL
			  AND monitors.next_check_at <= $1
			ORDER BY monitors.next_check_at, monitors.id
			FOR UPDATE OF monitors SKIP LOCKED
			LIMIT $2
		)
		UPDATE monitors
		SET next_check_at = $1 + make_interval(secs => monitors.check_interval_seconds),
		    updated_at = $1
		FROM due, projects
		WHERE monitors.id = due.id
		  AND projects.id = monitors.project_id
		RETURNING monitors.id, monitors.project_id, projects.name, monitors.name,
		          monitors.type, monitors.url, monitors.timeout_seconds
	`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim due monitors: %w", err)
	}
	defer rows.Close()

	monitors := make([]DueMonitor, 0)
	for rows.Next() {
		var monitor DueMonitor
		if err := rows.Scan(
			&monitor.ID,
			&monitor.ProjectID,
			&monitor.ProjectName,
			&monitor.Name,
			&monitor.Type,
			&monitor.URL,
			&monitor.TimeoutSeconds,
		); err != nil {
			return nil, fmt.Errorf("scan due monitor: %w", err)
		}
		monitors = append(monitors, monitor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due monitors: %w", err)
	}
	return monitors, nil
}

func (store *PostgresStore) RecordResult(
	ctx context.Context,
	monitor DueMonitor,
	result Result,
) (Transition, error) {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return Transition{}, fmt.Errorf("begin result transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	transition, err := recordResult(ctx, transaction, monitor, result)
	if err != nil {
		return Transition{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Transition{}, fmt.Errorf("commit monitor result: %w", err)
	}
	return transition, nil
}

func (store *PostgresStore) RecordHeartbeat(
	ctx context.Context,
	tokenHash []byte,
	now time.Time,
) (Transition, error) {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return Transition{}, fmt.Errorf("begin heartbeat transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	var monitor DueMonitor
	err = transaction.QueryRow(ctx, `
		SELECT monitors.id, monitors.project_id, projects.name, monitors.name,
		       monitors.type, monitors.url, monitors.timeout_seconds
		FROM monitors
		JOIN projects ON projects.id = monitors.project_id
		WHERE monitors.heartbeat_token_hash = $1
		  AND monitors.type = 'heartbeat'
		  AND monitors.enabled = true
		  AND monitors.deleted_at IS NULL
		  AND projects.deleted_at IS NULL
		FOR UPDATE OF monitors
	`, tokenHash).Scan(
		&monitor.ID,
		&monitor.ProjectID,
		&monitor.ProjectName,
		&monitor.Name,
		&monitor.Type,
		&monitor.URL,
		&monitor.TimeoutSeconds,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Transition{}, ErrNotFound
	}
	if err != nil {
		return Transition{}, fmt.Errorf("select heartbeat monitor: %w", err)
	}

	if _, err := transaction.Exec(ctx, `
		UPDATE monitors
		SET next_check_at = $2::timestamptz + make_interval(secs => check_interval_seconds)
		WHERE id = $1
	`, monitor.ID, now); err != nil {
		return Transition{}, fmt.Errorf("schedule next heartbeat: %w", err)
	}
	transition, err := recordResult(ctx, transaction, monitor, Result{
		CheckedAt: now,
		Available: true,
	})
	if err != nil {
		return Transition{}, err
	}
	if err := transaction.Commit(ctx); err != nil {
		return Transition{}, fmt.Errorf("commit heartbeat: %w", err)
	}
	return transition, nil
}

func (store *PostgresStore) Checks(
	ctx context.Context,
	userID,
	projectID,
	monitorID string,
	limit int,
) ([]Check, error) {
	limit = normalizeLimit(limit)
	rows, err := store.database.Query(ctx, `
		SELECT uptime_checks.id, uptime_checks.checked_at, uptime_checks.available,
		       uptime_checks.status_code, uptime_checks.response_time_ms, uptime_checks.error_message
		FROM uptime_checks
		JOIN monitors ON monitors.id = uptime_checks.monitor_id
		JOIN projects ON projects.id = monitors.project_id
		WHERE projects.user_id = $1
		  AND projects.id = $2
		  AND monitors.id = $3
		  AND projects.deleted_at IS NULL
		  AND monitors.deleted_at IS NULL
		ORDER BY uptime_checks.checked_at DESC, uptime_checks.id DESC
		LIMIT $4
	`, userID, projectID, monitorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list checks: %w", err)
	}
	defer rows.Close()

	checks := make([]Check, 0)
	for rows.Next() {
		var check Check
		var statusCode pgtype.Int4
		var responseTime pgtype.Int8
		var errorMessage pgtype.Text
		if err := rows.Scan(
			&check.ID,
			&check.CheckedAt,
			&check.Available,
			&statusCode,
			&responseTime,
			&errorMessage,
		); err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		check.CheckedAt = check.CheckedAt.UTC()
		if statusCode.Valid {
			value := int(statusCode.Int32)
			check.StatusCode = &value
		}
		if responseTime.Valid {
			value := responseTime.Int64
			check.ResponseTimeMS = &value
		}
		if errorMessage.Valid {
			value := errorMessage.String
			check.Error = &value
		}
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate checks: %w", err)
	}
	return checks, nil
}

func (store *PostgresStore) Incidents(
	ctx context.Context,
	userID,
	projectID,
	monitorID string,
	limit int,
) ([]Incident, error) {
	limit = normalizeLimit(limit)
	rows, err := store.database.Query(ctx, `
		SELECT incidents.id, incidents.started_at, incidents.resolved_at, incidents.cause
		FROM incidents
		JOIN monitors ON monitors.id = incidents.monitor_id
		JOIN projects ON projects.id = monitors.project_id
		WHERE projects.user_id = $1
		  AND projects.id = $2
		  AND monitors.id = $3
		  AND projects.deleted_at IS NULL
		  AND monitors.deleted_at IS NULL
		ORDER BY incidents.started_at DESC, incidents.id DESC
		LIMIT $4
	`, userID, projectID, monitorID, limit)
	if err != nil {
		return nil, fmt.Errorf("list incidents: %w", err)
	}
	defer rows.Close()

	incidents := make([]Incident, 0)
	for rows.Next() {
		var incident Incident
		var resolvedAt pgtype.Timestamptz
		var cause pgtype.Text
		if err := rows.Scan(&incident.ID, &incident.StartedAt, &resolvedAt, &cause); err != nil {
			return nil, fmt.Errorf("scan incident: %w", err)
		}
		incident.StartedAt = incident.StartedAt.UTC()
		if resolvedAt.Valid {
			value := resolvedAt.Time.UTC()
			incident.ResolvedAt = &value
		}
		if cause.Valid {
			value := cause.String
			incident.Cause = &value
		}
		incidents = append(incidents, incident)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate incidents: %w", err)
	}
	return incidents, nil
}

type resultExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func recordResult(
	ctx context.Context,
	executor resultExecutor,
	monitor DueMonitor,
	result Result,
) (Transition, error) {
	errorMessage := optionalError(result.Error)
	commandTag, err := executor.Exec(ctx, `
		UPDATE monitors
		SET last_checked_at = $2,
		    last_available = $3,
		    last_status_code = $4,
		    last_response_time_ms = $5,
		    last_error = $6,
		    updated_at = $2
		WHERE id = $1 AND deleted_at IS NULL
	`, monitor.ID, result.CheckedAt, result.Available, optionalInt(result.StatusCode), optionalInt64(result.ResponseTimeMS), errorMessage)
	if err != nil {
		return Transition{}, fmt.Errorf("update monitor status: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		// The owner may have deleted the monitor while a check was running.
		return Transition{}, nil
	}
	if _, err := executor.Exec(ctx, `
		INSERT INTO uptime_checks (
			monitor_id, checked_at, available, status_code, response_time_ms, error_message
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, monitor.ID, result.CheckedAt, result.Available, optionalInt(result.StatusCode), optionalInt64(result.ResponseTimeMS), errorMessage); err != nil {
		return Transition{}, fmt.Errorf("insert uptime check: %w", err)
	}

	transition := Transition{
		ProjectName: monitor.ProjectName,
		MonitorName: monitor.Name,
		URL:         monitor.URL,
		OccurredAt:  result.CheckedAt,
	}
	if !result.Available {
		incidentID, err := identity.NewUUID()
		if err != nil {
			return Transition{}, fmt.Errorf("generate incident ID: %w", err)
		}
		commandTag, err := executor.Exec(ctx, `
			INSERT INTO incidents (id, monitor_id, started_at, cause)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (monitor_id) WHERE resolved_at IS NULL DO NOTHING
		`, incidentID, monitor.ID, result.CheckedAt, errorMessage)
		if err != nil {
			return Transition{}, fmt.Errorf("open incident: %w", err)
		}
		if commandTag.RowsAffected() > 0 {
			transition.Kind = "down"
			if result.Error != nil {
				transition.Cause = *result.Error
			}
		}
		return transition, nil
	}

	commandTag, err = executor.Exec(ctx, `
		UPDATE incidents
		SET resolved_at = $2
		WHERE monitor_id = $1 AND resolved_at IS NULL
	`, monitor.ID, result.CheckedAt)
	if err != nil {
		return Transition{}, fmt.Errorf("resolve incident: %w", err)
	}
	if commandTag.RowsAffected() > 0 {
		transition.Kind = "recovered"
	}
	return transition, nil
}

func normalizeLimit(limit int) int {
	if limit < 1 {
		return 100
	}
	if limit > maximumHistoryItems {
		return maximumHistoryItems
	}
	return limit
}

func optionalError(value *string) any {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if len(trimmed) > 500 {
		trimmed = trimmed[:500]
	}
	if trimmed == "" {
		return nil
	}
	return trimmed
}

func optionalInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}
