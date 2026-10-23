package views

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/status"
)

func renderLanding(t *testing.T, tenants []status.Tenant) string {
	t.Helper()

	var buf bytes.Buffer
	// Anonymous user on purpose: the header only links /claim for a
	// logged-in user, so any href="/claim" here comes from the CTA.
	page := Landing(PageProps{Title: "Apps"}, tenants)
	if err := page.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	return buf.String()
}

func TestLandingNewAppCTA(t *testing.T) {
	cases := map[string][]status.Tenant{
		"no tenants": nil,
		"with tenants": {{
			Slug: "demo",
			Stages: map[string]status.Stage{
				"prod":    {Stage: "prod", Host: "demo.apps.meko.work"},
				"staging": {Stage: "staging", Host: "demo.staging.apps.meko.work"},
			},
		}},
	}

	for name, tenants := range cases {
		t.Run(name, func(t *testing.T) {
			html := renderLanding(t, tenants)

			for _, want := range []string{
				`href="/claim"`,
				`class="btn-primary"`,
				"Neue App",
				`href="` + onboardingGuideURL + `"`,
				"Onboarding-Anleitung",
			} {
				if !strings.Contains(html, want) {
					t.Errorf("landing page lacks %q", want)
				}
			}
		})
	}
}

func TestLandingEmptyStatesLinkClaim(t *testing.T) {
	html := renderLanding(t, nil)

	// Hero + cards empty state + table empty state.
	if got := strings.Count(html, `href="/claim"`); got != 3 {
		t.Errorf(`href="/claim" count = %d, want 3`, got)
	}

	if !strings.Contains(html, "Noch keine Tenants geclaimt.") {
		t.Error("empty state text missing")
	}
}
