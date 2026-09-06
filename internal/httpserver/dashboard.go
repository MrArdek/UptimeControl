package httpserver

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed static/index.html
var dashboardFiles embed.FS

func registerDashboard(mux *http.ServeMux, basePath string) {
	dashboardPath := routePath(basePath, "/")
	handler := func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != dashboardPath {
			http.NotFound(response, request)
			return
		}
		if request.Method != http.MethodGet {
			methodNotAllowed(response, http.MethodGet)
			return
		}
		content, err := dashboardFiles.ReadFile("static/index.html")
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error", "dashboard could not be loaded")
			return
		}
		content = []byte(strings.ReplaceAll(string(content), "__BASE_PATH__", basePath))
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; connect-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = response.Write(content)
	}

	if basePath != "" {
		mux.HandleFunc(basePath, func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, dashboardPath, http.StatusPermanentRedirect)
		})
	}
	mux.HandleFunc(dashboardPath, handler)
}
