package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/sites"
)

const shutdownTimeout = 10 * time.Second
const readinessTimeout = 2 * time.Second

type databasePinger interface {
	Ping(context.Context) error
}

type authService interface {
	Register(context.Context, string, string) (auth.Result, error)
	Login(context.Context, string, string) (auth.Result, error)
	CurrentUser(context.Context, string) (auth.User, error)
	Logout(context.Context, string) error
}

type siteService interface {
	List(context.Context, string) ([]sites.Site, error)
	Create(context.Context, string, sites.CreateInput) (sites.Site, error)
	ByID(context.Context, string, string) (sites.Site, error)
	Update(context.Context, string, string, sites.UpdateInput) (sites.Site, error)
	Delete(context.Context, string, string) error
}

// Options controls security-sensitive HTTP behavior.
type Options struct {
	AllowedOrigin string
	BasePath      string
	CookieSecure  bool
}

// New creates the HTTP server with conservative timeouts and application routes.
func New(
	address string,
	database databasePinger,
	authentication authService,
	siteManagement siteService,
	options Options,
) *http.Server {
	return newServer(address, newHandler(database, authentication, siteManagement, options))
}

// NewApplication creates the complete self-hosted application server.
func NewApplication(
	address string,
	database databasePinger,
	authentication authService,
	siteManagement siteService,
	projectManagement projectService,
	history historyService,
	heartbeats heartbeatService,
	options Options,
) *http.Server {
	return newServer(address, newApplicationHandler(
		database,
		authentication,
		siteManagement,
		projectManagement,
		history,
		heartbeats,
		options,
	))
}

func newServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
}

// ShutdownContext returns a bounded context for graceful server shutdown.
func ShutdownContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), shutdownTimeout)
}

func newHandler(
	database databasePinger,
	authentication authService,
	siteManagement siteService,
	options Options,
) http.Handler {
	return newApplicationHandler(database, authentication, siteManagement, nil, nil, nil, options)
}

func newApplicationHandler(
	database databasePinger,
	authentication authService,
	siteManagement siteService,
	projectManagement projectService,
	history historyService,
	heartbeats heartbeatService,
	options Options,
) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(routePath(options.BasePath, "/health"), healthHandler)
	mux.HandleFunc(routePath(options.BasePath, "/ready"), readyHandler(database))

	authenticationHandlers := newAuthHandlers(authentication, options)
	mux.HandleFunc(routePath(options.BasePath, "/api/v1/auth/register"), authenticationHandlers.register)
	mux.HandleFunc(routePath(options.BasePath, "/api/v1/auth/login"), authenticationHandlers.login)
	mux.HandleFunc(routePath(options.BasePath, "/api/v1/auth/logout"), authenticationHandlers.logout)
	mux.HandleFunc(routePath(options.BasePath, "/api/v1/auth/me"), authenticationHandlers.me)

	if projectManagement != nil && history != nil {
		projectHandlers := newProjectHandlers(authentication, projectManagement, history, options)
		mux.HandleFunc(routePath(options.BasePath, "/api/v1/projects"), projectHandlers.collection)
		mux.HandleFunc(routePath(options.BasePath, "/api/v1/projects/"), projectHandlers.item)
	} else {
		// Kept only for isolated compatibility tests while installations migrate to projects.
		siteHandlers := newSiteHandlers(authentication, siteManagement, options)
		mux.HandleFunc(routePath(options.BasePath, "/api/v1/sites"), siteHandlers.collection)
		mux.HandleFunc(routePath(options.BasePath, "/api/v1/sites/"), siteHandlers.item)
	}
	if heartbeats != nil {
		mux.HandleFunc(routePath(options.BasePath, "/api/v1/heartbeat/"), heartbeatHandler(heartbeats, options.BasePath))
	}
	registerDashboard(mux, options.BasePath)

	return mux
}

func routePath(basePath, endpoint string) string {
	return basePath + endpoint
}

func healthHandler(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		response.Header().Set("Allow", http.MethodGet)
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeStatus(response, http.StatusOK, "ok")
}

func readyHandler(database databasePinger) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.Header().Set("Allow", http.MethodGet)
			http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		pingContext, cancelPing := context.WithTimeout(request.Context(), readinessTimeout)
		defer cancelPing()

		if err := database.Ping(pingContext); err != nil {
			writeStatus(response, http.StatusServiceUnavailable, "unavailable")
			return
		}

		writeStatus(response, http.StatusOK, "ready")
	}
}

func writeStatus(response http.ResponseWriter, statusCode int, status string) {
	response.Header().Set("Cache-Control", "no-store")
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(statusCode)
	_ = json.NewEncoder(response).Encode(struct {
		Status string `json:"status"`
	}{Status: status})
}
