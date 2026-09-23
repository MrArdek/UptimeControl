package checknodes

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/MrArdek/UptimeControl/internal/monitoring"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresStore struct{ database *pgxpool.Pool }

func NewPostgresStore(database *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{database: database}
}

func (store *PostgresStore) Create(ctx context.Context, userID string, node Node, secretHash []byte) (Node, error) {
	_, err := store.database.Exec(ctx, `
		INSERT INTO check_nodes (id, user_id, name, region, secret_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
	`, node.ID, userID, node.Name, node.Region, secretHash, node.CreatedAt)
	if err != nil {
		return Node{}, fmt.Errorf("create check node: %w", err)
	}
	node.Assignments = []Assignment{}
	return node, nil
}

func (store *PostgresStore) List(ctx context.Context, userID string, now time.Time) ([]Node, error) {
	rows, err := store.database.Query(ctx, `
		SELECT id, name, region, enabled, last_seen_at, created_at, revoked_at,
		       enabled AND revoked_at IS NULL AND last_seen_at >= $2 - interval '2 minutes'
		FROM check_nodes WHERE user_id = $1 ORDER BY created_at, id
	`, userID, now)
	if err != nil {
		return nil, fmt.Errorf("list check nodes: %w", err)
	}
	defer rows.Close()
	nodes := make([]Node, 0)
	for rows.Next() {
		var node Node
		if err := rows.Scan(&node.ID, &node.Name, &node.Region, &node.Enabled, &node.LastSeenAt, &node.CreatedAt, &node.RevokedAt, &node.Online); err != nil {
			return nil, fmt.Errorf("scan check node: %w", err)
		}
		node.CreatedAt = node.CreatedAt.UTC()
		if node.LastSeenAt != nil {
			value := node.LastSeenAt.UTC()
			node.LastSeenAt = &value
		}
		if node.RevokedAt != nil {
			value := node.RevokedAt.UTC()
			node.RevokedAt = &value
		}
		node.Assignments = []Assignment{}
		nodes = append(nodes, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate check nodes: %w", err)
	}
	for index := range nodes {
		assignments, err := store.assignmentsForNode(ctx, nodes[index].ID)
		if err != nil {
			return nil, err
		}
		nodes[index].Assignments = assignments
	}
	return nodes, nil
}

func (store *PostgresStore) assignmentsForNode(ctx context.Context, nodeID string) ([]Assignment, error) {
	rows, err := store.database.Query(ctx, `
		SELECT ma.id, ma.node_id, ma.monitor_id, m.project_id, p.name, m.name, m.type,
		       m.url, m.target, cn.region, m.check_interval_seconds, m.timeout_seconds,
		       ma.leased_until, ma.last_result_at
		FROM monitor_assignments ma
		JOIN check_nodes cn ON cn.id = ma.node_id
		JOIN monitors m ON m.id = ma.monitor_id
		JOIN projects p ON p.id = m.project_id
		WHERE ma.node_id = $1 AND ma.enabled = true AND m.deleted_at IS NULL AND p.deleted_at IS NULL
		ORDER BY p.name, m.name, ma.id
	`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("list node assignments: %w", err)
	}
	defer rows.Close()
	assignments := make([]Assignment, 0)
	for rows.Next() {
		assignment, err := scanAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan node assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate node assignments: %w", err)
	}
	return assignments, nil
}

func (store *PostgresStore) Revoke(ctx context.Context, userID, nodeID string, now time.Time) error {
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin node revoke: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE check_nodes SET enabled = false, revoked_at = $3, updated_at = $3 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, nodeID, userID, now)
	if err != nil {
		return fmt.Errorf("revoke check node: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE monitor_assignments SET enabled = false, updated_at = $2 WHERE node_id = $1 AND enabled = true`, nodeID, now); err != nil {
		return fmt.Errorf("disable node assignments: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit node revoke: %w", err)
	}
	return nil
}

func (store *PostgresStore) Assign(ctx context.Context, userID, nodeID string, assignment Assignment, now time.Time) (Assignment, error) {
	err := store.database.QueryRow(ctx, `
		INSERT INTO monitor_assignments (id, node_id, monitor_id, next_check_at, created_at, updated_at)
		SELECT $3, cn.id, m.id, $5, $5, $5
		FROM check_nodes cn
		JOIN monitors m ON m.id = $4
		JOIN projects p ON p.id = m.project_id
		WHERE cn.id = $1 AND cn.user_id = $2 AND cn.enabled = true AND cn.revoked_at IS NULL
		  AND p.user_id = $2 AND p.deleted_at IS NULL AND m.deleted_at IS NULL
		  AND m.enabled = true AND m.type IN ('http', 'tcp')
		RETURNING id
	`, nodeID, userID, assignment.ID, assignment.MonitorID, now).Scan(&assignment.ID)
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return Assignment{}, ErrAlreadyAssigned
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return Assignment{}, ErrNotFound
	}
	if err != nil {
		return Assignment{}, fmt.Errorf("assign monitor to node: %w", err)
	}
	assignments, err := store.assignmentsForNode(ctx, nodeID)
	if err != nil {
		return Assignment{}, err
	}
	for _, item := range assignments {
		if item.ID == assignment.ID {
			return item, nil
		}
	}
	return Assignment{}, ErrNotFound
}

