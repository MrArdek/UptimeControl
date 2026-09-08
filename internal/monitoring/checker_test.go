package monitoring

import (
	"context"
	"net"
	"net/netip"
	"strings"
	"testing"
)

type fixedDialer struct {
	address string
}

func (dialer *fixedDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	dialer.address = address
	client, server := net.Pipe()
	_ = server.Close()
	return client, nil
}

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

func TestTCPCheckerConnectsToResolvedPublicAddress(t *testing.T) {
	checker := NewHTTPChecker()
	checker.resolver = fixedResolver{addresses: []netip.Addr{netip.MustParseAddr("203.0.113.10")}}
	dialer := &fixedDialer{}
	checker.dialer = dialer
	result := checker.Check(context.Background(), DueMonitor{
		Type:           "tcp",
		Target:         "service.example:5432",
		TimeoutSeconds: 2,
	})
	if !result.Available || result.Error != nil {
		t.Fatalf("TCP result = %#v, want available", result)
	}
	if result.StatusCode != nil || result.ResponseTimeMS == nil {
		t.Fatalf("TCP result has invalid HTTP/timing fields: %#v", result)
	}
	if dialer.address != "203.0.113.10:5432" {
		t.Fatalf("dialed address = %q, want resolved public target", dialer.address)
	}
}
