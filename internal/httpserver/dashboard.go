package httpserver

import (
	"embed"
	"mime"
	"net/http"
	"path"
	"strings"
)

//go:embed static/dist
var dashboardFiles embed.FS

func registerDashboard(mux *http.ServeMux, basePath string) {
	dashboardPath := routePath(basePath, "/")
	handler := func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet && request.Method != http.MethodHead {
			methodNotAllowed(response, http.MethodGet+", "+http.MethodHead)
			return
		}
		relative := strings.TrimPrefix(request.URL.Path, basePath)
		if relative == request.URL.Path && basePath != "" {
			http.NotFound(response, request)
			return
		}
		if strings.HasPrefix(relative, "/api/") {
			http.NotFound(response, request)
			return
		}
		if strings.HasPrefix(relative, "/assets/") {
			serveDashboardAsset(response, request, relative)
			return
		}
		if !dashboardRoute(relative) {
			http.NotFound(response, request)
			return
		}
		content, err := dashboardFiles.ReadFile("static/dist/index.html")
		if err != nil {
			writeError(response, http.StatusInternalServerError, "internal_error", "dashboard could not be loaded")
			return
		}
		content = []byte(strings.NewReplacer(
			"__BASE_PATH__", basePath,
			"__BASE_HREF__", basePath+"/",
		).Replace(string(content)))
		setDashboardSecurityHeaders(response)
		response.Header().Set("Content-Type", "text/html; charset=utf-8")
		response.Header().Set("Cache-Control", "no-store")
		if request.Method == http.MethodGet {
			_, _ = response.Write(content)
		}
	}

	if basePath != "" {
		mux.HandleFunc(basePath, func(response http.ResponseWriter, request *http.Request) {
			if request.URL.Path != basePath {
				http.NotFound(response, request)
				return
			}
			http.Redirect(response, request, dashboardPath, http.StatusPermanentRedirect)
		})
	}
	mux.HandleFunc(dashboardPath, handler)
}

func dashboardRoute(relative string) bool {
	if relative == "/" || relative == "/projects" || relative == "/settings" ||
		relative == "/analytics" || relative == "/events" || relative == "/properties" || relative == "/servers" {
		return true
	}
	return strings.HasPrefix(relative, "/projects/")
}

func serveDashboardAsset(response http.ResponseWriter, request *http.Request, relative string) {
	cleaned := path.Clean(relative)
	if cleaned != relative || !strings.HasPrefix(cleaned, "/assets/") {
		http.NotFound(response, request)
		return
	}
	content, err := dashboardFiles.ReadFile("static/dist" + cleaned)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	setDashboardSecurityHeaders(response)
	response.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	response.Header().Set("Content-Type", mime.TypeByExtension(path.Ext(cleaned)))
	if request.Method == http.MethodGet {
		_, _ = response.Write(content)
	}
}

func setDashboardSecurityHeaders(response http.ResponseWriter) {
	response.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'self'; frame-ancestors 'none'; form-action 'self'")
	response.Header().Set("X-Content-Type-Options", "nosniff")
	response.Header().Set("Referrer-Policy", "no-referrer")
}
