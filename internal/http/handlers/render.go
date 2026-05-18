package handlers

import (
	"log/slog"
	"net/http"

	"github.com/a-h/templ"
)

// render writes a templ.Component as the response body, defaulting to
// text/html and a 200 unless the handler already set a status.
func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := c.Render(r.Context(), w); err != nil {
		slog.Default().Error("render", slog.Any("err", err))
	}
}
