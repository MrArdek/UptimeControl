package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/monitoring"
	"github.com/MrArdek/UptimeControl/internal/projects"
)

const (
	maximumLiveConnections = 50
	liveSnapshotInterval   = 5 * time.Second
	liveWriteTimeout       = 10 * time.Second
)

type liveEventSource struct {
	authentication authService
	projects       projectService
	history        historyService
	connections    chan struct{}
	now            func() time.Time
}

type liveSnapshot struct {
	Projects    projects.ProjectPage      `json:"projects"`
	Incidents   []monitoring.OpenIncident `json:"incidents"`
	GeneratedAt time.Time                 `json:"generated_at"`
}

func newLiveEventSource(authentication authService, projectManagement projectService, history historyService) *liveEventSource {
	return &liveEventSource{
		authentication: authentication,
		projects:       projectManagement,
		history:        history,
		connections:    make(chan struct{}, maximumLiveConnections),
		now:            time.Now,
	}
}

func (source *liveEventSource) serve(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(response, http.MethodGet)
		return
	}
	token, err := sessionToken(request)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return
	}
	user, err := source.authentication.CurrentUser(request.Context(), token)
	if errors.Is(err, auth.ErrUnauthorized) {
		writeError(response, http.StatusUnauthorized, "unauthorized", "authentication is required")
		return
	}
	if err != nil {
		writeError(response, http.StatusInternalServerError, "internal_error", "request could not be completed")
		return
	}
	select {
	case source.connections <- struct{}{}:
		defer func() { <-source.connections }()
	default:
		writeError(response, http.StatusTooManyRequests, "stream_limit", "too many live dashboard connections")
		return
	}
	flusher, ok := response.(http.Flusher)
	if !ok {
		writeError(response, http.StatusInternalServerError, "stream_unavailable", "live updates are unavailable")
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache, no-store")
	response.Header().Set("Connection", "keep-alive")
	response.Header().Set("X-Accel-Buffering", "no")
	if _, err := response.Write([]byte("retry: 5000\n\n")); err != nil {
		return
	}
	flusher.Flush()
	if !source.sendSnapshot(response, flusher, request.Context(), user.ID) {
		return
	}

	ticker := time.NewTicker(liveSnapshotInterval)
	defer ticker.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case <-ticker.C:
			if _, err := source.authentication.CurrentUser(request.Context(), token); errors.Is(err, auth.ErrUnauthorized) {
				source.sendEvent(response, flusher, "session-expired", "", map[string]string{"code": "unauthorized"})
				return
			} else if err != nil {
				if !source.sendEvent(response, flusher, "stream-error", "", map[string]string{"code": "snapshot_unavailable"}) {
					return
				}
				continue
			}
			if !source.sendSnapshot(response, flusher, request.Context(), user.ID) {
				return
			}
		}
	}
}

func (source *liveEventSource) sendSnapshot(response http.ResponseWriter, flusher http.Flusher, ctx context.Context, userID string) bool {
	projectPage, err := source.projects.ListPage(ctx, userID, 500, "")
	if err != nil {
		return source.sendEvent(response, flusher, "stream-error", "", map[string]string{"code": "snapshot_unavailable"})
	}
	incidents, err := source.history.OpenIncidents(ctx, userID)
	if err != nil {
		return source.sendEvent(response, flusher, "stream-error", "", map[string]string{"code": "snapshot_unavailable"})
	}
	now := source.now().UTC()
	return source.sendEvent(response, flusher, "snapshot", fmt.Sprint(now.UnixNano()), liveSnapshot{
		Projects: projectPage, Incidents: incidents, GeneratedAt: now,
	})
}

func (source *liveEventSource) sendEvent(response http.ResponseWriter, flusher http.Flusher, event, id string, value any) bool {
	data, err := json.Marshal(value)
	if err != nil {
		return false
	}
	controller := http.NewResponseController(response)
	if err := controller.SetWriteDeadline(time.Now().Add(liveWriteTimeout)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		return false
	}
	if id != "" {
		if _, err := fmt.Fprintf(response, "id: %s\n", id); err != nil {
			return false
		}
	}
	if _, err := fmt.Fprintf(response, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
