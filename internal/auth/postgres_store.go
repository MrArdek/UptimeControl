package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const uniqueViolationCode = "23505"
const usersEmailUniqueConstraint = "users_email_unique_active"

// PostgresStore persists users and sessions in PostgreSQL.
type PostgresStore struct {
	database *pgxpool.Pool
}

func NewPostgresStore(database *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{database: database}
}

func (store *PostgresStore) CreateUserWithSession(
	ctx context.Context,
	user User,
	passwordHash string,
	session Session,
) error {
	transaction, err := store.database.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin registration transaction: %w", err)
	}
	defer func() { _ = transaction.Rollback(ctx) }()

	// A self-hosted instance has one bootstrap owner in the MVP. The table lock
	// prevents two simultaneous first registrations from creating two owners.
	if _, err := transaction.Exec(ctx, "LOCK TABLE users IN EXCLUSIVE MODE"); err != nil {
		return fmt.Errorf("lock users for owner bootstrap: %w", err)
	}
	var ownerExists bool
	if err := transaction.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM users WHERE deleted_at IS NULL)
	`).Scan(&ownerExists); err != nil {
		return fmt.Errorf("check existing owner: %w", err)
	}
	if ownerExists {
		return ErrRegistrationClosed
	}

	_, err = transaction.Exec(ctx, `
		INSERT INTO users (id, email, password_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $4)
	`, user.ID, user.Email, passwordHash, user.CreatedAt)
	if err != nil {
		var postgresError *pgconn.PgError
		if errors.As(err, &postgresError) &&
			postgresError.Code == uniqueViolationCode &&
			postgresError.ConstraintName == usersEmailUniqueConstraint {
			return ErrEmailTaken
		}

		return fmt.Errorf("insert user: %w", err)
	}

	if err := createSession(ctx, transaction, session); err != nil {
		return err
	}

	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit registration: %w", err)
	}

	return nil
}

func (store *PostgresStore) UserByEmail(ctx context.Context, email string) (User, string, error) {
	var user User
	var passwordHash string
	err := store.database.QueryRow(ctx, `
		SELECT id, email, created_at, password_hash
		FROM users
		WHERE lower(email) = lower($1) AND deleted_at IS NULL
	`, email).Scan(&user.ID, &user.Email, &user.CreatedAt, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", fmt.Errorf("select user by email: %w", err)
	}
	user.CreatedAt = user.CreatedAt.UTC()

	return user, passwordHash, nil
}

func (store *PostgresStore) CreateSession(ctx context.Context, session Session) error {
	if err := createSession(ctx, store.database, session); err != nil {
		return err
	}

	return nil
}

func (store *PostgresStore) UserBySessionHash(ctx context.Context, tokenHash []byte) (User, error) {
	var user User
	err := store.database.QueryRow(ctx, `
		SELECT users.id, users.email, users.created_at
		FROM sessions
		JOIN users ON users.id = sessions.user_id
		WHERE sessions.token_hash = $1
		  AND sessions.revoked_at IS NULL
		  AND sessions.expires_at > now()
		  AND users.deleted_at IS NULL
	`, tokenHash).Scan(&user.ID, &user.Email, &user.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("select user by session: %w", err)
	}
	user.CreatedAt = user.CreatedAt.UTC()

	return user, nil
}

func (store *PostgresStore) RevokeSession(ctx context.Context, tokenHash []byte) error {
	commandTag, err := store.database.Exec(ctx, `
		UPDATE sessions
		SET revoked_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > now()
	`, tokenHash)
	if err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	if commandTag.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

type sessionExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func createSession(ctx context.Context, executor sessionExecutor, session Session) error {
	_, err := executor.Exec(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at)
		VALUES ($1, $2, $3, $4)
	`, session.ID, session.UserID, session.TokenHash, session.ExpiresAt)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}

	return nil
}
