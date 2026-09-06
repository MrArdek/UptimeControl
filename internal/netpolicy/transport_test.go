package netpolicy

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestSafeDialContextBlocksLiteralPrivateAddress(t *testing.T) {
	dial := SafeDialContext(nil)
	_, err := dial(context.Background(), "tcp", "127.0.0.1:80")
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("dial error = %v, want blocked network", err)
	}
}

func TestPublicRedirectValidatorRejectsPrivateTarget(t *testing.T) {
	validator := PublicRedirectValidator(3)
	request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/latest", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if err := validator(request, nil); err == nil {
		t.Fatal("validator accepted a private redirect target")
	}
}
