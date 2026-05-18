package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/argocd"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

// Landing renders the all-apps grid.
type Landing struct {
	Cfg    *config.Config
	Argo   *argocd.Client
	Logger *slog.Logger
}

func (h Landing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	apps, err := registry.FetchAppsFromGitHub(ctx, h.Cfg.AppsRegistryRepo)
	if err != nil {
		h.Logger.Error("fetch apps", slog.Any("err", err))
	}

	rows := make([]views.LandingRow, 0, len(apps))

	argoApps, err := h.Argo.ListApplications(ctx)
	if err != nil {
		h.Logger.Warn("argo list applications failed", slog.Any("err", err))
	}

	argoBySlug := indexBySlug(argoApps, "prod")

	for _, app := range apps {
		st := views.StatusFromArgo(argoBySlug[app.Slug], "prod")

		rows = append(rows, views.LandingRow{App: app, Status: st})
	}

	user := userFromCtx(ctx)

	props := views.PageProps{Title: "Apps", User: user}

	render(w, r, views.Landing(props, rows))
}

// indexBySlug maps Argo CD applications back to a slug by stripping the
// "-staging" / "-prod" suffix. Stage filter limits the index to one stage.
func indexBySlug(apps []argocd.Application, stage string) map[string]*argocd.Application {
	out := make(map[string]*argocd.Application, len(apps))

	for i := range apps {
		name := apps[i].Metadata.Name

		slug, st := splitSlugStage(name)
		if slug == "" {
			continue
		}

		if stage != "" && st != stage {
			continue
		}

		a := apps[i]

		out[slug] = &a
	}

	return out
}

func splitSlugStage(name string) (string, string) {
	if name == "" {
		return "", ""
	}

	switch {
	case strings.HasSuffix(name, "-staging"):
		return strings.TrimSuffix(name, "-staging"), "staging"
	case strings.HasSuffix(name, "-prod"):
		return strings.TrimSuffix(name, "-prod"), "prod"
	default:
		return name, ""
	}
}

// userFromCtx adapts a middleware.SessionUser into the views.SessionUser
// the templates expect. nil → anonymous.
func userFromCtx(ctx context.Context) *views.SessionUser {
	u := middleware.UserFromContext(ctx)
	if u == nil {
		return nil
	}

	return &views.SessionUser{
		Login:   u.Login,
		Name:    u.Name,
		IsAdmin: u.IsAdmin,
	}
}
