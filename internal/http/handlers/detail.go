package handlers

import (
	"log/slog"
	"net/http"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/argocd"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/status"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

// Detail renders the per-app page. Combines the registry claim YAML,
// the Argo CD Application objects per stage, the per-stage metrics
// snapshot (traffic, restarts, sparkline 30d), and the resource tree.
type Detail struct {
	Cfg        *config.Config
	Argo       *argocd.Client
	Aggregator *status.Aggregator
	Logger     *slog.Logger
}

func (h Detail) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	slug := r.PathValue("slug")

	props := views.PageProps{Title: slug, User: userFromCtx(ctx)}

	if !registry.SlugRegex.MatchString(slug) {
		http.Error(w, "invalid slug", http.StatusBadRequest)
		return
	}

	app, yamlBytes, err := registry.FetchSingleApp(ctx, h.Cfg.AppsRegistryRepo, slug)
	if err != nil {
		h.Logger.Info("app not found", slog.String("slug", slug), slog.Any("err", err))

		w.WriteHeader(http.StatusNotFound)

		render(w, r, views.Detail(props, views.DetailProps{}))

		return
	}

	staging, err := h.Argo.GetApplication(ctx, slug+"-staging")
	if err != nil {
		h.Logger.Debug("argo get staging", slog.Any("err", err))
	}

	prod, err := h.Argo.GetApplication(ctx, slug+"-prod")
	if err != nil {
		h.Logger.Debug("argo get prod", slog.Any("err", err))
	}

	var tree *argocd.ResourceTree

	stageName := slug + "-prod"
	if prod == nil {
		stageName = slug + "-staging"
	}

	if t, err := h.Argo.GetResourceTree(ctx, stageName); err == nil {
		tree = t
	}

	// Aggregate the rich slice (metrics + drift + compliance) for this
	// one tenant. The Aggregator already knows how to merge everything
	// for any app — we just hand it a single-element slice.
	var tenant *status.Tenant

	if h.Aggregator != nil {
		all := h.Aggregator.BuildAll(ctx, []registry.App{*app})
		if len(all) > 0 {
			t := all[0]
			tenant = &t
		}
	}

	render(w, r, views.Detail(props, views.DetailProps{
		App:     app,
		Staging: staging,
		Prod:    prod,
		Tree:    tree,
		YAML:    string(yamlBytes),
		Tenant:  tenant,
	}))
}

// PartialAppStatus is the HTMX-swap target for the landing-page cells.
type PartialAppStatus struct {
	Cfg    *config.Config
	Argo   *argocd.Client
	Logger *slog.Logger
}

func (h PartialAppStatus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")

	if !registry.SlugRegex.MatchString(slug) {
		http.Error(w, "invalid slug", http.StatusBadRequest)
		return
	}

	app, err := h.Argo.GetApplication(r.Context(), slug+"-prod")
	if err != nil {
		h.Logger.Debug("partial status get failed", slog.Any("err", err))
	}

	render(w, r, views.StatusCell(views.StatusFromArgo(app, "prod")))
}
