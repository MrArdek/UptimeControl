package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/MrArdek/UptimeControl/internal/checknodes"
	"github.com/MrArdek/UptimeControl/internal/identity"
	"github.com/MrArdek/UptimeControl/internal/monitoring"
)

type healthState struct {
	startedAt   time.Time
	lastContact atomic.Int64
	spool       *resultSpool
}

func main() {
	if err := run(); err != nil {
		slog.Error("check node stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	config, err := loadConfig()
	if err != nil {
		return err
	}
	spool, err := openSpool(config.BufferFile)
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	client := newBackendClient(config)
	checker := monitoring.NewHTTPChecker()
	state := &healthState{startedAt: time.Now().UTC(), spool: spool}
	runtimeContext, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	healthServer := startHealthServer(config.HealthAddr, state)
	defer func() {
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = healthServer.Shutdown(shutdown)
	}()

	logger.Info("regional check node started", "region", config.Region, "health_address", config.HealthAddr)
	ticker := time.NewTicker(config.PollEvery)
	defer ticker.Stop()
	for {
		if err := cycle(runtimeContext, config, client, checker, spool, state, logger); err != nil && !errors.Is(err, context.Canceled) {
			logger.Warn("node cycle failed", "error", err)
		}
		select {
		case <-runtimeContext.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func cycle(ctx context.Context, config nodeConfig, client *backendClient, checker monitoring.Checker, spool *resultSpool, state *healthState, logger *slog.Logger) error {
	if batch := spool.batch(100); len(batch) > 0 {
		outcome, err := client.results(ctx, batch)
		if err != nil {
			return err
		}
		logRejected(logger, outcome)
		if err := spool.acknowledge(len(batch)); err != nil {
			return err
		}
		state.lastContact.Store(time.Now().Unix())
	}
	assignments, err := client.assignments(ctx)
	if err != nil {
		return err
	}
	state.lastContact.Store(time.Now().Unix())
	var workers sync.WaitGroup
	semaphore := make(chan struct{}, 4)
	for _, assignment := range assignments {
		assignment := assignment
		if assignment.Region != config.Region {
			return fmt.Errorf("assignment region %q does not match configured region %q", assignment.Region, config.Region)
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			started := time.Now().UTC()
			result := checker.Check(ctx, monitoring.DueMonitor{ID: assignment.MonitorID, Type: assignment.Type, URL: assignment.URL, Target: assignment.Target, TimeoutSeconds: assignment.TimeoutSeconds, CheckIntervalSeconds: assignment.CheckIntervalSeconds})
			resultID, idErr := identity.NewUUID()
			if idErr != nil {
				logger.Error("generate result ID", "error", idErr)
				return
			}
			finished := time.Now().UTC()
			regional := checknodes.Result{ResultID: resultID, AssignmentID: assignment.ID, MonitorID: assignment.MonitorID, Region: config.Region, StartedAt: started, FinishedAt: finished, Available: result.Available, StatusCode: result.StatusCode, ResponseTimeMS: result.ResponseTimeMS, ErrorCode: normalizeError(result)}
			if err := spool.append(regional); err != nil {
				logger.Error("buffer regional result", "error", err)
			}
		}()
	}
	workers.Wait()
	if batch := spool.batch(100); len(batch) > 0 {
		outcome, err := client.results(ctx, batch)
		if err != nil {
			return err
		}
		logRejected(logger, outcome)
		if err := spool.acknowledge(len(batch)); err != nil {
			return err
		}
		state.lastContact.Store(time.Now().Unix())
	}
	return nil
}

func logRejected(logger *slog.Logger, outcome checknodes.BatchOutcome) {
	if len(outcome.Rejected) > 0 {
		logger.Warn("backend rejected regional results", "count", len(outcome.Rejected))
	}
}

func normalizeError(result monitoring.Result) *string {
	if result.Available || result.Error == nil {
		return nil
	}
	value := "connection_failed"
	switch strings.ToLower(*result.Error) {
	case "request timed out":
		value = "request_timeout"
	case "dns lookup failed":
		value = "dns_failure"
	case "connection refused":
		value = "connection_refused"
	case "tls connection failed":
		value = "tls_failure"
	case "target is in a blocked network", "unsafe monitoring url", "unsafe tcp target":
		value = "blocked_target"
	default:
		if result.StatusCode != nil {
			value = "unexpected_status"
		}
	}
	return &value
}

func startHealthServer(address string, state *healthState) *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(response http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			response.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		last := state.lastContact.Load()
		payload := struct {
			Status             string     `json:"status"`
			StartedAt          time.Time  `json:"started_at"`
			LastBackendContact *time.Time `json:"last_backend_contact"`
			BufferedResults    int        `json:"buffered_results"`
		}{Status: "ok", StartedAt: state.startedAt, BufferedResults: state.spool.len()}
		if last > 0 {
			value := time.Unix(last, 0).UTC()
			payload.LastBackendContact = &value
		}
		response.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(response).Encode(payload)
	})
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("health server stopped", "error", err)
		}
	}()
	return server
}
