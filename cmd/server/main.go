package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/config"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/httpapi"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/mailer"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/service"
	"github.com/hayes-ulricu4b97qv/tyoechonext/internal/store"
)

const maintenanceOperationTimeout = 10 * time.Second

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	exitCode := 0
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(shutdown)

	dataDir := envOr("APP_DATA_DIR", "./data")
	port := envOr("APP_PORT", "8081")
	settings := config.FromEnv()
	smtpMailer, err := mailer.FromEnv()
	if err != nil {
		logger.Error("configure SMTP mail delivery", "error", err)
		return 1
	}
	var configuredMailer service.Mailer
	if smtpMailer != nil {
		configuredMailer = smtpMailer
	}

	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		logger.Error("create data directory", "error", err)
		return 1
	}
	logFile, err := openApplicationLog(filepath.Join(dataDir, "app.log"), maxApplicationLogSize)
	if err != nil {
		logger.Warn("open application log", "error", err)
	} else {
		defer logFile.Close()
		logger = slog.New(slog.NewJSONHandler(logFanout{os.Stdout, logFile}, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
	if value := os.Getenv("APP_COOKIE_SECURE"); value != "" && value != "true" && value != "false" {
		logger.Warn("APP_COOKIE_SECURE must be exactly true or false; Secure cookies are disabled")
	}

	databasePath := filepath.Join(dataDir, "app.db")
	repository, err := store.NewSQLiteRepository(databasePath)
	if err != nil {
		logger.Error("open repository", "error", err)
		return 1
	}
	defer repository.Close()
	cleanOutboundMail(context.Background(), repository, logger)
	cleanExpiredSessions(context.Background(), repository, logger)

	server := httpapi.NewServer(repository, settings, logger, configuredMailer)
	httpServer := &http.Server{
		Addr:              net.JoinHostPort(settings.ListenHost, port),
		Handler:           server.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      settings.WriteTimeout,
		IdleTimeout:       60 * time.Second,
	}
	listener, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		logger.Error("listen", "addr", httpServer.Addr, "error", err)
		return 1
	}

	maintenanceCtx, stopMaintenance := context.WithCancel(context.Background())
	maintenanceDone := make(chan struct{})
	go func() {
		defer close(maintenanceDone)
		maintainDailyCleanup(maintenanceCtx, repository, logger)
	}()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- httpServer.Serve(listener)
	}()
	logger.Info("server listening", "addr", listener.Addr().String(), "version", settings.Version)

	var serveErr error
	serveExited := false
	select {
	case <-shutdown:
	case serveErr = <-serveDone:
		serveExited = true
	}
	stopMaintenance()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	shutdownErr := httpServer.Shutdown(ctx)
	cancel()
	if shutdownErr != nil {
		logger.Error("graceful shutdown failed", "error", shutdownErr)
		exitCode = 1
		if closeErr := httpServer.Close(); closeErr != nil {
			logger.Error("force close HTTP server failed", "error", closeErr)
		}
	}
	if !serveExited {
		serveErr = <-serveDone
	}
	if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
		logger.Error("server stopped", "error", serveErr)
		exitCode = 1
	}
	backgroundCtx, cancelBackground := context.WithTimeout(context.Background(), 12*time.Second)
	backgroundErr := server.WaitForBackgroundWork(backgroundCtx)
	cancelBackground()
	if backgroundErr != nil {
		logger.Warn("wait for background mail delivery", "error", backgroundErr)
		exitCode = 1
	}
	<-maintenanceDone
	return exitCode
}

func maintainDailyCleanup(ctx context.Context, repository store.Repository, logger *slog.Logger) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cleanOutboundMail(ctx, repository, logger)
			cleanExpiredSessions(ctx, repository, logger)
		}
	}
}

func cleanOutboundMail(ctx context.Context, repository store.Repository, logger *slog.Logger) {
	cleanupCtx, cancel := context.WithTimeout(ctx, maintenanceOperationTimeout)
	defer cancel()
	cutoff := time.Now().UTC().Add(-90 * 24 * time.Hour)
	if err := repository.CleanupOutboundMail(cleanupCtx, cutoff); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("clean outbound mail history", "error", err)
	}
}

func cleanExpiredSessions(ctx context.Context, repository store.Repository, logger *slog.Logger) {
	cleanupCtx, cancel := context.WithTimeout(ctx, maintenanceOperationTimeout)
	defer cancel()
	if err := repository.PurgeSessionsContext(cleanupCtx, time.Now().UTC()); err != nil && !errors.Is(err, context.Canceled) {
		logger.Warn("clean expired sessions", "error", err)
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
