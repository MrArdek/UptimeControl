package monitoring

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MrArdek/UptimeControl/internal/identity"
	"github.com/MrArdek/UptimeControl/internal/pagination"
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
		          monitors.type, monitors.url, monitors.target, monitors.timeout_seconds
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
			&monitor.Target,
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
		       monitors.type, monitors.url, monitors.target, monitors.timeout_seconds
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
		&monitor.Target,
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

func (store *PostgresStore) CheckPage(
	ctx context.Context,
	userID,
	projectID,
	monitorID string,
	query HistoryQuery,
) (CheckPage, error) {
	arguments := []any{userID, projectID, monitorID, query.From, query.To}
	statement := `
		SELECT uptime_checks.id, uptime_checks.checked_at, uptime_checks.available,
		       uptime_checks.status_code, uptime_checks.response_time_ms, uptime_checks.error_message
		FROM uptime_checks
		JOIN monitors ON monitors.id = uptime_checks.monitor_id
		JOIN projects ON projects.id = monitors.project_id
		WHERE projects.user_id = $1 AND projects.id = $2 AND monitors.id = $3
		  AND projects.deleted_at IS NULL AND monitors.deleted_at IS NULL
		  AND uptime_checks.checked_at >= $4 AND uptime_checks.checked_at < $5`
	if query.Cursor != "" {
		cursorTime, cursorIDText, err := pagination.Decode(query.Cursor, historyScope("checks", userID, projectID, monitorID, query))
		cursorID, parseErr := strconv.ParseInt(cursorIDText, 10, 64)
		if err != nil || parseErr != nil || cursorID < 1 {
			return CheckPage{}, ErrInvalidCursor
		}
		statement += ` AND (uptime_checks.checked_at, uptime_checks.id) < ($6, $7)`
		arguments = append(arguments, cursorTime, cursorID)
	}
	statement += ` ORDER BY uptime_checks.checked_at DESC, uptime_checks.id DESC LIMIT $` + fmt.Sprint(len(arguments)+1)
	arguments = append(arguments, query.Limit+1)
	rows, err := store.database.Query(ctx, statement, arguments...)
	if err != nil {
		return CheckPage{}, fmt.Errorf("list check page: %w", err)
	}
	defer rows.Close()
	checks := make([]Check, 0, query.Limit+1)
	for rows.Next() {
		check, err := scanCheck(rows)
		if err != nil {
			return CheckPage{}, fmt.Errorf("scan check page: %w", err)
		}
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return CheckPage{}, fmt.Errorf("iterate check page: %w", err)
	}
	page := CheckPage{Checks: checks, From: query.From, To: query.To}
	if len(checks) > query.Limit {
		page.Checks = checks[:query.Limit]
		last := page.Checks[len(page.Checks)-1]
		next, err := pagination.Encode(last.CheckedAt, checkCursorID(last.ID), historyScope("checks", userID, projectID, monitorID, query))
		if err != nil {
			return CheckPage{}, fmt.Errorf("encode check cursor: %w", err)
		}
		page.NextCursor = &next
	}
	return page, nil
}

