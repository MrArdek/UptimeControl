package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const defaultHTTPAddress = ":8080"
const defaultPublicOrigin = "http://localhost:8080"

// Config contains the values required to start the application.
type Config struct {
	HTTPAddress         string
	DatabaseURL         string
	PublicOrigin        string
	BasePath            string
	SessionCookieSecure bool
}

// Load reads configuration from the environment and validates it before startup.
func Load() (Config, error) {
	address := strings.TrimSpace(os.Getenv("HTTP_ADDR"))
	if address == "" {
		address = defaultHTTPAddress
	}

	if err := validateHTTPAddress(address); err != nil {
		return Config{}, fmt.Errorf("HTTP_ADDR: %w", err)
	}

	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if err := validateDatabaseURL(databaseURL); err != nil {
		return Config{}, fmt.Errorf("DATABASE_URL: %w", err)
	}

	publicOrigin := strings.TrimSpace(os.Getenv("PUBLIC_ORIGIN"))
	if publicOrigin == "" {
		publicOrigin = defaultPublicOrigin
	}
	publicOrigin = strings.TrimSuffix(publicOrigin, "/")
	if err := validatePublicOrigin(publicOrigin); err != nil {
		return Config{}, fmt.Errorf("PUBLIC_ORIGIN: %w", err)
	}

	basePath, err := normalizeBasePath(os.Getenv("BASE_PATH"))
	if err != nil {
		return Config{}, fmt.Errorf("BASE_PATH: %w", err)
	}

	cookieSecure, err := parseCookieSecure(os.Getenv("SESSION_COOKIE_SECURE"))
	if err != nil {
		return Config{}, fmt.Errorf("SESSION_COOKIE_SECURE: %w", err)
	}

	return Config{
		HTTPAddress:         address,
		DatabaseURL:         databaseURL,
		PublicOrigin:        publicOrigin,
		BasePath:            basePath,
		SessionCookieSecure: cookieSecure,
	}, nil
}

func normalizeBasePath(value string) (string, error) {
	basePath := strings.TrimSpace(value)
	if basePath == "" || basePath == "/" {
		return "", nil
	}

	if !strings.HasPrefix(basePath, "/") {
		return "", fmt.Errorf("must start with /")
	}
	if strings.ContainsAny(basePath, "?#\\") {
		return "", fmt.Errorf("must not contain a query, fragment, or backslash")
	}

	basePath = strings.TrimSuffix(basePath, "/")
	for _, segment := range strings.Split(strings.TrimPrefix(basePath, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("must contain only non-empty path segments")
		}
		for _, character := range segment {
			if !isBasePathCharacter(character) {
				return "", fmt.Errorf("contains unsupported character %q", character)
			}
		}
	}

	return basePath, nil
}

func isBasePathCharacter(character rune) bool {
	return character >= 'a' && character <= 'z' ||
		character >= 'A' && character <= 'Z' ||
		character >= '0' && character <= '9' ||
		strings.ContainsRune("-._~", character)
}

func validatePublicOrigin(origin string) error {
	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return fmt.Errorf("must be a valid URL: %w", err)
	}

	if parsedOrigin.Scheme != "http" && parsedOrigin.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}

	if parsedOrigin.Host == "" {
		return fmt.Errorf("host is required")
	}

	if parsedOrigin.User != nil || parsedOrigin.Path != "" || parsedOrigin.RawQuery != "" || parsedOrigin.Fragment != "" {
		return fmt.Errorf("must contain only scheme and host")
	}

	return nil
}

func parseCookieSecure(value string) (bool, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return true, nil
	}

	secure, err := strconv.ParseBool(trimmed)
	if err != nil {
		return false, fmt.Errorf("must be true or false")
	}

	return secure, nil
}

func validateDatabaseURL(databaseURL string) error {
	if databaseURL == "" {
		return fmt.Errorf("is required")
	}

	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		return fmt.Errorf("must be a valid PostgreSQL URL: %w", err)
	}

	if parsedURL.Scheme != "postgres" && parsedURL.Scheme != "postgresql" {
		return fmt.Errorf("scheme must be postgres or postgresql")
	}

	if parsedURL.Host == "" {
		return fmt.Errorf("host is required")
	}

	if strings.TrimPrefix(parsedURL.Path, "/") == "" {
		return fmt.Errorf("database name is required")
	}

	return nil
}

func validateHTTPAddress(address string) error {
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("must have host:port format: %w", err)
	}

	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be a number from 1 to 65535")
	}

	return nil
}
