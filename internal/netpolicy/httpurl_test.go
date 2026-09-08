package netpolicy

import (
	"net/netip"
	"testing"
)

func TestNormalizeHTTPURL(t *testing.T) {
	normalized, err := NormalizeHTTPURL(" HTTPS://Example.COM/status?full=true ")
	if err != nil {
		t.Fatalf("NormalizeHTTPURL() returned an error: %v", err)
	}
	if normalized != "https://example.com/status?full=true" {
		t.Fatalf("normalized URL = %q", normalized)
	}
}

func TestNormalizeHTTPURLRejectsPrivateTargets(t *testing.T) {
	for _, target := range []string{
		"http://localhost/",
		"http://127.0.0.1/",
		"http://10.0.0.1/",
		"http://metadata.internal/",
		"file:///etc/passwd",
		"https://user:password@example.com/",
	} {
		t.Run(target, func(t *testing.T) {
			if _, err := NormalizeHTTPURL(target); err == nil {
				t.Fatalf("NormalizeHTTPURL(%q) returned no error", target)
			}
		})
	}
}

func TestAddressAllowed(t *testing.T) {
	tests := map[string]bool{
		"1.1.1.1":      true,
		"2606:4700::1": true,
		"127.0.0.1":    false,
		"10.0.0.1":     false,
		"169.254.1.1":  false,
		"100.64.0.1":   false,
		"::1":          false,
	}

	for rawAddress, wantAllowed := range tests {
		if allowed := AddressAllowed(netip.MustParseAddr(rawAddress)); allowed != wantAllowed {
			t.Errorf("AddressAllowed(%s) = %v, want %v", rawAddress, allowed, wantAllowed)
		}
	}
}

func TestNormalizeTCPAddress(t *testing.T) {
	tests := map[string]string{
		" Example.COM:443 ": "example.com:443",
		"[2606:4700::1]:53": "[2606:4700::1]:53",
	}
	for input, want := range tests {
		got, err := NormalizeTCPAddress(input)
		if err != nil {
			t.Fatalf("NormalizeTCPAddress(%q): %v", input, err)
		}
		if got != want {
			t.Fatalf("NormalizeTCPAddress(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNormalizeTCPAddressRejectsUnsafeTargets(t *testing.T) {
	for _, target := range []string{
		"localhost:5432",
		"127.0.0.1:22",
		"10.0.0.1:6379",
		"metadata.internal:80",
		"example.com",
		"https://example.com:443",
		"example.com:0",
		"example.com:65536",
	} {
		if _, err := NormalizeTCPAddress(target); err == nil {
			t.Fatalf("NormalizeTCPAddress(%q) returned no error", target)
		}
	}
}