func (store *PostgresStore) IncidentPage(
	ctx context.Context,
	userID,
	projectID,
	monitorID string,
	query HistoryQuery,
) (IncidentPage, error) {
	arguments := []any{userID, projectID, monitorID, query.From, query.To}
	statement := `
		SELECT incidents.id, incidents.started_at, incidents.resolved_at, incidents.cause
		FROM incidents
		JOIN monitors ON monitors.id = incidents.monitor_id
		JOIN projects ON projects.id = monitors.project_id
		WHERE projects.user_id = $1 AND projects.id = $2 AND monitors.id = $3
		  AND projects.deleted_at IS NULL AND monitors.deleted_at IS NULL
		  AND incidents.started_at < $5 AND COALESCE(incidents.resolved_at, $5) > $4`
	if query.Cursor != "" {
		cursorTime, cursorID, err := pagination.Decode(query.Cursor, historyScope("incidents", userID, projectID, monitorID, query))
		if err != nil || !identity.ValidUUID(cursorID) {
			return IncidentPage{}, ErrInvalidCursor
		}
		statement += ` AND (incidents.started_at, incidents.id) < ($6, $7)`
		arguments = append(arguments, cursorTime, cursorID)
	}
	statement += ` ORDER BY incidents.started_at DESC, incidents.id DESC LIMIT $` + fmt.Sprint(len(arguments)+1)
	arguments = append(arguments, query.Limit+1)
	rows, err := store.database.Query(ctx, statement, arguments...)
	if err != nil {
		return IncidentPage{}, fmt.Errorf("list incident page: %w", err)
	}
	defer rows.Close()
	incidents := make([]Incident, 0, query.Limit+1)
	now := time.Now().UTC()
	for rows.Next() {
		incident, err := scanIncident(rows)
		if err != nil {
			return IncidentPage{}, fmt.Errorf("scan incident page: %w", err)
		}
		setIncidentDuration(&incident, now)
		incidents = append(incidents, incident)
	}
	if err := rows.Err(); err != nil {
		return IncidentPage{}, fmt.Errorf("iterate incident page: %w", err)
	}
	page := IncidentPage{Incidents: incidents, From: query.From, To: query.To}
	if len(incidents) > query.Limit {
		page.Incidents = incidents[:query.Limit]
		last := page.Incidents[len(page.Incidents)-1]
		next, err := pagination.Encode(last.StartedAt, last.ID, historyScope("incidents", userID, projectID, monitorID, query))
		if err != nil {
			return IncidentPage{}, fmt.Errorf("encode incident cursor: %w", err)
		}
		page.NextCursor = &next
	}
	return page, nil
}

func (store *PostgresStore) Summary(
	ctx context.Context,
	userID,
	projectID,
	monitorID string,
	query HistoryQuery,
) (MonitorSummary, error) {
	var enabled bool
	var interval int
	var checkedAt pgtype.Timestamptz
	var available pgtype.Bool
	err := store.database.QueryRow(ctx, `
		SELECT monitors.enabled, monitors.check_interval_seconds,
		       monitors.last_checked_at, monitors.last_available
		FROM monitors
		JOIN projects ON projects.id = monitors.project_id
		WHERE projects.user_id = $1 AND projects.id = $2 AND monitors.id = $3
		  AND projects.deleted_at IS NULL AND monitors.deleted_at IS NULL
	`, userID, projectID, monitorID).Scan(&enabled, &interval, &checkedAt, &available)
	if errors.Is(err, pgx.ErrNoRows) {
		return MonitorSummary{}, ErrNotFound
	}
	if err != nil {
		return MonitorSummary{}, fmt.Errorf("select monitor summary state: %w", err)
	}
	var lastCheckedAt *time.Time
	var lastAvailable *bool
	if checkedAt.Valid {
		value := checkedAt.Time.UTC()
		lastCheckedAt = &value
	}
	if available.Valid {
		value := available.Bool
		lastAvailable = &value
	}

	checks, err := store.summaryChecks(ctx, userID, projectID, monitorID, query)
	if err != nil {
		return MonitorSummary{}, err
	}
	lastIncident, err := store.lastIncident(ctx, userID, projectID, monitorID, query.To)
	if err != nil {
		return MonitorSummary{}, err
	}
	now := time.Now().UTC()
	if lastIncident != nil {
		setIncidentDuration(lastIncident, now)
	}
	return calculateSummary(query, now, enabled, interval, lastCheckedAt, lastAvailable, checks, lastIncident), nil
}

