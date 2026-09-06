package monitoring

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

type fixedResolver struct {
	addresses []netip.Addr
}

func (resolver fixedResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return resolver.addresses, nil
}

func TestHTTPCheckerBlocksPrivateDNSResult(t *testing.T) {
	checker := NewHTTPChecker()
	checker.resolver = fixedResolver{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}
	_, err := checker.dialContext(context.Background(), "tcp", "public.example:80")
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("dialContext() error = %v, want blocked network", err)
	}
}

func TestSafeNetworkErrorHidesInternalDetails(t *testing.T) {
	message := safeNetworkError(context.DeadlineExceeded)
	if message != "request timed out" {
		t.Fatalf("safeNetworkError() = %q", message)
	}
}
