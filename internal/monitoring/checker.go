package monitoring

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/MrArdek/UptimeControl/internal/netpolicy"
)

const (
	maximumRedirects = 3
	defaultTimeout   = 10 * time.Second
)

type addressResolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type HTTPChecker struct {
	resolver addressResolver
	dialer   net.Dialer
	now      func() time.Time
}

func NewHTTPChecker() *HTTPChecker {
	return &HTTPChecker{
		resolver: net.DefaultResolver,
		dialer: net.Dialer{
			Timeout:   5 * time.Second,
			KeepAlive: 15 * time.Second,
		},
		now: time.Now,
	}
}

func (checker *HTTPChecker) Check(ctx context.Context, monitor DueMonitor) Result {
	checkedAt := checker.now().UTC()
	result := Result{CheckedAt: checkedAt}
	if monitor.Type != "http" {
		message := "unsupported monitor type"
		result.Error = &message
		return result
	}
	if _, err := netpolicy.NormalizeHTTPURL(monitor.URL); err != nil {
		message := "unsafe monitoring URL"
		result.Error = &message
		return result
	}

	timeout := time.Duration(monitor.TimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > 30*time.Second {
		timeout = defaultTimeout
	}
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           checker.dialContext,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			if len(via) >= maximumRedirects {
				return errors.New("too many redirects")
			}
			if _, err := netpolicy.NormalizeHTTPURL(request.URL.String()); err != nil {
				return errors.New("redirect target is not public")
			}
			return nil
		},
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, monitor.URL, nil)
	if err != nil {
		message := "invalid request URL"
		result.Error = &message
		return result
	}
	request.Header.Set("User-Agent", "UptimeControl/1.0")
	request.Header.Set("Accept", "*/*")

	startedAt := checker.now()
	response, err := client.Do(request)
	elapsed := checker.now().Sub(startedAt).Milliseconds()
	if elapsed < 0 {
		elapsed = 0
	}
	result.ResponseTimeMS = &elapsed
	if err != nil {
		message := safeNetworkError(err)
		result.Error = &message
		return result
	}
	defer response.Body.Close()

	statusCode := response.StatusCode
	result.StatusCode = &statusCode
	result.Available = statusCode >= 200 && statusCode < 400
	if !result.Available {
		message := fmt.Sprintf("unexpected HTTP status %d", statusCode)
		result.Error = &message
	}
	return result
}

func (checker *HTTPChecker) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.New("invalid target address")
	}

	addresses := make([]netip.Addr, 0, 2)
	if literal, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		addresses = append(addresses, literal)
	} else {
		addresses, err = checker.resolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return nil, errors.New("DNS lookup failed")
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("DNS returned no addresses")
	}
	for _, resolvedAddress := range addresses {
		if !netpolicy.AddressAllowed(resolvedAddress) {
			return nil, errors.New("target resolved to a blocked network")
		}
	}

	var lastError error
	for _, resolvedAddress := range addresses {
		connection, err := checker.dialer.DialContext(ctx, network, net.JoinHostPort(resolvedAddress.String(), port))
		if err == nil {
			return connection, nil
		}
		lastError = err
	}
	if lastError == nil {
		lastError = errors.New("connection failed")
	}
	return nil, lastError
}

func safeNetworkError(err error) string {
	var urlError *url.Error
	if errors.As(err, &urlError) {
		err = urlError.Err
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "deadline exceeded"), strings.Contains(message, "timeout"):
		return "request timed out"
	case strings.Contains(message, "blocked network"), strings.Contains(message, "not public"):
		return "target is in a blocked network"
	case strings.Contains(message, "dns"):
		return "DNS lookup failed"
	case strings.Contains(message, "refused"):
		return "connection refused"
	case strings.Contains(message, "tls") || strings.Contains(message, "certificate"):
		return "TLS connection failed"
	default:
		return "connection failed"
	}
}