func (store *PostgresStore) summaryChecks(
	ctx context.Context,
	userID,
	projectID,
	monitorID string,
	query HistoryQuery,
) ([]Check, error) {
	rows, err := store.database.Query(ctx, `
		WITH selected AS (
			(SELECT uptime_checks.id, uptime_checks.checked_at, uptime_checks.available,
			        uptime_checks.status_code, uptime_checks.response_time_ms, uptime_checks.error_message
			 FROM uptime_checks
			 JOIN monitors ON monitors.id = uptime_checks.monitor_id
			 JOIN projects ON projects.id = monitors.project_id
			 WHERE projects.user_id = $1 AND projects.id = $2 AND monitors.id = $3
			   AND projects.deleted_at IS NULL AND monitors.deleted_at IS NULL
			   AND uptime_checks.checked_at < $4
			 ORDER BY uptime_checks.checked_at DESC, uptime_checks.id DESC LIMIT 1)
			UNION ALL
			(SELECT uptime_checks.id, uptime_checks.checked_at, uptime_checks.available,
			        uptime_checks.status_code, uptime_checks.response_time_ms, uptime_checks.error_message
			 FROM uptime_checks
			 JOIN monitors ON monitors.id = uptime_checks.monitor_id
			 JOIN projects ON projects.id = monitors.project_id
			 WHERE projects.user_id = $1 AND projects.id = $2 AND monitors.id = $3
			   AND projects.deleted_at IS NULL AND monitors.deleted_at IS NULL
			   AND uptime_checks.checked_at >= $4 AND uptime_checks.checked_at < $5)
		)
		SELECT id, checked_at, available, status_code, response_time_ms, error_message
		FROM selected ORDER BY checked_at, id
	`, userID, projectID, monitorID, query.From, query.To)
	if err != nil {
		return nil, fmt.Errorf("select summary checks: %w", err)
	}
	defer rows.Close()
	checks := make([]Check, 0)
	for rows.Next() {
		check, err := scanCheck(rows)
		if err != nil {
			return nil, fmt.Errorf("scan summary check: %w", err)
		}
		checks = append(checks, check)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate summary checks: %w", err)
	}
	return checks, nil
}

func (store *PostgresStore) lastIncident(
	ctx context.Context,
	userID,
	projectID,
	monitorID string,
	before time.Time,
) (*Incident, error) {
	row := store.database.QueryRow(ctx, `
		SELECT incidents.id, incidents.started_at, incidents.resolved_at, incidents.cause
		FROM incidents
		JOIN monitors ON monitors.id = incidents.monitor_id
		JOIN projects ON projects.id = monitors.project_id
		WHERE projects.user_id = $1 AND projects.id = $2 AND monitors.id = $3
		  AND projects.deleted_at IS NULL AND monitors.deleted_at IS NULL
		  AND incidents.started_at < $4
		ORDER BY incidents.started_at DESC, incidents.id DESC LIMIT 1
	`, userID, projectID, monitorID, before)
	incident, err := scanIncident(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select last incident: %w", err)
	}
	return &incident, nil
}

func scanCheck(row rowScanner) (Check, error) {
	var check Check
	var statusCode pgtype.Int4
	var responseTime pgtype.Int8
	var errorMessage pgtype.Text
	if err := row.Scan(&check.ID, &check.CheckedAt, &check.Available, &statusCode, &responseTime, &errorMessage); err != nil {
		return Check{}, err
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
	return check, nil
}

func scanIncident(row rowScanner) (Incident, error) {
	var incident Incident
	var resolvedAt pgtype.Timestamptz
	var cause pgtype.Text
	if err := row.Scan(&incident.ID, &incident.StartedAt, &resolvedAt, &cause); err != nil {
		return Incident{}, err
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
	return incident, nil
}

func setIncidentDuration(incident *Incident, now time.Time) {
	end := now.UTC()
	if incident.ResolvedAt != nil {
		end = incident.ResolvedAt.UTC()
	}
	if end.After(incident.StartedAt) {
		incident.DurationSeconds = int64(end.Sub(incident.StartedAt) / time.Second)
	}
}

type resultExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type rowScanner interface {
	Scan(...any) error
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
		ProjectID:   monitor.ProjectID,
		ProjectName: monitor.ProjectName,
		MonitorID:   monitor.ID,
		MonitorName: monitor.Name,
		URL:         monitor.URL,
		Target:      monitor.Target,
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
		if err := enqueueTransition(ctx, executor, &transition); err != nil {
			return Transition{}, err
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
	if err := enqueueTransition(ctx, executor, &transition); err != nil {
		return Transition{}, err
	}
	return transition, nil
}

func (store *PostgresStore) RecordDelivery(
	ctx context.Context,
	webhookID,
	event string,
	statusCode *int,
	attempts int,
	lastError *string,
) error {
	if attempts < 1 {
		attempts = 1
	}
	if _, err := store.database.Exec(ctx, `
		INSERT INTO webhook_deliveries (webhook_id, event, status_code, attempts, last_error)
		VALUES ($1, $2, $3, $4, $5)
	`, webhookID, event, optionalInt(statusCode), attempts, optionalError(lastError)); err != nil {
		return fmt.Errorf("insert webhook delivery: %w", err)
	}

	return nil
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
