package projects

import (
	"context"
	"testing"
)

type memoryStore struct {
	createdProject Project
	createdMonitor Monitor
}

func (store *memoryStore) List(context.Context, string) ([]Project, error) { return nil, nil }
func (store *memoryStore) ByID(context.Context, string, string) (Project, error) {
	return Project{}, nil
}
func (store *memoryStore) Create(_ context.Context, _ string, project Project, monitor Monitor) error {
	store.createdProject = project
	store.createdMonitor = monitor
	return nil
}
func (store *memoryStore) UpdateProject(context.Context, string, string, UpdateProjectInput) (Project, error) {
	return Project{}, nil
}
func (store *memoryStore) SoftDeleteProject(context.Context, string, string) error { return nil }
func (store *memoryStore) AddMonitor(context.Context, string, string, Monitor) (Monitor, error) {
	return Monitor{}, nil
}
func (store *memoryStore) UpdateMonitor(context.Context, string, string, string, UpdateMonitorInput) (Monitor, error) {
	return Monitor{}, nil
}
func (store *memoryStore) SoftDeleteMonitor(context.Context, string, string, string) error {
	return nil
}

func TestCreateHTTPProject(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store)
	project, err := service.Create(context.Background(), "owner-id", CreateProjectInput{
		Name: "  Public API  ",
		Monitor: CreateMonitorInput{
			Type: "http",
			Name: "Health",
			URL:  " HTTPS://Example.COM/health ",
		},
	})
	if err != nil {
		t.Fatalf("Create() returned an error: %v", err)
	}
	if project.Name != "Public API" || project.Monitors[0].URL != "https://example.com/health" {
		t.Fatalf("project was not normalized: %#v", project)
	}
	if project.Monitors[0].Type != "http" || project.Monitors[0].CheckIntervalSeconds != 60 {
		t.Fatalf("HTTP monitor defaults are wrong: %#v", project.Monitors[0])
	}
}

func TestCreateHeartbeatProjectReturnsOnlyHashedSecretForStorage(t *testing.T) {
	store := &memoryStore{}
	service := NewService(store)
	project, err := service.Create(context.Background(), "owner-id", CreateProjectInput{
		Name: "Telegram bot",
		Monitor: CreateMonitorInput{
			Type: "heartbeat",
			Name: "Worker loop",
		},
	})
	if err != nil {
		t.Fatalf("Create() returned an error: %v", err)
	}
	monitor := project.Monitors[0]
	if len(monitor.HeartbeatToken) < 40 || len(monitor.HeartbeatTokenHash) != 32 {
		t.Fatalf("heartbeat secret is invalid: token=%d hash=%d", len(monitor.HeartbeatToken), len(monitor.HeartbeatTokenHash))
	}
	if monitor.URL != "" || monitor.Type != "heartbeat" {
		t.Fatalf("heartbeat monitor target is invalid: %#v", monitor)
	}
	if store.createdMonitor.HeartbeatToken != monitor.HeartbeatToken {
		t.Fatal("created monitor did not reach the store")
	}
}

func TestCreateRejectsUnsupportedMonitorType(t *testing.T) {
	service := NewService(&memoryStore{})
	_, err := service.Create(context.Background(), "owner-id", CreateProjectInput{
		Name:    "Bot",
		Monitor: CreateMonitorInput{Type: "process", Name: "Process"},
	})
	if err != ErrInvalidType {
		t.Fatalf("Create() error = %v, want ErrInvalidType", err)
	}
}
