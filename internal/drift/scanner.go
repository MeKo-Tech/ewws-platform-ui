package drift

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
)

// Scanner walks the registry, asks the Fetcher how many commits each
// app's staging is ahead of prod (per workload), and persists the
// results in a Store.
type Scanner struct {
	Fetcher  *Fetcher
	Store    *Store
	Registry string // "owner/repo" for ewws-apps-registry
	Logger   *slog.Logger
}

// RunOnce fetches the registry, computes drift for every (slug, component)
// pair where the app declares per-stage image tags, and persists the
// resulting snapshots. Returns the count of rows written.
func (s *Scanner) RunOnce(ctx context.Context) (int, error) {
	if s == nil {
		return 0, errors.New("scanner is nil")
	}

	apps, err := registry.FetchAppsFromGitHub(ctx, s.Registry)
	if err != nil {
		return 0, fmt.Errorf("fetch apps: %w", err)
	}

	processed := 0

	for _, app := range apps {
		owner, repo, err := registry.ParseGitHubURL(app.Repo.URL)
		if err != nil {
			s.logf(
				"warn",
				"skip app — bad repo url",
				"slug",
				app.Slug,
				"url",
				app.Repo.URL,
				"err",
				err,
			)
			continue
		}

		blocks := []struct {
			Component Component
			Block     registry.ImageBlock
		}{
			{ComponentBackend, app.Images.Backend},
			{ComponentFrontend, app.Images.Frontend},
		}

		for _, b := range blocks {
			snap, ok := s.collect(ctx, app.Slug, owner, repo, b.Component, b.Block)
			if !ok {
				continue
			}

			if err := s.Store.Upsert(ctx, snap); err != nil {
				s.logf("error", "persist drift_snapshot failed",
					"slug", app.Slug, "component", b.Component, "err", err)

				continue
			}

			processed++
		}
	}

	return processed, nil
}

// collect computes one (slug, component) snapshot. Returns (zero,false)
// when the component has no per-stage tags configured — we skip writing
// a row at all so the dashboard can tell "no drift" apart from "drift
// not tracked".
func (s *Scanner) collect(
	ctx context.Context, slug, owner, repo string, component Component, block registry.ImageBlock,
) (Snapshot, bool) {
	if block.Tag.Staging == "" && block.Tag.Prod == "" {
		return Snapshot{}, false
	}

	ahead, err := s.Fetcher.CommitsAhead(ctx, owner, repo, block.Tag.Prod, block.Tag.Staging)
	if err != nil {
		s.logf("warn", "compare failed", "slug", slug, "component", component, "err", err)
		// Persist a snapshot with the tags we know but zero ahead-count so
		// the UI can still render "drift status unavailable" gracefully.
	}

	return Snapshot{
		Slug:         slug,
		Component:    component,
		StagingTag:   block.Tag.Staging,
		ProdTag:      block.Tag.Prod,
		CommitsAhead: ahead,
		CollectedAt:  time.Now().UTC(),
	}, true
}

// RunForever runs RunOnce immediately, then on every tick of `interval`
// until ctx is canceled.
func (s *Scanner) RunForever(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		n, err := s.RunOnce(ctx)
		if err != nil {
			s.logf("warn", "drift scan iteration failed", "err", err)
		} else {
			s.logf("info", "drift scan iteration complete", "snapshots", n)
		}

		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
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
