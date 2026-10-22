package handlers

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/ewwsauth"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

func TestLoadAuthScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/scope-catalog/ewlt":
			_, _ = w.Write(
				[]byte(`{"name":"ewlt","description":"Werkzeug","urls":[],"permissions":[]}`),
			)
		case "/scope-catalog/unknown":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusBadGateway)
		}
	}))
	t.Cleanup(srv.Close)

	var logs bytes.Buffer

	enabled := Detail{
		EwwsAuth: ewwsauth.New(srv.URL, "platform-ui", "s3cret"),
		Logger:   slog.New(slog.NewTextHandler(&logs, nil)),
	}
	disabled := Detail{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	cases := []struct {
		name  string
		h     Detail
		scope string
		want  views.AuthScopeState
	}{
		{"ok", enabled, "ewlt", views.AuthScopeOK},
		{"not found", enabled, "unknown", views.AuthScopeNotFound},
		{"unreachable", enabled, "broken", views.AuthScopeUnreachable},
		{"disabled", disabled, "ewlt", views.AuthScopeDisabled},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.h.loadAuthScope(context.Background(), "slug", tc.scope)
			if p == nil {
				t.Fatal("panel is nil")
			}

			if p.State != tc.want {
				t.Errorf("state = %v, want %v", p.State, tc.want)
			}

			if tc.want == views.AuthScopeOK &&
				(p.Scope == nil || p.Scope.Description != "Werkzeug") {
				t.Errorf("scope = %+v", p.Scope)
			}
		})
	}

	if p := enabled.loadAuthScope(context.Background(), "slug", ""); p != nil {
		t.Errorf("no scope in claim must yield nil panel, got %+v", p)
	}

	if strings.Contains(logs.String(), "s3cret") {
		t.Error("log output leaks the client secret")
	}

	if !strings.Contains(logs.String(), "ewws-auth scope lookup failed") {
		t.Error("unreachable lookup was not logged")
	}
}
