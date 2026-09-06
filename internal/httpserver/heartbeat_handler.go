package httpserver

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/MrArdek/UptimeControl/internal/monitoring"
)

type heartbeatService interface {
	RecordHeartbeat(context.Context, string) error
}

func heartbeatHandler(service heartbeatService, basePath string) http.HandlerFunc {
	prefix := routePath(basePath, "/api/v1/heartbeat/")
	limiter := newFixedWindowLimiter(120, time.Minute)
	return func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			methodNotAllowed(response, http.MethodPost)
			return
		}
		if !limiter.Allow(clientIP(request)) {
			rateLimited(response)
			return
		}
		token := strings.TrimPrefix(request.URL.Path, prefix)
		if !validHeartbeatToken(token) {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		if err := service.RecordHeartbeat(request.Context(), token); err != nil {
			if errors.Is(err, monitoring.ErrNotFound) {
				// Do not reveal whether a secret heartbeat token exists.
				response.WriteHeader(http.StatusNoContent)
				return
			}
			writeError(response, http.StatusInternalServerError, "internal_error", "heartbeat could not be recorded")
			return
		}
		response.Header().Set("Cache-Control", "no-store")
		response.WriteHeader(http.StatusNoContent)
	}
}

func validHeartbeatToken(token string) bool {
	if len(token) != 43 || strings.Contains(token, "/") {
		return false
	}
	for _, character := range token {
		if !(character >= 'a' && character <= 'z') &&
			!(character >= 'A' && character <= 'Z') &&
			!(character >= '0' && character <= '9') &&
			character != '-' && character != '_' {
			return false
		}
	}
	return true
}
