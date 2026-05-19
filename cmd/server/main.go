// Command server is the ewws-platform-ui entrypoint.
//
// Reads config from env, builds the HTTP router, listens on $PORT.
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

	"github.com/MeKo-Tech/ewws-platform-ui/internal/argocd"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/compliance"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/drift"
	httpapp "github.com/MeKo-Tech/ewws-platform-ui/internal/http"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/metrics"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/prometheus"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/status"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/storage"
	gh "github.com/google/go-github/v68/github"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	logger := buildLogger(cfg.LogFormat)

	registry.SetAPIToken(cfg.GitHubAPIToken)

	argoCl := argocd.New(cfg.ArgoCDURL, cfg.ArgoCDToken)

	store := middleware.NewSessionStore(cfg.SessionSecret)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	reservedCtx, reservedCancel := context.WithTimeout(ctx, 10*time.Second)

	reserved, err := registry.LoadReservedSlugs(reservedCtx, cfg.AppsRegistryRepo)

	reservedCancel()

	if err != nil {
		logger.Warn("reserved-slugs fetch failed; continuing with empty list", slog.Any("err", err))
	}

	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		logger.Warn(
			"storage open failed; compliance scans disabled",
			slog.String("path", cfg.DBPath),
			slog.Any("err", err),
		)
	}

	if db != nil {
		defer db.Close()
	}

	complianceStore := compliance.NewStore(db)
	metricsStore := metrics.NewStore(db)
	driftStore := drift.NewStore(db)

	var ghClient *gh.Client
	if cfg.GitHubAPIToken != "" {
		ghClient = gh.NewClient(nil).WithAuthToken(cfg.GitHubAPIToken)
	}

	if db != nil && cfg.ComplianceScanInterval > 0 && ghClient != nil {
		scanner := &compliance.Scanner{
			GH:       ghClient,
			Store:    complianceStore,
			Registry: cfg.AppsRegistryRepo,
			Logger:   logger.With(slog.String("component", "compliance")),
		}
		go scanner.RunForever(ctx, cfg.ComplianceScanInterval)

		logger.Info(
			"compliance scanner started",
			slog.Duration("interval", cfg.ComplianceScanInterval),
		)
	} else {
		logger.Info("compliance scanner not started",
			slog.Bool("has_db", db != nil),
			slog.Duration("interval", cfg.ComplianceScanInterval),
			slog.Bool("has_token", cfg.GitHubAPIToken != ""))
	}

	if db != nil && cfg.MetricsScanInterval > 0 && cfg.PrometheusEnabled() {
		promClient := prometheus.New(cfg.PrometheusURL)

		scanner := &metrics.Scanner{
			Prom:     promClient,
			Store:    metricsStore,
			Registry: cfg.AppsRegistryRepo,
			Logger:   logger.With(slog.String("component", "metrics")),
		}
		go scanner.RunForever(ctx, cfg.MetricsScanInterval)

		logger.Info("metrics scanner started", slog.Duration("interval", cfg.MetricsScanInterval))
	} else {
		logger.Info("metrics scanner not started",
			slog.Bool("has_db", db != nil),
			slog.Duration("interval", cfg.MetricsScanInterval),
			slog.Bool("prometheus_enabled", cfg.PrometheusEnabled()))
	}

	if db != nil && cfg.DriftScanInterval > 0 && ghClient != nil {
		scanner := &drift.Scanner{
			Fetcher: &drift.Fetcher{
				GH:     ghClient,
				Logger: logger.With(slog.String("component", "drift")),
			},
			Store:    driftStore,
			Registry: cfg.AppsRegistryRepo,
			Logger:   logger.With(slog.String("component", "drift")),
		}
		go scanner.RunForever(ctx, cfg.DriftScanInterval)

		logger.Info("drift scanner started", slog.Duration("interval", cfg.DriftScanInterval))
	} else {
		logger.Info("drift scanner not started",
			slog.Bool("has_db", db != nil),
			slog.Duration("interval", cfg.DriftScanInterval),
			slog.Bool("has_token", cfg.GitHubAPIToken != ""))
	}

	aggregator := &status.Aggregator{
		Argo:            argoCl,
		MetricsStore:    metricsStore,
		DriftStore:      driftStore,
		ComplianceStore: complianceStore,
		Logger:          logger.With(slog.String("component", "aggregator")),
	}

	handler := httpapp.NewRouter(httpapp.Deps{
		Cfg:             cfg,
		Logger:          logger,
		Argo:            argoCl,
		SessionStore:    store,
		Reserved:        reserved,
		ComplianceStore: complianceStore,
		MetricsStore:    metricsStore,
		DriftStore:      driftStore,
		Aggregator:      aggregator,
	})

	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	serverErr := make(chan error, 1)

	go func() {
		logger.Info("listening", slog.String("addr", addr), slog.String("base_url", cfg.BaseURL))

		serverErr <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("listen: %w", err)
		}
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	return nil
}

func buildLogger(format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}

	if format == "text" {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}
