package monitoring

import (
	"context"
	"crypto/sha256"
	"os"
	"testing"
	"time"

	"github.com/MrArdek/UptimeControl/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresStoreRecordsHeartbeat(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	database, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	defer database.Close()

	var userID string
	if err := database.QueryRow(ctx, "SELECT id FROM users WHERE deleted_at IS NULL LIMIT 1").Scan(&userID); err != nil {
		t.Fatalf("select test owner: %v", err)
	}
	projectID, _ := identity.NewUUID()
	monitorID, _ := identity.NewUUID()
	rawToken := "integration-heartbeat-token-" + monitorID
	tokenHash := sha256.Sum256([]byte(rawToken))
	if _, err := database.Exec(ctx, `
		INSERT INTO projects (id, user_id, name) VALUES ($1, $2, 'Heartbeat integration test')
	`, projectID, userID); err != nil {
		t.Fatalf("insert heartbeat project fixture: %v", err)
	}
	t.Cleanup(func() {
		_, _ = database.Exec(ctx, "DELETE FROM uptime_checks WHERE monitor_id = $1", monitorID)
		_, _ = database.Exec(ctx, "DELETE FROM incidents WHERE monitor_id = $1", monitorID)
		_, _ = database.Exec(ctx, "DELETE FROM monitors WHERE id = $1", monitorID)
		_, _ = database.Exec(ctx, "DELETE FROM projects WHERE id = $1", projectID)
	})
	if _, err := database.Exec(ctx, `
		INSERT INTO monitors (
			id, project_id, type, name, url, heartbeat_token_hash,
			check_interval_seconds, timeout_seconds, next_check_at
		) VALUES ($1, $2, 'heartbeat', 'Worker', '', $3, 30, 10, now() + interval '30 seconds')
	`, monitorID, projectID, tokenHash[:]); err != nil {
		t.Fatalf("insert heartbeat fixture: %v", err)
	}
	store := NewPostgresStore(database)
	if _, err := store.RecordHeartbeat(ctx, tokenHash[:], time.Now().UTC()); err != nil {
		t.Fatalf("RecordHeartbeat() returned an error: %v", err)
	}

	var available bool
	if err := database.QueryRow(ctx, `
		SELECT available FROM uptime_checks WHERE monitor_id = $1 ORDER BY checked_at DESC LIMIT 1
	`, monitorID).Scan(&available); err != nil {
		t.Fatalf("select recorded heartbeat: %v", err)
	}
	if !available {
		t.Fatal("recorded heartbeat is unavailable")
	}
}
