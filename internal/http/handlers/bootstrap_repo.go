package handlers

import (
	"log/slog"
	"net/http"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/repobootstrap"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
	"github.com/google/go-github/v68/github"
	"golang.org/x/oauth2"
)

// BootstrapRepo runs the same set of "ensure" steps the at-claim flow
// uses (test-pr-title, release-please, branch protection) against the
// source repo of an already-claimed tenant. Vibe-coders can trigger it
// from the detail page when their compliance row shows openings.
//
// Auth: the user's OAuth token (granted `repo` + `workflow` at login)
// is the actor for every GitHub API call, so the audit log shows
// _who_ ran the bootstrap.
type BootstrapRepo struct {
	Cfg    *config.Config
	Logger *slog.Logger
}

func (h BootstrapRepo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slug := r.PathValue("slug")
	if !registry.SlugRegex.MatchString(slug) {
		http.Error(w, "invalid slug", http.StatusBadRequest)
		return
	}

	user := middleware.UserFromContext(ctx)
	if user == nil || user.Token == "" {
		http.Error(w, "login with GitHub first (we need your token to write the workflows)", http.StatusUnauthorized)
		return
	}

	app, _, err := registry.FetchSingleApp(ctx, h.Cfg.AppsRegistryRepo, slug)
	if err != nil {
		h.Logger.Warn("bootstrap: app not found", slog.String("slug", slug), slog.Any("err", err))
		http.Error(w, "slug not found in registry", http.StatusNotFound)

		return
	}

	owner, repo, err := registry.ParseGitHubURL(app.Repo.URL)
	if err != nil {
		h.Logger.Warn("bootstrap: bad repo URL", slog.String("slug", slug), slog.Any("err", err))
		http.Error(w, "the claim's repo URL didn't parse: "+err.Error(), http.StatusBadRequest)

		return
	}

	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: user.Token})
	client := github.NewClient(oauth2.NewClient(ctx, src))

	results := repobootstrap.Run(ctx, client, owner, repo, slug)

	h.Logger.Info(
		"bootstrap finished",
		slog.String("slug", slug),
		slog.String("repo", owner+"/"+repo),
		slog.String("actor", user.Login),
		slog.Int("steps", len(results)),
	)

	// HTMX swap: render just the result panel. Detail page polls /docs
	// for next-step instructions; we just inline the action list here.
	render(w, r, views.BootstrapResult(results))
}
