package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MrArdek/UptimeControl/internal/auth"
	"github.com/MrArdek/UptimeControl/internal/config"
	"github.com/MrArdek/UptimeControl/internal/httpserver"
	"github.com/MrArdek/UptimeControl/internal/migrations"
	"github.com/MrArdek/UptimeControl/internal/monitoring"
	"github.com/MrArdek/UptimeControl/internal/postgres"
	"github.com/MrArdek/UptimeControl/internal/projects"
	"github.com/MrArdek/UptimeControl/internal/sites"
)

const startupTimeout = 10 * time.Second

func main() {
	if err := run(); err != nil {
		slog.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load configuration: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	startupContext, cancelStartup := context.WithTimeout(context.Background(), startupTimeout)
	defer cancelStartup()

	database, err := postgres.Open(startupContext, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer database.Close()

	if err := migrations.Up(startupContext, database); err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}

	logger.Info("database connected and migrations applied")

	authentication := auth.NewService(auth.NewPostgresStore(database))
	siteManagement := sites.NewService(sites.NewPostgresStore(database))
	projectStore := projects.NewPostgresStore(database)
	projectManagement := projects.NewService(projectStore)
	monitorStore := monitoring.NewPostgresStore(database)
	notifiers := make([]monitoring.Notifier, 0, 2)
	if cfg.TelegramBotToken != "" {
		notifiers = append(notifiers, monitoring.NewTelegramNotifier(cfg.TelegramBotToken, cfg.TelegramChatID))
	}
	// Webhook delivery fans out alongside Telegram: existing single-channel
	// installations keep working unchanged when no webhook is configured.
	notifiers = append(notifiers, monitoring.NewWebhookDispatcher(projectStore, monitorStore, logger))
	var notifier monitoring.Notifier
	if len(notifiers) > 0 {
		notifier = monitoring.NewMultiNotifier(notifiers...)
	}
	scheduler := monitoring.NewScheduler(monitorStore, monitoring.NewHTTPChecker(), notifier, logger)
	runtimeContext, cancelRuntime := context.WithCancel(context.Background())
	monitoringDone := make(chan struct{})
	go func() {
		defer close(monitoringDone)
		scheduler.Run(runtimeContext)
	}()
	defer func() {
		cancelRuntime()
		<-monitoringDone
	}()

	server := httpserver.NewApplication(
		cfg.HTTPAddress,
		database,
		authentication,
		siteManagement,
		projectManagement,
		monitorStore,
		scheduler,
		httpserver.Options{
			AllowedOrigin: cfg.PublicOrigin,
			BasePath:      cfg.BasePath,
			CookieSecure:  cfg.SessionCookieSecure,
		})
	serverErrors := make(chan error, 1)

	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	logger.Info("http server started", "address", cfg.HTTPAddress, "base_path", cfg.BasePath)

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(shutdownSignals)

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("serve HTTP: %w", err)
	case receivedSignal := <-shutdownSignals:
		logger.Info("shutdown signal received", "signal", receivedSignal.String())
	}
	cancelRuntime()

	shutdownContext, cancelShutdown := httpserver.ShutdownContext()
	defer cancelShutdown()

	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown HTTP server: %w", err)
	}

	if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP: %w", err)
	}

	logger.Info("http server stopped", "timeout", (10 * time.Second).String())

	return nil
}
