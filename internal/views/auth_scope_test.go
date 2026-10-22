package views

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/ewwsauth"
)

func renderPanel(t *testing.T, p *AuthScopePanel) string {
	t.Helper()

	var buf bytes.Buffer
	if err := detailAuthScopePanel(p).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	return buf.String()
}

func TestDetailAuthScopePanel(t *testing.T) {
	if got := renderPanel(t, nil); strings.TrimSpace(got) != "" {
		t.Errorf("nil panel rendered %q", got)
	}

	ok := renderPanel(t, &AuthScopePanel{Name: "ewlt", State: AuthScopeOK, Scope: &ewwsauth.Scope{
		Name:        "ewlt",
		Description: "Werkzeugverwaltung",
		URLs: []ewwsauth.ScopeURL{
			{Environment: "staging", URL: "https://s.example"},
			{Environment: "prod", URL: "https://p.example", Label: "Live"},
		},
		Permissions: []ewwsauth.Permission{{Key: "ewlt:full", Name: "Vollzugriff"}},
	}})

	for _, want := range []string{
		"ewws-auth-Scope", "Werkzeugverwaltung", "<code>ewlt:full</code>", "Vollzugriff",
		`href="https://p.example"`, `rel="noopener noreferrer"`, `target="_blank"`, "Live",
	} {
		if !strings.Contains(ok, want) {
			t.Errorf("OK panel lacks %q", want)
		}
	}

	if strings.Index(ok, "https://p.example") > strings.Index(ok, "https://s.example") {
		t.Error("prod URLs must come before staging")
	}

	states := map[AuthScopeState]string{
		AuthScopeDisabled:    "ewws-auth nicht angebunden",
		AuthScopeNotFound:    "ist in ewws-auth nicht bekannt",
		AuthScopeUnreachable: "ewws-auth nicht erreichbar",
	}
	for state, want := range states {
		got := renderPanel(t, &AuthScopePanel{Name: "ewlt", State: state})
		if !strings.Contains(got, want) {
			t.Errorf("state %v: output lacks %q", state, want)
		}
	}
}
