package metrics

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/prometheus"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
)

// SparklineDays is the rolling window we keep per (slug, stage). 30 days
// × 24 buckets = 720 ints — enough to drive both the small landing-page
// sparkline (last-7d slice) and the larger detail-page chart (full 30d).
const SparklineDays = 30

// Scanner walks the registry and runs the queries package against each
// (slug, stage) pair, persisting the resulting Snapshot via a Store.
type Scanner struct {
	Prom     *prometheus.Client
	Store    *Store
	Registry string // "owner/repo" for ewws-apps-registry
	Logger   *slog.Logger

	// disabledLogged is flipped after the first "scanner disabled" log so
	// RunForever doesn't spam the log every interval.
	disabledLogged sync.Once
}

// RunOnce fetches the registry, queries Prometheus for every (slug, stage)
// pair, and persists the snapshots. Errors on individual queries are
// logged at warn-level but otherwise swallowed — degraded mode is fine.
// Returns the count of snapshots written.
func (s *Scanner) RunOnce(ctx context.Context) (int, error) {
	if s == nil {
		return 0, errors.New("scanner is nil")
	}

	if s.Prom == nil {
		s.disabledLogged.Do(func() {
			s.logf("info", "metrics scanner skipped — Prometheus disabled")
		})

		return 0, nil
	}

	apps, err := registry.FetchAppsFromGitHub(ctx, s.Registry)
	if err != nil {
		return 0, fmt.Errorf("fetch apps: %w", err)
	}

	processed := 0

	for _, app := range apps {
		for _, stage := range []string{"staging", "prod"} {
			snap := s.collect(ctx, app.Slug, stage)
			if err := s.Store.Upsert(ctx, snap); err != nil {
				s.logf("error", "persist metrics_snapshot failed",
					"slug", app.Slug, "stage", stage, "err", err)

				continue
			}

			processed++
		}
	}

	return processed, nil
}

// collect runs every per-(slug,stage) query, logging warnings but never
// failing the whole snapshot — partial data is preferred over none.
func (s *Scanner) collect(ctx context.Context, slug, stage string) Snapshot {
	snap := Snapshot{
		Slug:        slug,
		Stage:       stage,
		CollectedAt: time.Now().UTC(),
	}

	if v, err := Requests24h(ctx, s.Prom, slug, stage); err == nil {
		snap.Requests24h = v
	} else {
		s.warn("requests_24h", slug, stage, err)
	}

	if v, err := Requests7d(ctx, s.Prom, slug, stage); err == nil {
		snap.Requests7d = v
	} else {
		s.warn("requests_7d", slug, stage, err)
	}

	if t, err := LastRequestAt(ctx, s.Prom, slug, stage, 0); err == nil {
		snap.LastRequestAt = t
	} else {
		s.warn("last_request_at", slug, stage, err)
	}

	if v, err := ErrorRate5xx(ctx, s.Prom, slug, stage); err == nil {
		snap.ErrorRate5xx = v
	} else {
		s.warn("error_rate_5xx", slug, stage, err)
	}

	if v, err := Restarts24h(ctx, s.Prom, slug, stage); err == nil {
		snap.Restarts24h = v
	} else {
		s.warn("restarts_24h", slug, stage, err)
	}

	if v, err := MemoryUsedBytes(ctx, s.Prom, slug, stage); err == nil {
		snap.MemoryUsedBytes = v
	} else {
		s.warn("memory_used_bytes", slug, stage, err)
	}

	if v, err := MemoryLimitBytes(ctx, s.Prom, slug, stage); err == nil {
		snap.MemoryLimitBytes = v
	} else {
		s.warn("memory_limit_bytes", slug, stage, err)
	}

	if v, err := CPUUsedMillicores(ctx, s.Prom, slug, stage); err == nil {
		snap.CPUUsedMillicores = v
	} else {
		s.warn("cpu_used_millicores", slug, stage, err)
	}

	if v, err := CPULimitMillicores(ctx, s.Prom, slug, stage); err == nil {
		snap.CPULimitMillicores = v
	} else {
		s.warn("cpu_limit_millicores", slug, stage, err)
	}

	if sp, err := SparklineHourly(ctx, s.Prom, slug, stage, SparklineDays); err == nil {
		snap.SparklineHourly = sp
	} else {
		s.warn("sparkline_hourly", slug, stage, err)
	}

	return snap
}

// RunForever runs RunOnce immediately, then on every tick of `interval`
// until ctx is canceled.
func (s *Scanner) RunForever(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		n, err := s.RunOnce(ctx)
		if err != nil {
			s.logf("warn", "metrics scan iteration failed", "err", err)
		} else {
			s.logf("info", "metrics scan iteration complete", "snapshots", n)
		}

		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Scanner) warn(metric, slug, stage string, err error) {
	s.logf("warn", "prometheus query failed",
		"metric", metric, "slug", slug, "stage", stage, "err", err)
}

func (s *Scanner) logf(level, msg string, kv ...any) {
	if s.Logger == nil {
		return
	}

	switch level {
	case "info":
		s.Logger.Info(msg, kv...)
	case "warn":
		s.Logger.Warn(msg, kv...)
	case "error":
		s.Logger.Error(msg, kv...)
	}
}
