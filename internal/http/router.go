// Package httpapp wires the HTTP stack: middleware chain, ServeMux,
// embedded static FS. main.go calls NewRouter to get a single http.Handler.
package httpapp

import (
	"log/slog"
	"net/http"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/argocd"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/compliance"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/handlers"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/static"
)

// Deps bundles everything the router needs.
type Deps struct {
	Cfg             *config.Config
	Logger          *slog.Logger
	Argo            *argocd.Client
	SessionStore    *middleware.SessionStore
	Reserved        *registry.ReservedSlugs
	ComplianceStore *compliance.Store
}

// NewRouter builds the *http.ServeMux + middleware chain.
func NewRouter(deps Deps) http.Handler {
	mux := http.NewServeMux()

	registerStatic(mux)
	registerHealth(mux, deps)
	registerPages(mux, deps)
	registerAuth(mux, deps)
	registerPartials(mux, deps)

	chain := middleware.Recover(deps.Logger)(
		middleware.Logging(deps.Logger)(
			middleware.Session(deps.SessionStore, deps.Logger)(mux),
		),
	)

	return chain
}

func registerStatic(mux *http.ServeMux) {
	fs := http.FileServer(http.FS(static.Files))
	mux.Handle("GET /static/", http.StripPrefix("/static/", fs))
}

func registerHealth(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /healthz", handlers.Health{})
	mux.Handle("GET /readyz", handlers.Ready{Cfg: d.Cfg})
}

func registerPages(mux *http.ServeMux, d Deps) {
	landing := handlers.Landing{Cfg: d.Cfg, Argo: d.Argo, Logger: d.Logger}
	mux.Handle("GET /{$}", landing)

	detail := handlers.Detail{Cfg: d.Cfg, Argo: d.Argo, Logger: d.Logger}
	mux.Handle("GET /app/{slug}", detail)

	claim := handlers.Claim{Cfg: d.Cfg, Reserved: d.Reserved, Logger: d.Logger}
	mux.Handle("GET /claim", claim)
	mux.Handle("POST /claim", claim)
}

func registerAuth(mux *http.ServeMux, d Deps) {
	auth := handlers.Auth{Cfg: d.Cfg, SessionStore: d.SessionStore, Logger: d.Logger}
	mux.HandleFunc("GET /auth/login", auth.Login)
	mux.HandleFunc("GET /auth/callback", auth.Callback)
	mux.HandleFunc("POST /auth/logout", auth.Logout)
}

func registerPartials(mux *http.ServeMux, d Deps) {
	mux.Handle("GET /partials/app-status/{slug}", handlers.PartialAppStatus{
		Cfg:    d.Cfg,
		Argo:   d.Argo,
		Logger: d.Logger,
	})
	mux.Handle("GET /partials/compliance/{slug}", handlers.PartialCompliance{
		Cfg:   d.Cfg,
		Store: d.ComplianceStore,
	})
}
