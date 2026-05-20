package handlers

import (
	"net/http"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

// Docs renders the static onboarding + best-practice page at /docs.
// Pure markup; no per-request state.
type Docs struct{}

func (h Docs) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	props := views.PageProps{Title: "Docs", User: userFromCtx(r.Context())}
	render(w, r, views.Docs(props))
}
