package handlers

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/status"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

// Landing renders the dashboard: one row/card per registry app, with
// Argo CD state + metrics + drift + compliance merged in by the Aggregator.
type Landing struct {
	Cfg        *config.Config
	Aggregator *status.Aggregator
	Logger     *slog.Logger
}

func (h Landing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	apps, err := registry.FetchAppsFromGitHub(ctx, h.Cfg.AppsRegistryRepo)
	if err != nil {
		h.Logger.Error("fetch apps", slog.Any("err", err))
	}

	var tenants []status.Tenant
	if h.Aggregator != nil {
		tenants = h.Aggregator.BuildAll(ctx, apps)
	}

	user := userFromCtx(ctx)

	props := views.PageProps{Title: "Apps", User: user}

	render(w, r, views.Landing(props, tenants))
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
