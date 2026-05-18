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
	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	httpapp "github.com/MeKo-Tech/ewws-platform-ui/internal/http"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
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

	handler := httpapp.NewRouter(httpapp.Deps{
		Cfg:          cfg,
		Logger:       logger,
		Argo:         argoCl,
		SessionStore: store,
		Reserved:     reserved,
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
