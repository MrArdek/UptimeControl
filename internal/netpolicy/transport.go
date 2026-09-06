package netpolicy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"
)

const safeDialTimeout = 5 * time.Second

// SafeDialContext returns a dial function that resolves the target hostname
// immediately before connecting and refuses blocked networks. It mirrors the
// monitoring checker policy so outbound webhooks get the same SSRF protection
// as inbound availability checks.
func SafeDialContext(resolver *net.Resolver) func(context.Context, string, string) (net.Conn, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	dialer := net.Dialer{Timeout: safeDialTimeout, KeepAlive: 15 * time.Second}

	return func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, errors.New("invalid target address")
		}

		addresses := make([]netip.Addr, 0, 2)
		if literal, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
			addresses = append(addresses, literal)
		} else {
			addresses, err = resolver.LookupNetIP(ctx, "ip", host)
			if err != nil {
				return nil, errors.New("DNS lookup failed")
			}
		}

		if len(addresses) == 0 {
			return nil, errors.New("DNS returned no addresses")
		}
		for _, resolvedAddress := range addresses {
			if !AddressAllowed(resolvedAddress) {
				return nil, errors.New("target resolved to a blocked network")
			}
		}
		for _, resolvedAddress := range addresses {
			connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(resolvedAddress.String(), port))
			if err == nil {
				return connection, nil
			}
		}
		return nil, errors.New("connection failed")
	}
}

// NewSafeTransport builds an HTTP transport that never uses a proxy and only
// connects through SafeDialContext. Timeouts mirror the monitoring checker.
func NewSafeTransport(timeout time.Duration) *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           SafeDialContext(nil),
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
	}
}

// PublicRedirectValidator rejects redirect chains that leave public HTTP(S)
// URLs or exceed the allowed hop count.
func PublicRedirectValidator(maximumRedirects int) func(*http.Request, []*http.Request) error {
	return func(request *http.Request, via []*http.Request) error {
		if len(via) >= maximumRedirects {
			return errors.New("too many redirects")
		}
		if _, err := NormalizeHTTPURL(request.URL.String()); err != nil {
			return errors.New("redirect target is not public")
		}
		return nil
	}
}
