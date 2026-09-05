package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryUser struct {
	user         User
	passwordHash string
}

type memoryStore struct {
	mutex    sync.Mutex
	users    map[string]memoryUser
	sessions map[string]Session
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		users:    make(map[string]memoryUser),
		sessions: make(map[string]Session),
	}
}

func (store *memoryStore) CreateUserWithSession(_ context.Context, user User, passwordHash string, session Session) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	if _, exists := store.users[user.Email]; exists {
		return ErrEmailTaken
	}

	store.users[user.Email] = memoryUser{user: user, passwordHash: passwordHash}
	store.sessions[string(session.TokenHash)] = session
	return nil
}

func (store *memoryStore) UserByEmail(_ context.Context, email string) (User, string, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	entry, exists := store.users[email]
	if !exists {
		return User{}, "", ErrNotFound
	}

	return entry.user, entry.passwordHash, nil
}

func (store *memoryStore) CreateSession(_ context.Context, session Session) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.sessions[string(session.TokenHash)] = session
	return nil
}

func (store *memoryStore) UserBySessionHash(_ context.Context, tokenHash []byte) (User, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	session, exists := store.sessions[string(tokenHash)]
	if !exists || session.ExpiresAt.Before(time.Now()) {
		return User{}, ErrNotFound
	}

	for _, entry := range store.users {
		if entry.user.ID == session.UserID {
			return entry.user, nil
		}
	}

	return User{}, ErrNotFound
}

func (store *memoryStore) RevokeSession(_ context.Context, tokenHash []byte) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	key := string(tokenHash)
	if _, exists := store.sessions[key]; !exists {
		return ErrNotFound
	}

	delete(store.sessions, key)
	return nil
}

func TestRegisterAuthenticateAndLogout(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store)
	service.now = func() time.Time {
		return time.Date(2026, 9, 5, 12, 0, 0, 0, time.UTC)
	}

	result, err := service.Register(context.Background(), "  USER@Example.COM ", "correct horse battery staple")
	if err != nil {
		t.Fatalf("Register() returned an error: %v", err)
	}

	if result.User.Email != "user@example.com" {
		t.Fatalf("registered email = %q, want normalized email", result.User.Email)
	}
	if result.Token == "" {
		t.Fatal("registered session token is empty")
	}

	currentUser, err := service.CurrentUser(context.Background(), result.Token)
	if err != nil {
		t.Fatalf("CurrentUser() returned an error: %v", err)
	}
	if currentUser.ID != result.User.ID {
		t.Fatalf("current user ID = %q, want %q", currentUser.ID, result.User.ID)
	}

	if err := service.Logout(context.Background(), result.Token); err != nil {
		t.Fatalf("Logout() returned an error: %v", err)
	}

	if _, err := service.CurrentUser(context.Background(), result.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("CurrentUser() error after logout = %v, want unauthorized", err)
	}
}

func TestLogin(t *testing.T) {
	store := newMemoryStore()
	service := NewService(store)

	if _, err := service.Register(context.Background(), "user@example.com", "correct horse battery staple"); err != nil {
		t.Fatalf("Register() returned an error: %v", err)
	}

	result, err := service.Login(context.Background(), "USER@example.com", "correct horse battery staple")
	if err != nil {
		t.Fatalf("Login() returned an error: %v", err)
	}
	if result.Token == "" {
		t.Fatal("login session token is empty")
	}

	if _, err := service.Login(context.Background(), "user@example.com", "incorrect password value"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want invalid credentials", err)
	}

	if _, err := service.Login(context.Background(), "missing@example.com", "incorrect password value"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() missing user error = %v, want invalid credentials", err)
	}
}

func TestRegisterRejectsInvalidInputAndDuplicateEmail(t *testing.T) {
	service := NewService(newMemoryStore())

	if _, err := service.Register(context.Background(), "not-an-email", "correct horse battery staple"); !errors.Is(err, ErrInvalidEmail) {
		t.Fatalf("Register() email error = %v, want invalid email", err)
	}

	if _, err := service.Register(context.Background(), "user@example.com", "short"); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("Register() password error = %v, want invalid password", err)
	}

	if _, err := service.Register(context.Background(), "user@example.com", "correct horse battery staple"); err != nil {
		t.Fatalf("Register() returned an error: %v", err)
	}

	if _, err := service.Register(context.Background(), "USER@example.com", "another valid password"); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("Register() duplicate error = %v, want email taken", err)
	}
}
