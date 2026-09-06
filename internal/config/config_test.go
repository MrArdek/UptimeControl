package config

import "testing"

func TestLoadUsesDefaultHTTPAddress(t *testing.T) {
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("DATABASE_URL", "postgres://localhost/uptime_control")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}

	if cfg.HTTPAddress != ":8080" {
		t.Fatalf("HTTPAddress = %q, want %q", cfg.HTTPAddress, ":8080")
	}

	if cfg.PublicOrigin != "http://localhost:8080" {
		t.Fatalf("PublicOrigin = %q, want default origin", cfg.PublicOrigin)
	}

	if cfg.BasePath != "" {
		t.Fatalf("BasePath = %q, want root", cfg.BasePath)
	}

	if !cfg.SessionCookieSecure {
		t.Fatal("SessionCookieSecure = false, want secure default")
	}
}

func TestLoadAcceptsValidHTTPAddress(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	t.Setenv("DATABASE_URL", "postgres://localhost/uptime_control")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}

	if cfg.HTTPAddress != "127.0.0.1:9090" {
		t.Fatalf("HTTPAddress = %q, want %q", cfg.HTTPAddress, "127.0.0.1:9090")
	}
}

func TestLoadRejectsInvalidHTTPAddress(t *testing.T) {
	t.Setenv("HTTP_ADDR", "localhost")
	t.Setenv("DATABASE_URL", "postgres://localhost/uptime_control")

	if _, err := Load(); err == nil {
		t.Fatal("Load() returned no error for an invalid address")
	}
}

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() returned no error without DATABASE_URL")
	}
}

func TestLoadRejectsNonPostgreSQLURL(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("DATABASE_URL", "http://localhost/uptime_control")

	if _, err := Load(); err == nil {
		t.Fatal("Load() returned no error for a non-PostgreSQL URL")
	}
}

func TestLoadAcceptsLocalCookieConfiguration(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("DATABASE_URL", "postgres://localhost/uptime_control")
	t.Setenv("PUBLIC_ORIGIN", "http://127.0.0.1:8080/")
	t.Setenv("SESSION_COOKIE_SECURE", "false")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}

	if cfg.PublicOrigin != "http://127.0.0.1:8080" {
		t.Fatalf("PublicOrigin = %q, want normalized origin", cfg.PublicOrigin)
	}

	if cfg.SessionCookieSecure {
		t.Fatal("SessionCookieSecure = true, want false for local HTTP")
	}
}

func TestLoadRejectsPublicOriginWithPath(t *testing.T) {
	t.Setenv("HTTP_ADDR", ":8080")
	t.Setenv("DATABASE_URL", "postgres://localhost/uptime_control")
	t.Setenv("PUBLIC_ORIGIN", "https://example.com/app")

	if _, err := Load(); err == nil {
		t.Fatal("Load() returned no error for PUBLIC_ORIGIN with a path")
	}
}

func TestLoadAcceptsAndNormalizesBasePath(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/uptime_control")
	t.Setenv("BASE_PATH", "/uptimec/")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned an error: %v", err)
	}

	if cfg.BasePath != "/uptimec" {
		t.Fatalf("BasePath = %q, want %q", cfg.BasePath, "/uptimec")
	}
}

func TestLoadRejectsInvalidBasePath(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/uptime_control")

	for _, basePath := range []string{"uptimec", "/uptimec//admin", "/../uptimec", "/uptimec?debug=true"} {
		t.Run(basePath, func(t *testing.T) {
			t.Setenv("BASE_PATH", basePath)
			if _, err := Load(); err == nil {
				t.Fatalf("Load() returned no error for BASE_PATH %q", basePath)
			}
		})
	}
}

func TestLoadRequiresCompleteTelegramConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/uptime_control")
	t.Setenv("TELEGRAM_BOT_TOKEN", "secret-token")
	t.Setenv("TELEGRAM_CHAT_ID", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() returned no error for incomplete Telegram configuration")
	}
}
