package views

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func renderDocs(t *testing.T) string {
	t.Helper()

	var buf bytes.Buffer
	if err := Docs(PageProps{Title: "Docs"}).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	return buf.String()
}

// TestDocsDescribesCurrentFlow pins the parts of the onboarding flow the
// page must carry: SealedSecrets, the own client_id, the EWWS steps and the
// links to the detailed guides.
func TestDocsDescribesCurrentFlow(t *testing.T) {
	html := renderDocs(t)

	for _, want := range []string{
		"go-react-template",
		"just secrets-seal",
		"sops-key-vibe",
		"meko-vibe-vault",
		"sealed_secrets",
		"auth.client_ids",
		"oidcPublicClients",
		"sync-tagger-secret-repos.sh",
		"vibe-app-repos",
		"resource_exception",
		"@MeKo-Tech/ewws",
		"tenant-&lt;slug&gt;-&lt;stage&gt;",
		"platform://guide/new-app",
		"secrets.VIBE_CODE_KEY_VAULT_CLIENT_SECRET",
		docsRunbookURL,
		docsOnboardingURL,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("docs page misses %q", want)
		}
	}
}

// TestDocsDropsRetiredFlow guards against the pre-SealedSecrets flow
// creeping back: per-repo Azure Key Vault pinning, the plaintext
// values.sops.env scaffold and helm deploys from the app repo.
func TestDocsDropsRetiredFlow(t *testing.T) {
	html := renderDocs(t)

	for _, unwanted := range []string{
		"sops --encrypt --in-place",
		"az login",
		"image-updater",
		"ExternalSecret",
		"values.staging.sops.env",
	} {
		if strings.Contains(html, unwanted) {
			t.Errorf("docs page still mentions %q", unwanted)
		}
	}
}
