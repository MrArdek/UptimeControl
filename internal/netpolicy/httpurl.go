package netpolicy

import (
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

const maximumURLBytes = 2048

var ErrUnsafeURL = errors.New("URL must be a public HTTP or HTTPS address")

// NormalizeHTTPURL validates the stable parts of a user-provided monitoring URL.
// DNS results must still be checked immediately before every connection.
func NormalizeHTTPURL(rawURL string) (string, error) {
	normalized := strings.TrimSpace(rawURL)
	if normalized == "" || len(normalized) > maximumURLBytes {
		return "", ErrUnsafeURL
	}

	parsedURL, err := url.ParseRequestURI(normalized)
	if err != nil {
		return "", ErrUnsafeURL
	}

	parsedURL.Scheme = strings.ToLower(parsedURL.Scheme)
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", ErrUnsafeURL
	}
	if parsedURL.Hostname() == "" || parsedURL.User != nil || parsedURL.Fragment != "" {
		return "", ErrUnsafeURL
	}

	if portText := parsedURL.Port(); portText != "" {
		port, err := strconv.Atoi(portText)
		if err != nil || port < 1 || port > 65535 {
			return "", ErrUnsafeURL
		}
	}

	hostname := strings.ToLower(strings.TrimSuffix(parsedURL.Hostname(), "."))
	if !HostnameAllowed(hostname) {
		return "", ErrUnsafeURL
	}

	port := parsedURL.Port()
	if net.ParseIP(hostname) != nil && strings.Contains(hostname, ":") {
		parsedURL.Host = "[" + hostname + "]"
	} else {
		parsedURL.Host = hostname
	}
	if port != "" {
		parsedURL.Host = net.JoinHostPort(hostname, port)
	}

	return parsedURL.String(), nil
}

func HostnameAllowed(hostname string) bool {
	if strings.Contains(hostname, "%") {
		return false
	}

	if hostname == "localhost" ||
		strings.HasSuffix(hostname, ".localhost") ||
		strings.HasSuffix(hostname, ".local") ||
		strings.HasSuffix(hostname, ".internal") ||
		strings.HasSuffix(hostname, ".invalid") ||
		strings.HasSuffix(hostname, ".test") {
		return false
	}

	address, err := netip.ParseAddr(hostname)
	if err != nil {
		return strings.Contains(hostname, ".")
	}

	return AddressAllowed(address)
}

// AddressAllowed rejects networks that a public monitor must never reach.
func AddressAllowed(address netip.Addr) bool {
	address = address.Unmap()
	if !address.IsValid() ||
		address.IsLoopback() ||
		address.IsPrivate() ||
		address.IsLinkLocalUnicast() ||
		address.IsLinkLocalMulticast() ||
		address.IsMulticast() ||
		address.IsUnspecified() {
		return false
	}

	carrierGradeNAT := netip.MustParsePrefix("100.64.0.0/10")
	return !carrierGradeNAT.Contains(address)
}
