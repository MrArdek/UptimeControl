package migrations

import (
	"context"
	"os"
	"testing"

	"github.com/MrArdek/UptimeControl/internal/identity"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProjectsMigrationPreservesExistingSiteHistory(t *testing.T) {
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

	var existingTable *string
	if err := database.QueryRow(ctx, "SELECT to_regclass('public.users')::text").Scan(&existingTable); err != nil {
		t.Fatalf("check empty database: %v", err)
	}
	if existingTable != nil {
		t.Fatal("TEST_DATABASE_URL must point to an empty disposable database")
	}

	for _, name := range []string{"000001_initial_schema.up.sql", "000002_unique_active_site_url.up.sql"} {
		migrationSQL, err := migrationFiles.ReadFile("sql/" + name)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := database.Exec(ctx, string(migrationSQL)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}
	if _, err := database.Exec(ctx, `
		CREATE TABLE schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		t.Fatalf("create migration registry: %v", err)
	}
	for _, name := range []string{"000001_initial_schema.up.sql", "000002_unique_active_site_url.up.sql"} {
		if _, err := database.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", name); err != nil {
			t.Fatalf("record migration %s: %v", name, err)
		}
	}

	userID, _ := identity.NewUUID()
	siteID, _ := identity.NewUUID()
	incidentID, _ := identity.NewUUID()
	if _, err := database.Exec(ctx, `
		INSERT INTO users (id, email, password_hash) VALUES ($1, 'migration@example.com', 'test-hash')
	`, userID); err != nil {
		t.Fatalf("insert legacy user: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO sites (id, user_id, name, url) VALUES ($1, $2, 'Legacy API', 'https://example.com/health')
	`, siteID, userID); err != nil {
		t.Fatalf("insert legacy site: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO uptime_checks (site_id, available, status_code, response_time_ms)
		VALUES ($1, false, 503, 1200)
	`, siteID); err != nil {
		t.Fatalf("insert legacy check: %v", err)
	}
	if _, err := database.Exec(ctx, `
		INSERT INTO incidents (id, site_id, started_at, cause)
		VALUES ($1, $2, now(), 'legacy outage')
	`, incidentID, siteID); err != nil {
		t.Fatalf("insert legacy incident: %v", err)
	}

	if err := Up(ctx, database); err != nil {
		t.Fatalf("Up() returned an error: %v", err)
	}

	var projectCount, monitorCount, checkCount, incidentCount int
	if err := database.QueryRow(ctx, "SELECT count(*) FROM projects WHERE id = $1", siteID).Scan(&projectCount); err != nil {
		t.Fatalf("count migrated project: %v", err)
	}
	if err := database.QueryRow(ctx, "SELECT count(*) FROM monitors WHERE id = $1 AND project_id = $1", siteID).Scan(&monitorCount); err != nil {
		t.Fatalf("count migrated monitor: %v", err)
	}
	if err := database.QueryRow(ctx, "SELECT count(*) FROM uptime_checks WHERE monitor_id = $1", siteID).Scan(&checkCount); err != nil {
		t.Fatalf("count migrated checks: %v", err)
	}
	if err := database.QueryRow(ctx, "SELECT count(*) FROM incidents WHERE monitor_id = $1", siteID).Scan(&incidentCount); err != nil {
		t.Fatalf("count migrated incidents: %v", err)
	}
	if projectCount != 1 || monitorCount != 1 || checkCount != 1 || incidentCount != 1 {
		t.Fatalf("migration lost data: projects=%d monitors=%d checks=%d incidents=%d", projectCount, monitorCount, checkCount, incidentCount)
	}
}
