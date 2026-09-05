package auth

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	minimumPasswordCharacters = 12
	maximumPasswordBytes      = 128
	sessionLifetime           = 7 * 24 * time.Hour
)

var (
	ErrEmailTaken         = errors.New("email is already registered")
	ErrInvalidEmail       = errors.New("invalid email")
	ErrInvalidPassword    = errors.New("password does not meet requirements")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrNotFound           = errors.New("not found")
)

// User is the public representation of an authenticated account.
type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	CreatedAt time.Time `json:"created_at"`
}

// Session is the database representation of an issued session.
type Session struct {
	ID        string
	UserID    string
	TokenHash []byte
	ExpiresAt time.Time
}

// Result contains the data needed to return a user and set a session cookie.
type Result struct {
	User      User
	Token     string
	ExpiresAt time.Time
}

// Store describes the database operations required by authentication.
type Store interface {
	CreateUserWithSession(context.Context, User, string, Session) error
	UserByEmail(context.Context, string) (User, string, error)
	CreateSession(context.Context, Session) error
	UserBySessionHash(context.Context, []byte) (User, error)
	RevokeSession(context.Context, []byte) error
}

// Service implements registration, login and session authentication.
type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{
		store: store,
		now:   time.Now,
	}
}

func (service *Service) Register(ctx context.Context, email, password string) (Result, error) {
	normalizedEmail, err := normalizeEmail(email)
	if err != nil {
		return Result{}, err
	}

	if err := validatePassword(password); err != nil {
		return Result{}, err
	}

	passwordHash, err := hashPassword(password)
	if err != nil {
		return Result{}, fmt.Errorf("hash password: %w", err)
	}

	result, session, err := service.newResult(normalizedEmail)
	if err != nil {
		return Result{}, err
	}

	if err := service.store.CreateUserWithSession(ctx, result.User, passwordHash, session); err != nil {
		return Result{}, err
	}

	return result, nil
}

func (service *Service) Login(ctx context.Context, email, password string) (Result, error) {
	normalizedEmail, err := normalizeEmail(email)
	if err != nil || validatePasswordLength(password) != nil {
		return Result{}, ErrInvalidCredentials
	}

	user, passwordHash, err := service.store.UserByEmail(ctx, normalizedEmail)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// Perform equivalent expensive work to reduce email-enumeration timing leaks.
			_, _ = hashPassword(password)
			return Result{}, ErrInvalidCredentials
		}

		return Result{}, err
	}

	passwordMatches, err := verifyPassword(password, passwordHash)
	if err != nil {
		return Result{}, fmt.Errorf("verify password: %w", err)
	}
	if !passwordMatches {
		return Result{}, ErrInvalidCredentials
	}

	token, tokenHash, err := newSessionToken()
	if err != nil {
		return Result{}, fmt.Errorf("generate session token: %w", err)
	}

	sessionID, err := newUUID()
	if err != nil {
		return Result{}, fmt.Errorf("generate session ID: %w", err)
	}

	expiresAt := service.now().UTC().Add(sessionLifetime)
	if err := service.store.CreateSession(ctx, Session{
		ID:        sessionID,
		UserID:    user.ID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}); err != nil {
		return Result{}, err
	}

	return Result{User: user, Token: token, ExpiresAt: expiresAt}, nil
}

func (service *Service) CurrentUser(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrUnauthorized
	}

	user, err := service.store.UserBySessionHash(ctx, hashSessionToken(token))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return User{}, ErrUnauthorized
		}

		return User{}, err
	}

	return user, nil
}

func (service *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return ErrUnauthorized
	}

	if err := service.store.RevokeSession(ctx, hashSessionToken(token)); err != nil {
		if errors.Is(err, ErrNotFound) {
			return ErrUnauthorized
		}

		return err
	}

	return nil
}

func (service *Service) newResult(email string) (Result, Session, error) {
	userID, err := newUUID()
	if err != nil {
		return Result{}, Session{}, fmt.Errorf("generate user ID: %w", err)
	}

	sessionID, err := newUUID()
	if err != nil {
		return Result{}, Session{}, fmt.Errorf("generate session ID: %w", err)
	}

	token, tokenHash, err := newSessionToken()
	if err != nil {
		return Result{}, Session{}, fmt.Errorf("generate session token: %w", err)
	}

	now := service.now().UTC()
	expiresAt := now.Add(sessionLifetime)
	user := User{ID: userID, Email: email, CreatedAt: now}
	session := Session{
		ID:        sessionID,
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt,
	}

	return Result{User: user, Token: token, ExpiresAt: expiresAt}, session, nil
}

func normalizeEmail(email string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(email))
	if len(normalized) == 0 || len(normalized) > 254 {
		return "", ErrInvalidEmail
	}

	parsed, err := mail.ParseAddress(normalized)
	if err != nil || parsed.Address != normalized || !strings.Contains(normalized, "@") {
		return "", ErrInvalidEmail
	}

	return normalized, nil
}

func validatePassword(password string) error {
	if err := validatePasswordLength(password); err != nil {
		return err
	}

	if strings.TrimSpace(password) != password {
		return ErrInvalidPassword
	}

	return nil
}

func validatePasswordLength(password string) error {
	if !utf8.ValidString(password) || len(password) > maximumPasswordBytes {
		return ErrInvalidPassword
	}

	if utf8.RuneCountInString(password) < minimumPasswordCharacters {
		return ErrInvalidPassword
	}

	return nil
}
