package main

import "testing"

func TestLoadConfigRequiresHTTPSByDefault(t *testing.T) {
	t.Setenv("CHECK_NODE_BACKEND_URL", "http://example.com/uptimec")
	t.Setenv("CHECK_NODE_SECRET", "0123456789abcdefghijklmnopqrstuvwxyz")
	t.Setenv("CHECK_NODE_REGION", "eu-central")
	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig() accepted insecure backend")
	}

	t.Setenv("CHECK_NODE_BACKEND_URL", "https://example.com/uptimec")
	config, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if config.Region != "eu-central" || config.HealthAddr != "127.0.0.1:8090" {
		t.Fatalf("config = %+v", config)
	}
}

func TestLoadConfigAllowsExplicitLocalHTTP(t *testing.T) {
	t.Setenv("CHECK_NODE_BACKEND_URL", "http://127.0.0.1:8080")
	t.Setenv("CHECK_NODE_SECRET", "0123456789abcdefghijklmnopqrstuvwxyz")
	t.Setenv("CHECK_NODE_REGION", "eu-central")
	t.Setenv("CHECK_NODE_ALLOW_INSECURE_HTTP", "true")
	if _, err := loadConfig(); err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
}

func TestLoadConfigRejectsInvalidCredentials(t *testing.T) {
	t.Setenv("CHECK_NODE_BACKEND_URL", "https://example.com/uptimec")
	t.Setenv("CHECK_NODE_SECRET", "short")
	t.Setenv("CHECK_NODE_REGION", "eu-central")
	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig() accepted a short node secret")
	}

	t.Setenv("CHECK_NODE_SECRET", "0123456789abcdefghijklmnopqrstuvwxyz")
	t.Setenv("CHECK_NODE_REGION", "Europe Central")
	if _, err := loadConfig(); err == nil {
		t.Fatal("loadConfig() accepted an invalid region")
	}
}