func (store *PostgresStore) RemoveAssignment(ctx context.Context, userID, nodeID, assignmentID string, now time.Time) error {
	tag, err := store.database.Exec(ctx, `
		UPDATE monitor_assignments ma SET enabled = false, updated_at = $4
		FROM check_nodes cn
		WHERE ma.id = $1 AND ma.node_id = $2 AND cn.id = ma.node_id AND cn.user_id = $3 AND ma.enabled = true
	`, assignmentID, nodeID, userID, now)
	if err != nil {
		return fmt.Errorf("remove monitor assignment: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (store *PostgresStore) Authenticate(ctx context.Context, secretHash []byte, now time.Time) (Node, error) {
	var node Node
	err := store.database.QueryRow(ctx, `
		UPDATE check_nodes SET last_seen_at = $2, updated_at = $2
		WHERE secret_hash = $1 AND enabled = true AND revoked_at IS NULL
		RETURNING id, name, region, enabled, last_seen_at, created_at
	`, secretHash, now).Scan(&node.ID, &node.Name, &node.Region, &node.Enabled, &node.LastSeenAt, &node.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Node{}, ErrUnauthorized
	}
	if err != nil {
		return Node{}, fmt.Errorf("authenticate check node: %w", err)
	}
	node.Online = true
	node.Assignments = []Assignment{}
	return node, nil
}

func (store *PostgresStore) ClaimAssignments(ctx context.Context, nodeID string, now time.Time, limit int) ([]Assignment, error) {
	rows, err := store.database.Query(ctx, `
		WITH due AS (
			SELECT ma.id
			FROM monitor_assignments ma
			JOIN check_nodes cn ON cn.id = ma.node_id
			JOIN monitors m ON m.id = ma.monitor_id
			JOIN projects p ON p.id = m.project_id
			WHERE ma.node_id = $1 AND ma.enabled = true AND ma.next_check_at <= $2
			  AND (ma.leased_until IS NULL OR ma.leased_until <= $2)
			  AND cn.enabled = true AND cn.revoked_at IS NULL
			  AND m.enabled = true AND m.deleted_at IS NULL AND p.deleted_at IS NULL
			ORDER BY ma.next_check_at, ma.id
			FOR UPDATE OF ma SKIP LOCKED LIMIT $3
		)
		UPDATE monitor_assignments ma
		SET next_check_at = $2 + make_interval(secs => m.check_interval_seconds),
		    leased_until = $2 + make_interval(secs => GREATEST(m.timeout_seconds + 30, 60)), updated_at = $2
		FROM due, monitors m, projects p, check_nodes cn
		WHERE ma.id = due.id AND m.id = ma.monitor_id AND p.id = m.project_id AND cn.id = ma.node_id
		RETURNING ma.id, ma.node_id, ma.monitor_id, m.project_id, p.name, m.name, m.type,
		          m.url, m.target, cn.region, m.check_interval_seconds, m.timeout_seconds,
		          ma.leased_until, ma.last_result_at
	`, nodeID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("claim node assignments: %w", err)
	}
	defer rows.Close()
	assignments := make([]Assignment, 0)
	for rows.Next() {
		assignment, err := scanAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan claimed assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate claimed assignments: %w", err)
	}
	return assignments, nil
}

func (store *PostgresStore) RecordResults(ctx context.Context, node Node, results []Result, now time.Time) (BatchOutcome, error) {
	tx, err := store.database.Begin(ctx)
	if err != nil {
		return BatchOutcome{}, fmt.Errorf("begin regional results: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	outcome := BatchOutcome{Rejected: []Rejection{}}
	for _, result := range results {
		monitor, active, err := assignmentMonitor(ctx, tx, node.ID, result.AssignmentID, result.MonitorID)
		if errors.Is(err, ErrNotFound) {
			outcome.Rejected = append(outcome.Rejected, Rejection{ID: result.ResultID, Code: "invalid_assignment"})
			continue
		}
		if err != nil {
			return BatchOutcome{}, err
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO uptime_checks (
				monitor_id, checked_at, available, status_code, response_time_ms, error_message,
				node_id, assignment_id, result_id, region, started_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
			ON CONFLICT (node_id, result_id) WHERE node_id IS NOT NULL AND result_id IS NOT NULL DO NOTHING
		`, result.MonitorID, result.FinishedAt, result.Available, result.StatusCode, result.ResponseTimeMS,
			result.ErrorCode, node.ID, result.AssignmentID, result.ResultID, node.Region, result.StartedAt)
		if err != nil {
			return BatchOutcome{}, fmt.Errorf("insert regional result: %w", err)
		}
		if tag.RowsAffected() == 0 {
			outcome.Duplicate++
			continue
		}
		outcome.Accepted++
		if _, err := tx.Exec(ctx, `UPDATE monitor_assignments SET last_result_at = $2, leased_until = NULL, updated_at = $3 WHERE id = $1`, result.AssignmentID, result.FinishedAt, now); err != nil {
			return BatchOutcome{}, fmt.Errorf("complete assignment: %w", err)
		}
		if !active {
			continue
		}
		available, decided, responseTime, err := regionalDecision(ctx, tx, monitor.ID, monitor.CheckIntervalSeconds, result.FinishedAt)
		if err != nil {
			return BatchOutcome{}, err
		}
		if decided {
			aggregate := monitoring.Result{CheckedAt: result.FinishedAt, Available: available, ResponseTimeMS: responseTime}
			if !available {
				code := "regional quorum unavailable"
				aggregate.Error = &code
			}
			if _, err := monitoring.RecordRegionalState(ctx, tx, monitor, aggregate); err != nil {
				return BatchOutcome{}, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return BatchOutcome{}, fmt.Errorf("commit regional results: %w", err)
	}
	return outcome, nil
}

func assignmentMonitor(ctx context.Context, tx pgx.Tx, nodeID, assignmentID, monitorID string) (monitoring.DueMonitor, bool, error) {
	var monitor monitoring.DueMonitor
	var active bool
	err := tx.QueryRow(ctx, `
		SELECT m.id, m.project_id, p.name, m.name, m.type, m.url, m.target,
		       m.timeout_seconds, m.check_interval_seconds,
		       ma.enabled AND m.enabled AND m.deleted_at IS NULL AND p.deleted_at IS NULL
		FROM monitor_assignments ma
		JOIN check_nodes cn ON cn.id = ma.node_id
		JOIN monitors m ON m.id = ma.monitor_id
		JOIN projects p ON p.id = m.project_id
		WHERE ma.id = $1 AND ma.node_id = $2 AND ma.monitor_id = $3
		  AND cn.enabled = true AND cn.revoked_at IS NULL
		FOR UPDATE OF ma
	`, assignmentID, nodeID, monitorID).Scan(&monitor.ID, &monitor.ProjectID, &monitor.ProjectName, &monitor.Name, &monitor.Type, &monitor.URL, &monitor.Target, &monitor.TimeoutSeconds, &monitor.CheckIntervalSeconds, &active)
	if errors.Is(err, pgx.ErrNoRows) {
		return monitoring.DueMonitor{}, false, ErrNotFound
	}
	if err != nil {
		return monitoring.DueMonitor{}, false, fmt.Errorf("select assignment monitor: %w", err)
	}
	return monitor, active, nil
}

func regionalDecision(ctx context.Context, tx pgx.Tx, monitorID string, interval int, at time.Time) (bool, bool, *int64, error) {
	var total, up, down int
	var average *int64
	var current bool
	err := tx.QueryRow(ctx, `
		WITH active AS (
			SELECT ma.id FROM monitor_assignments ma JOIN check_nodes cn ON cn.id = ma.node_id
			WHERE ma.monitor_id = $1 AND ma.enabled = true AND cn.enabled = true AND cn.revoked_at IS NULL
		), latest AS (
			SELECT active.id, sample.available, sample.response_time_ms
			FROM active LEFT JOIN LATERAL (
				SELECT available, response_time_ms FROM uptime_checks
				WHERE assignment_id = active.id
				  AND checked_at >= $2 - make_interval(secs => GREATEST($3 * 2, 300))
				  AND checked_at <= $2
				ORDER BY checked_at DESC, id DESC LIMIT 1
			) sample ON true
		)
		SELECT count(*), count(*) FILTER (WHERE available = true), count(*) FILTER (WHERE available = false),
		       round(avg(response_time_ms))::bigint,
		       (SELECT last_checked_at IS NULL OR last_checked_at <= $2 FROM monitors WHERE id = $1)
		FROM latest
	`, monitorID, at, interval).Scan(&total, &up, &down, &average, &current)
	if err != nil {
		return false, false, nil, fmt.Errorf("calculate regional quorum: %w", err)
	}
	if total == 0 || !current {
		return false, false, average, nil
	}
	quorum := total/2 + 1
	if down >= quorum {
		return false, true, average, nil
	}
	if up >= quorum {
		return true, true, average, nil
	}
	return false, false, average, nil
}

func scanAssignment(row interface{ Scan(...any) error }) (Assignment, error) {
	var assignment Assignment
	err := row.Scan(&assignment.ID, &assignment.NodeID, &assignment.MonitorID, &assignment.ProjectID,
		&assignment.ProjectName, &assignment.MonitorName, &assignment.Type, &assignment.URL, &assignment.Target,
		&assignment.Region, &assignment.CheckIntervalSeconds, &assignment.TimeoutSeconds,
		&assignment.LeasedUntil, &assignment.LastResultAt)
	if assignment.LeasedUntil != nil {
		value := assignment.LeasedUntil.UTC()
		assignment.LeasedUntil = &value
	}
	if assignment.LastResultAt != nil {
		value := assignment.LastResultAt.UTC()
		assignment.LastResultAt = &value
	}
	return assignment, err
}
