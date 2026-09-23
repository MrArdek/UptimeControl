package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var nodeRegionPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{1,31}$`)

type nodeConfig struct {
	BackendURL string
	Secret     string
	Region     string
	HealthAddr string
	BufferFile string
	PollEvery  time.Duration
	AllowHTTP  bool
}

func loadConfig() (nodeConfig, error) {
	config := nodeConfig{
		BackendURL: strings.TrimRight(strings.TrimSpace(os.Getenv("CHECK_NODE_BACKEND_URL")), "/"),
		Secret:     strings.TrimSpace(os.Getenv("CHECK_NODE_SECRET")),
		Region:     strings.ToLower(strings.TrimSpace(os.Getenv("CHECK_NODE_REGION"))),
		HealthAddr: strings.TrimSpace(os.Getenv("CHECK_NODE_HEALTH_ADDR")),
		BufferFile: strings.TrimSpace(os.Getenv("CHECK_NODE_BUFFER_FILE")),
		PollEvery:  5 * time.Second,
	}
	if config.HealthAddr == "" {
		config.HealthAddr = "127.0.0.1:8090"
	}
	if config.BufferFile == "" {
		config.BufferFile = "check-node-buffer.json"
	}
	if value := strings.TrimSpace(os.Getenv("CHECK_NODE_POLL_INTERVAL")); value != "" {
		duration, err := time.ParseDuration(value)
		if err != nil || duration < time.Second || duration > time.Minute {
			return nodeConfig{}, fmt.Errorf("CHECK_NODE_POLL_INTERVAL must be between 1s and 1m")
		}
		config.PollEvery = duration
	}
	if value := strings.TrimSpace(os.Getenv("CHECK_NODE_ALLOW_INSECURE_HTTP")); value != "" {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nodeConfig{}, fmt.Errorf("CHECK_NODE_ALLOW_INSECURE_HTTP must be true or false")
		}
		config.AllowHTTP = parsed
	}
	parsed, err := url.Parse(config.BackendURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nodeConfig{}, fmt.Errorf("CHECK_NODE_BACKEND_URL must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && config.AllowHTTP) {
		return nodeConfig{}, fmt.Errorf("CHECK_NODE_BACKEND_URL must use https; set CHECK_NODE_ALLOW_INSECURE_HTTP=true only for local testing")
	}
	if len(config.Secret) < 32 || len(config.Secret) > 128 {
		return nodeConfig{}, fmt.Errorf("CHECK_NODE_SECRET must contain 32 to 128 characters")
	}
	if !nodeRegionPattern.MatchString(config.Region) {
		return nodeConfig{}, fmt.Errorf("CHECK_NODE_REGION must be a valid lowercase region code")
	}
	if _, _, err := net.SplitHostPort(config.HealthAddr); err != nil {
		return nodeConfig{}, fmt.Errorf("CHECK_NODE_HEALTH_ADDR must use host:port format: %w", err)
	}
	return config, nil
}
