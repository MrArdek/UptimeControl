package migrations

import (
	"context"
	"embed"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID int64 = 8_278_463_736_651_001

//go:embed sql/*.up.sql
var migrationFiles embed.FS

// Up applies every pending migration in filename order inside one transaction.
func Up(ctx context.Context, database *pgxpool.Pool) error {
	transaction, err := database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	if _, err := transaction.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}

	if _, err := transaction.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := migrationFiles.ReadDir("sql")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}

	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() < entries[right].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}

		version := entry.Name()
		var applied bool
		if err := transaction.QueryRow(
			ctx,
			"SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)",
			version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}

		if applied {
			continue
		}

		migrationSQL, err := migrationFiles.ReadFile("sql/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}

		if _, err := transaction.Exec(ctx, string(migrationSQL)); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}

		if _, err := transaction.Exec(
			ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			version,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", version, err)
		}
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}

	return nil
}
