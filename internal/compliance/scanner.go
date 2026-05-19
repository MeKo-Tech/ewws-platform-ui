package compliance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	gh "github.com/google/go-github/v68/github"
)

// Scanner walks the registry and runs every Check against each app's
// source repo, persisting the results in a Store.
type Scanner struct {
	GH       *gh.Client
	Store    *Store
	Registry string // "owner/repo" for ewws-apps-registry
	Logger   *slog.Logger

	// Checks lets tests inject a smaller / fake suite. nil → StandardChecks.
	Checks []Check
}

// RunOnce fetches the registry once and runs every Check on every app.
// Errors from individual checks are persisted (status=error) and do not
// abort the whole run. Returns the count of (apps × checks) processed.
func (s *Scanner) RunOnce(ctx context.Context) (int, error) {
	if s == nil {
		return 0, errors.New("scanner is nil")
	}

	apps, err := registry.FetchAppsFromGitHub(ctx, s.Registry)
	if err != nil {
		return 0, fmt.Errorf("fetch apps: %w", err)
	}

	checks := s.Checks
	if checks == nil {
		checks = StandardChecks()
	}

	processed := 0
	for _, app := range apps {
		owner, repo, err := parseRepoURL(app.Repo.URL)
		if err != nil {
			s.logf(ctx, "warn", "skip app — bad repo url", "slug", app.Slug, "url", app.Repo.URL, "err", err)
			continue
		}

		for _, c := range checks {
			status, details := c.Run(ctx, s.GH, owner, repo)

			res := CheckResult{
				Slug:        app.Slug,
				Repo:        fmt.Sprintf("%s/%s", owner, repo),
				CheckName:   c.Name,
				Status:      status,
				Details:     details,
				LastChecked: time.Now().UTC(),
			}

			if err := s.Store.Upsert(ctx, res); err != nil {
				s.logf(ctx, "error", "persist check failed", "slug", app.Slug, "check", c.Name, "err", err)
				continue
			}

			processed++
		}
	}

	return processed, nil
}

// RunForever runs RunOnce immediately, then on every tick of `interval`
// until ctx is canceled. Returns when ctx is done.
func (s *Scanner) RunForever(ctx context.Context, interval time.Duration) {
	tick := time.NewTicker(interval)
	defer tick.Stop()

	for {
		n, err := s.RunOnce(ctx)
		if err != nil {
			s.logf(ctx, "warn", "scan iteration failed", "err", err)
		} else {
			s.logf(ctx, "info", "scan iteration complete", "checks_processed", n)
		}

		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (s *Scanner) logf(_ context.Context, level, msg string, kv ...any) {
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

// parseRepoURL extracts owner + repo from a https://github.com/Owner/Repo URL.
// Tolerates trailing slashes and `.git` suffixes.
func parseRepoURL(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", err
	}
	if u.Host != "github.com" {
		return "", "", fmt.Errorf("not a github.com URL: %s", raw)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("path missing owner/repo: %s", u.Path)
	}

	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("empty owner or repo: %s", raw)
	}

	return owner, repo, nil
}
