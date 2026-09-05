package httpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
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

// Options controls security-sensitive HTTP behavior.
type Options struct {
	AllowedOrigin string
	CookieSecure  bool
}

// New creates the HTTP server with conservative timeouts and application routes.
func New(address string, database databasePinger, authentication authService, options Options) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           newHandler(database, authentication, options),
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

func newHandler(database databasePinger, authentication authService, options Options) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler)
	mux.HandleFunc("/ready", readyHandler(database))

	authenticationHandlers := newAuthHandlers(authentication, options)
	mux.HandleFunc("/api/v1/auth/register", authenticationHandlers.register)
	mux.HandleFunc("/api/v1/auth/login", authenticationHandlers.login)
	mux.HandleFunc("/api/v1/auth/logout", authenticationHandlers.logout)
	mux.HandleFunc("/api/v1/auth/me", authenticationHandlers.me)

	return mux
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
