package monitoring

import (
	"context"
	"fmt"
	"time"
)

type OpenIncident struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	ProjectName string    `json:"project_name"`
	MonitorID   string    `json:"monitor_id"`
	MonitorName string    `json:"monitor_name"`
	StartedAt   time.Time `json:"started_at"`
	Cause       *string   `json:"cause"`
}

func (store *PostgresStore) OpenIncidents(ctx context.Context, userID string) ([]OpenIncident, error) {
	rows, err := store.database.Query(ctx, `
		SELECT incidents.id, projects.id, projects.name, monitors.id, monitors.name,
		       incidents.started_at, incidents.cause
		FROM incidents
		JOIN monitors ON monitors.id = incidents.monitor_id
		JOIN projects ON projects.id = monitors.project_id
		WHERE projects.user_id = $1 AND projects.deleted_at IS NULL
		  AND monitors.deleted_at IS NULL AND incidents.resolved_at IS NULL
		ORDER BY incidents.started_at DESC, incidents.id DESC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list open incidents: %w", err)
	}
	defer rows.Close()
	incidents := make([]OpenIncident, 0)
	for rows.Next() {
		var incident OpenIncident
		if err := rows.Scan(
			&incident.ID, &incident.ProjectID, &incident.ProjectName,
			&incident.MonitorID, &incident.MonitorName, &incident.StartedAt, &incident.Cause,
		); err != nil {
			return nil, fmt.Errorf("scan open incident: %w", err)
		}
		incident.StartedAt = incident.StartedAt.UTC()
		incidents = append(incidents, incident)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate open incidents: %w", err)
	}
	return incidents, nil
}
