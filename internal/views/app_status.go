// Package views — types shared between the partial HTMX handlers and
// the badge templates. Kept in a plain .go file so they don't get
// rewritten every time `templ generate` touches the directory.
package views

import "github.com/MeKo-Tech/ewws-platform-ui/internal/argocd"

// AppStatus is the small cell rendered by /partials/app-status/<slug>.
// Used by status_cell.templ + the partial handler that polls Argo CD.
type AppStatus struct {
	Sync       string
	Health     string
	LastDeploy string
	StageName  string // "staging" | "prod"
	Found      bool
}

// StatusFromArgo derives an AppStatus from an *argocd.Application.
func StatusFromArgo(app *argocd.Application, stage string) AppStatus {
	if app == nil {
		return AppStatus{StageName: stage}
	}

	out := AppStatus{
		Sync:      app.Status.Sync.Status,
		Health:    app.Status.Health.Status,
		StageName: stage,
		Found:     true,
	}

	if !app.Status.OperationState.FinishedAt.IsZero() {
		out.LastDeploy = app.Status.OperationState.FinishedAt.Format("2006-01-02 15:04")
	}

	return out
}
