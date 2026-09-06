package projects

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maximumListedProjects      = 100
	uniqueViolationCode        = "23505"
	uniqueMonitorURLConstraint = "monitors_project_url_unique_active"
)

type PostgresStore struct {
	database *pgxpool.Pool
}

func NewPostgresStore(database *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{database: database}
}

func (store *PostgresStore) List(ctx context.Context, userID string) ([]Project, error) {
	rows, err := store.database.Query(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM projects
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, userID, maximumListedProjects)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()

	projectList := make([]Project, 0)
	for rows.Next() {
		var project Project
		if err := rows.Scan(&project.ID, &project.Name, &project.Description, &project.CreatedAt, &project.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		project.CreatedAt = project.CreatedAt.UTC()
		project.UpdatedAt = project.UpdatedAt.UTC()
		project.Monitors = make([]Monitor, 0)
		projectList = append(projectList, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate projects: %w", err)
	}

	for index := range projectList {
		monitors, err := store.listMonitors(ctx, projectList[index].ID)
		if err != nil {
			return nil, err
		}
		projectList[index].Monitors = monitors
	}
	return projectList, nil
}

func (store *PostgresStore) ByID(ctx context.Context, userID, projectID string) (Project, error) {
	var project Project
	err := store.database.QueryRow(ctx, `
		SELECT id, name, description, created_at, updated_at
		FROM projects
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, projectID, userID).Scan(
		&project.ID,
		&project.Name,
		&project.Description,
		&project.CreatedAt,
		&project.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("select project: %w", err)
	}
	project.CreatedAt = project.CreatedAt.UTC()
	project.UpdatedAt = project.UpdatedAt.UTC()
	project.Monitors, err = store.listMonitors(ctx, project.ID)
	if err != nil {
		return Project{}, err
	}
	return project, nil
}

func (store *PostgresStore) Create(ctx context.Context, userID string, project Project, monitor Monitor) error {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin project creation: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if _, err := transaction.Exec(ctx, `
		INSERT INTO projects (id, user_id, name, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, project.ID, userID, project.Name, project.Description, project.CreatedAt, project.UpdatedAt); err != nil {
		return fmt.Errorf("insert project: %w", err)
	}

	if err := insertMonitor(ctx, transaction, monitor); err != nil {
		return err
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit project creation: %w", err)
	}
	return nil
}

func (store *PostgresStore) UpdateProject(
	ctx context.Context,
	userID,
	projectID string,
	input UpdateProjectInput,
) (Project, error) {
	commandTag, err := store.database.Exec(ctx, `
		UPDATE projects
		SET name = COALESCE($3, name),
		    description = COALESCE($4, description),
		    updated_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, projectID, userID, optionalString(input.Name), optionalString(input.Description))
	if err != nil {
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return Project{}, ErrNotFound
	}
	return store.ByID(ctx, userID, projectID)
}

func (store *PostgresStore) SoftDeleteProject(ctx context.Context, userID, projectID string) error {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin project deletion: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	commandTag, err := transaction.Exec(ctx, `
		UPDATE projects
		SET deleted_at = now(), updated_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, projectID, userID)
	if err != nil {
		return fmt.Errorf("soft-delete project: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := transaction.Exec(ctx, `
		UPDATE monitors
		SET deleted_at = now(), enabled = false, updated_at = now()
		WHERE project_id = $1 AND deleted_at IS NULL
	`, projectID); err != nil {
		return fmt.Errorf("soft-delete project monitors: %w", err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit project deletion: %w", err)
	}
	return nil
}

func (store *PostgresStore) AddMonitor(
	ctx context.Context,
	userID,
	projectID string,
	monitor Monitor,
) (Monitor, error) {
	commandTag, err := store.database.Exec(ctx, `
		INSERT INTO monitors (
			id, project_id, type, name, url, heartbeat_token_hash, check_interval_seconds,
			timeout_seconds, enabled, next_check_at, created_at, updated_at
		)
		SELECT $1, projects.id, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
		FROM projects
		WHERE projects.id = $2 AND projects.user_id = $13 AND projects.deleted_at IS NULL
	`,
		monitor.ID,
		projectID,
		monitor.Type,
		monitor.Name,
		monitor.URL,
		monitor.HeartbeatTokenHash,
		monitor.CheckIntervalSeconds,
		monitor.TimeoutSeconds,
		monitor.Enabled,
		initialNextCheck(monitor),
		monitor.CreatedAt,
		monitor.UpdatedAt,
		userID,
	)
	if isDuplicateMonitorURL(err) {
		return Monitor{}, ErrAlreadyExists
	}
	if err != nil {
		return Monitor{}, fmt.Errorf("insert monitor: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return Monitor{}, ErrNotFound
	}
	return monitor, nil
}

func (store *PostgresStore) UpdateMonitor(
	ctx context.Context,
	userID,
	projectID,
	monitorID string,
	input UpdateMonitorInput,
) (Monitor, error) {
	row := store.database.QueryRow(ctx, `
		UPDATE monitors
		SET name = COALESCE($4, name),
		    url = COALESCE($5, url),
		    check_interval_seconds = COALESCE($6, check_interval_seconds),
		    timeout_seconds = COALESCE($7, timeout_seconds),
		    enabled = COALESCE($8, enabled),
		    next_check_at = CASE
		        WHEN COALESCE($8, enabled) THEN LEAST(next_check_at, now())
		        ELSE next_check_at
		    END,
		    updated_at = now()
		WHERE id = $1
		  AND project_id = $2
		  AND deleted_at IS NULL
		  AND EXISTS (
		      SELECT 1 FROM projects
		      WHERE projects.id = $2 AND projects.user_id = $3 AND projects.deleted_at IS NULL
		  )
		RETURNING id, project_id, type, name, url, check_interval_seconds,
		          timeout_seconds, enabled, last_checked_at, last_available,
		          last_status_code, last_response_time_ms, last_error, created_at, updated_at
	`,
		monitorID,
		projectID,
		userID,
		optionalString(input.Name),
		optionalString(input.URL),
		optionalInt(input.CheckIntervalSeconds),
		optionalInt(input.TimeoutSeconds),
		optionalBool(input.Enabled),
	)
	monitor, err := scanMonitor(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return Monitor{}, ErrNotFound
	}
	if isDuplicateMonitorURL(err) {
		return Monitor{}, ErrAlreadyExists
	}
	if err != nil {
		return Monitor{}, fmt.Errorf("update monitor: %w", err)
	}
	return monitor, nil
}

func (store *PostgresStore) SoftDeleteMonitor(ctx context.Context, userID, projectID, monitorID string) error {
	commandTag, err := store.database.Exec(ctx, `
		UPDATE monitors
		SET deleted_at = now(), enabled = false, updated_at = now()
		WHERE id = $1
		  AND project_id = $2
		  AND deleted_at IS NULL
		  AND EXISTS (
		      SELECT 1 FROM projects
		      WHERE projects.id = $2 AND projects.user_id = $3 AND projects.deleted_at IS NULL
		  )
	`, monitorID, projectID, userID)
	if err != nil {
		return fmt.Errorf("soft-delete monitor: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (store *PostgresStore) listMonitors(ctx context.Context, projectID string) ([]Monitor, error) {
	rows, err := store.database.Query(ctx, `
		SELECT id, project_id, type, name, url, check_interval_seconds,
		       timeout_seconds, enabled, last_checked_at, last_available,
		       last_status_code, last_response_time_ms, last_error, created_at, updated_at
		FROM monitors
		WHERE project_id = $1 AND deleted_at IS NULL
		ORDER BY created_at, id
	`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list monitors: %w", err)
	}
	defer rows.Close()

	monitors := make([]Monitor, 0)
	for rows.Next() {
		monitor, err := scanMonitor(rows)
		if err != nil {
			return nil, fmt.Errorf("scan monitor: %w", err)
		}
		monitors = append(monitors, monitor)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monitors: %w", err)
	}
	return monitors, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanMonitor(row rowScanner) (Monitor, error) {
	var monitor Monitor
	var checkedAt pgtype.Timestamptz
	var available pgtype.Bool
	var statusCode pgtype.Int4
	var responseTime pgtype.Int8
	var lastError pgtype.Text
	err := row.Scan(
		&monitor.ID,
		&monitor.ProjectID,
		&monitor.Type,
		&monitor.Name,
		&monitor.URL,
		&monitor.CheckIntervalSeconds,
		&monitor.TimeoutSeconds,
		&monitor.Enabled,
		&checkedAt,
		&available,
		&statusCode,
		&responseTime,
		&lastError,
		&monitor.CreatedAt,
		&monitor.UpdatedAt,
	)
	if err != nil {
		return Monitor{}, err
	}
	if checkedAt.Valid {
		value := checkedAt.Time.UTC()
		monitor.LastCheckedAt = &value
	}
	if available.Valid {
		value := available.Bool
		monitor.LastAvailable = &value
	}
	if statusCode.Valid {
		value := int(statusCode.Int32)
		monitor.LastStatusCode = &value
	}
	if responseTime.Valid {
		value := responseTime.Int64
		monitor.LastResponseTimeMS = &value
	}
	if lastError.Valid {
		value := lastError.String
		monitor.LastError = &value
	}
	monitor.CreatedAt = monitor.CreatedAt.UTC()
	monitor.UpdatedAt = monitor.UpdatedAt.UTC()
	return monitor, nil
}

func insertMonitor(ctx context.Context, executor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, monitor Monitor) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO monitors (
			id, project_id, type, name, url, heartbeat_token_hash, check_interval_seconds,
			timeout_seconds, enabled, next_check_at, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`,
		monitor.ID,
		monitor.ProjectID,
		monitor.Type,
		monitor.Name,
		monitor.URL,
		monitor.HeartbeatTokenHash,
		monitor.CheckIntervalSeconds,
		monitor.TimeoutSeconds,
		monitor.Enabled,
		initialNextCheck(monitor),
		monitor.CreatedAt,
		monitor.UpdatedAt,
	)
	if isDuplicateMonitorURL(err) {
		return ErrAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("insert monitor: %w", err)
	}
	return nil
}

func isDuplicateMonitorURL(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		postgresError.Code == uniqueViolationCode &&
		postgresError.ConstraintName == uniqueMonitorURLConstraint
}

func initialNextCheck(monitor Monitor) time.Time {
	if monitor.Type == "heartbeat" {
		return monitor.CreatedAt.Add(time.Duration(monitor.CheckIntervalSeconds) * time.Second)
	}
	return monitor.CreatedAt
}

func optionalString(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func optionalBool(value *bool) any {
	if value == nil {
		return nil
	}
	return *value
}

func normalizeTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
