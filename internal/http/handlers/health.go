// Package handlers contains the HTTP handlers wired into the server.
package handlers

import (
	"net/http"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
)

// Health is the trivial /healthz handler — always 200.
type Health struct{}

func (Health) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

// Ready returns 200 once OAuth config is present (required for full feature parity).
type Ready struct {
	Cfg *config.Config
}

func (h Ready) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	if h.Cfg == nil || !h.Cfg.OAuthReady() {
		http.Error(w, "oauth config missing", http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ready"))
}
