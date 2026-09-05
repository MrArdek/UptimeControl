package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestOpenDoesNotExposePasswordInConnectionError(t *testing.T) {
	const secretMarker = "secret-marker-must-not-appear"
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := Open(ctx, "postgresql://uptime:"+secretMarker+"@127.0.0.1:1/uptime_control")
	if err == nil {
		t.Fatal("Open() returned no error for an unavailable database")
	}

	if strings.Contains(err.Error(), secretMarker) {
		t.Fatalf("Open() exposed the database password: %v", err)
	}
}

func TestOpenSanitizesParserErrors(t *testing.T) {
	const secretMarker = "secret-marker-must-not-appear"

	_, err := Open(context.Background(), "postgresql://uptime:"+secretMarker+"@%")
	if err == nil {
		t.Fatal("Open() returned no error for an invalid database URL")
	}

	if strings.Contains(err.Error(), secretMarker) {
		t.Fatalf("Open() exposed the database password: %v", err)
	}
}
