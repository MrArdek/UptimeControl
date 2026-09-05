package sites

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maximumListedSites      = 100
	uniqueViolationCode     = "23505"
	uniqueSiteURLConstraint = "sites_user_url_unique_active"
)

type PostgresStore struct {
	database *pgxpool.Pool
}

func NewPostgresStore(database *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{database: database}
}

func (store *PostgresStore) List(ctx context.Context, userID string) ([]Site, error) {
	rows, err := store.database.Query(ctx, `
		SELECT id, name, url, check_interval_seconds, enabled, created_at, updated_at
		FROM sites
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC, id DESC
		LIMIT $2
	`, userID, maximumListedSites)
	if err != nil {
		return nil, fmt.Errorf("list sites: %w", err)
	}
	defer rows.Close()

	siteList := make([]Site, 0)
	for rows.Next() {
		var site Site
		if err := rows.Scan(
			&site.ID,
			&site.Name,
			&site.URL,
			&site.CheckIntervalSeconds,
			&site.Enabled,
			&site.CreatedAt,
			&site.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan site: %w", err)
		}
		normalizeTimes(&site)
		siteList = append(siteList, site)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sites: %w", err)
	}

	return siteList, nil
}

func (store *PostgresStore) Create(ctx context.Context, userID string, site Site) error {
	_, err := store.database.Exec(ctx, `
		INSERT INTO sites (
			id, user_id, name, url, check_interval_seconds, enabled, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		site.ID,
		userID,
		site.Name,
		site.URL,
		site.CheckIntervalSeconds,
		site.Enabled,
		site.CreatedAt,
		site.UpdatedAt,
	)
	if isDuplicateSiteURL(err) {
		return ErrAlreadyExists
	}
	if err != nil {
		return fmt.Errorf("insert site: %w", err)
	}

	return nil
}

func (store *PostgresStore) ByID(ctx context.Context, userID, siteID string) (Site, error) {
	var site Site
	err := store.database.QueryRow(ctx, `
		SELECT id, name, url, check_interval_seconds, enabled, created_at, updated_at
		FROM sites
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, siteID, userID).Scan(
		&site.ID,
		&site.Name,
		&site.URL,
		&site.CheckIntervalSeconds,
		&site.Enabled,
		&site.CreatedAt,
		&site.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	if err != nil {
		return Site{}, fmt.Errorf("select site: %w", err)
	}

	normalizeTimes(&site)
	return site, nil
}

func (store *PostgresStore) Update(
	ctx context.Context,
	userID string,
	siteID string,
	input UpdateInput,
) (Site, error) {
	var site Site
	err := store.database.QueryRow(ctx, `
		UPDATE sites
		SET name = COALESCE($3, name),
		    url = COALESCE($4, url),
		    check_interval_seconds = COALESCE($5, check_interval_seconds),
		    enabled = COALESCE($6, enabled),
		    updated_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
		RETURNING id, name, url, check_interval_seconds, enabled, created_at, updated_at
	`,
		siteID,
		userID,
		optionalString(input.Name),
		optionalString(input.URL),
		optionalInt(input.CheckIntervalSeconds),
		optionalBool(input.Enabled),
	).Scan(
		&site.ID,
		&site.Name,
		&site.URL,
		&site.CheckIntervalSeconds,
		&site.Enabled,
		&site.CreatedAt,
		&site.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	if isDuplicateSiteURL(err) {
		return Site{}, ErrAlreadyExists
	}
	if err != nil {
		return Site{}, fmt.Errorf("update site: %w", err)
	}

	normalizeTimes(&site)
	return site, nil
}

func (store *PostgresStore) SoftDelete(ctx context.Context, userID, siteID string) error {
	commandTag, err := store.database.Exec(ctx, `
		UPDATE sites
		SET deleted_at = now(), enabled = false, updated_at = now()
		WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
	`, siteID, userID)
	if err != nil {
		return fmt.Errorf("soft-delete site: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

func isDuplicateSiteURL(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) &&
		postgresError.Code == uniqueViolationCode &&
		postgresError.ConstraintName == uniqueSiteURLConstraint
}

func normalizeTimes(site *Site) {
	site.CreatedAt = site.CreatedAt.UTC()
	site.UpdatedAt = site.UpdatedAt.UTC()
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
