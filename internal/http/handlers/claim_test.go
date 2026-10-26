package handlers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	ghclient "github.com/MeKo-Tech/ewws-platform-ui/internal/github"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

func renderClaimForm(t *testing.T, prev views.ClaimFormState) string {
	t.Helper()

	var buf bytes.Buffer

	page := views.PageProps{Title: "Claim"}
	if err := views.Claim(page, claimProps("octo", "", "tok", prev)).
		Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	return buf.String()
}

func TestClaimFormFields(t *testing.T) {
	html := renderClaimForm(t, views.ClaimFormState{})

	for _, name := range []string{
		"slug", "repo_url", "has_backend", "has_frontend",
		"backend_image_repo", "backend_image_tag", "backend_memory_limit", "backend_cpu_limit",
		"backend_port", "backend_liveness_path", "backend_readiness_path", "backend_startup_path",
		"backend_env", "backend_env_from",
		"frontend_image_repo", "frontend_image_tag", "frontend_port", "frontend_env", "frontend_env_from",
		"staging_host", "staging_enabled", "staging_backend_env", "staging_frontend_env",
		"prod_host", "prod_enabled", "prod_backend_env", "prod_frontend_env",
		"auth_scope", "websocket", "ingress_auth", "shared_secrets",
	} {
		if !strings.Contains(html, `name="`+name+`"`) {
			t.Errorf("form lacks field %q", name)
		}
	}

	// ExternalSecrets (claim `secrets`, Key Vault meko-apps) were removed
	// from the platform; the form must not offer them any more.
	for _, gone := range []string{`name="secrets"`, "tenant-keyvault", "Vault-Suffix"} {
		if strings.Contains(html, gone) {
			t.Errorf("form still contains %q", gone)
		}
	}

	// The pre-rollout "enabled: false" caveat is gone since chart 1.10.0 is live.
	if strings.Contains(html, "Plattform-Rollout") {
		t.Error("form still shows the obsolete enabled:false rollout hint")
	}

	for _, want := range []string{
		"Nur Secret-Namen — niemals Secret-Werte eintragen.",
		"wird ignoriert, wenn die Komponente oben nicht angehakt ist",
		"onboarding.md#8-login-über-ewws-auth",
		"onboarding.md#7-secrets",
		"go-react-template/blob/main/AGENTS.md#secrets",
		`<option value="none" selected>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("form lacks %q", want)
		}
	}

	// Before the first submit both stages default to enabled.
	if n := strings.Count(html, `_enabled" value="1" checked`); n != 2 {
		t.Errorf("checked stage boxes = %d, want 2", n)
	}
}

func TestClaimFormKeepsValues(t *testing.T) {
	html := renderClaimForm(t, views.ClaimFormState{
		Submitted:     true,
		Slug:          "my-app",
		Staging:       views.ClaimStageFormState{Enabled: true, BackendEnv: "A=1\nB=secret:s/k"},
		Prod:          views.ClaimStageFormState{Enabled: false},
		SharedSecrets: "ipoffice-readonly",
		Backend:       views.ClaimComponentFormState{Port: "abc"},
	})

	for _, want := range []string{"A=1\nB=secret:s/k</textarea>", `value="ipoffice-readonly"`, `value="abc"`} {
		if !strings.Contains(html, want) {
			t.Errorf("re-rendered form lacks %q", want)
		}
	}

	if n := strings.Count(html, `_enabled" value="1" checked`); n != 1 {
		t.Errorf("checked stage boxes = %d, want 1 (prod unchecked)", n)
	}
}

func baseClaimForm() url.Values {
	return url.Values{
		"slug":                  {"my-app"},
		"repo_url":              {"https://github.com/MeKo-Tech/my-app"},
		"has_backend":           {"1"},
		"has_frontend":          {"1"},
		"backend_image_repo":    {"ghcr.io/meko-tech/my-app-backend"},
		"backend_image_tag":     {"1.0.0"},
		"backend_memory_limit":  {"256Mi"},
		"backend_cpu_limit":     {"250m"},
		"frontend_image_repo":   {"ghcr.io/meko-tech/my-app-frontend"},
		"frontend_image_tag":    {"sha-abc1234"},
		"frontend_memory_limit": {"128Mi"},
		"frontend_cpu_limit":    {"100m"},
		"staging_host":          {"my-app.staging.apps.meko.work"},
		"staging_enabled":       {"1"},
		"prod_host":             {"my-app.apps.meko.work"},
		"prod_enabled":          {"1"},
		"ingress_auth":          {"none"},
	}
}

var testUser = &middleware.SessionUser{Login: "octo"}

func TestParseClaimFormFrontendOnly(t *testing.T) {
	form := baseClaimForm()
	form.Del("has_backend")

	in, st, err := parseClaimForm(form, testUser)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if in.HasBackend || !in.HasFrontend || st.HasBackend {
		t.Errorf("components = backend %t frontend %t", in.HasBackend, in.HasFrontend)
	}

	if err := in.Validate(nil); err != nil {
		t.Fatalf("validate: %v", err)
	}

	files, err := claimFiles(in)
	if err != nil {
		t.Fatalf("claimFiles: %v", err)
	}

	for _, p := range []string{"apps/my-app.yaml", "tags/my-app/staging.yaml", "tags/my-app/prod.yaml"} {
		if len(files[p]) == 0 {
			t.Errorf("missing %s", p)
		}
	}

	if strings.Contains(string(files["apps/my-app.yaml"]), "my-app-backend") {
		t.Errorf("frontend-only claim renders backend:\n%s", files["apps/my-app.yaml"])
	}

	body := claimPRBody("octo", in, files)
	for _, want := range []string{
		"- components: frontend\n", "- frontend: `ghcr.io/meko-tech/my-app-frontend:sha-abc1234`",
		"`tags/my-app/prod.yaml`", "write-registry-tag", "platform rollout",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("PR body lacks %q:\n%s", want, body)
		}
	}

	if strings.Contains(body, "- backend:") {
		t.Errorf("PR body lists the absent backend:\n%s", body)
	}
}

func TestParseClaimFormProdDisabled(t *testing.T) {
	form := baseClaimForm()
	form.Del("prod_enabled")

	in, st, err := parseClaimForm(form, testUser)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if in.Prod.Enabled || st.Prod.Enabled || !in.Staging.Enabled {
		t.Errorf("stages = staging %t prod %t", in.Staging.Enabled, in.Prod.Enabled)
	}

	if err := in.Validate(nil); err != nil {
		t.Fatalf("validate: %v", err)
	}

	files, err := claimFiles(in)
	if err != nil {
		t.Fatalf("claimFiles: %v", err)
	}

	if _, ok := files["tags/my-app/prod.yaml"]; ok {
		t.Error("disabled prod stage got a tag file")
	}

	if !strings.Contains(string(files["apps/my-app.yaml"]), "enabled: false") {
		t.Errorf("claim lacks enabled: false:\n%s", files["apps/my-app.yaml"])
	}
}

func TestParseClaimFormEnvWithSecretRef(t *testing.T) {
	form := baseClaimForm()
	form.Set("shared_secrets", "ipoffice-readonly, my-app-db")
	form.Set("backend_env", "# comment\nLOG_LEVEL=info\nDB_PASSWORD=secret:my-app-db/DB_PASSWORD\n")
	form.Set("backend_env_from", "ipoffice-readonly")
	form.Set("backend_port", "9090")
	form.Set("staging_backend_env", "LOG_LEVEL=debug")
	form.Set("auth_scope", "my-app")
	form.Set("websocket", "1")

	in, _, err := parseClaimForm(form, testUser)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	want := []registry.EnvVar{
		{Name: "LOG_LEVEL", Value: "info"},
		{Name: "DB_PASSWORD", Secret: "my-app-db", Key: "DB_PASSWORD"},
	}
	if len(in.Backend.Env) != 2 || in.Backend.Env[0] != want[0] || in.Backend.Env[1] != want[1] {
		t.Errorf("backend env = %+v", in.Backend.Env)
	}

	if in.Backend.Port != 9090 || in.AuthScope != "my-app" || !in.Websocket {
		t.Errorf("input = %+v", in)
	}

	if len(in.SharedSecrets) != 2 || in.SharedSecrets[1] != "my-app-db" ||
		len(in.Backend.EnvFromSecrets) != 1 ||
		len(in.Staging.BackendEnv) != 1 {
		t.Errorf("secrets/env_from/stage env = %+v", in)
	}

	if err := in.Validate(nil); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestParseClaimFormErrorsKeepValues(t *testing.T) {
	for _, tc := range []struct {
		name, field, value, wantErr string
	}{
		{"bad port", "backend_port", "80a", "backend port"},
		{"bad env line", "frontend_env", "NOVALUE", "frontend env"},
		{"bad stage env", "prod_backend_env", "X=secret:nokey", "prod backend env"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			form := baseClaimForm()
			form.Set(tc.field, tc.value)

			in, st, err := parseClaimForm(form, testUser)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want %q", err, tc.wantErr)
			}

			if in != nil {
				t.Error("input must be nil on a parse error")
			}

			if st.Slug != "my-app" || !st.Submitted || !st.Prod.Enabled {
				t.Errorf("form state lost values: %+v", st)
			}

			html := renderClaimForm(t, st)
			if !strings.Contains(html, tc.value) {
				t.Errorf("re-rendered form lacks %q", tc.value)
			}
		})
	}
}

func TestParseClaimFormIgnoresUncheckedComponent(t *testing.T) {
	junk := map[string]string{
		"backend_env":         "NOVALUE",
		"backend_port":        "abc",
		"staging_backend_env": "X=secret:nokey",
		"prod_backend_env":    "garbage",
	}

	form := baseClaimForm()
	form.Del("has_backend")

	for k, v := range junk {
		form.Set(k, v)
	}

	in, st, err := parseClaimForm(form, testUser)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if err := in.Validate(nil); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if in.Backend.Port != 0 || len(in.Backend.Env) != 0 ||
		len(in.Staging.BackendEnv) != 0 || len(in.Prod.BackendEnv) != 0 {
		t.Errorf("unchecked backend leaked into the input: %+v", in)
	}

	got := map[string]string{
		"backend_env":         st.Backend.Env,
		"backend_port":        st.Backend.Port,
		"staging_backend_env": st.Staging.BackendEnv,
		"prod_backend_env":    st.Prod.BackendEnv,
	}
	for k, v := range junk {
		if got[k] != v {
			t.Errorf("form state %s = %q, want raw %q", k, got[k], v)
		}
	}
}

func TestClaimFormDisablesUncheckedComponent(t *testing.T) {
	html := renderClaimForm(t, views.ClaimFormState{
		Submitted:   true,
		HasFrontend: true,
		Backend:     views.ClaimComponentFormState{Port: "80"},
		Staging:     views.ClaimStageFormState{BackendEnv: "LEFT=over"},
	})

	for _, want := range []string{
		`<fieldset data-claim-component="backend" disabled>`,
		`<fieldset data-claim-component="frontend">`,
		`name="staging_backend_env" data-claim-component="backend" disabled`,
		`name="staging_frontend_env" data-claim-component="frontend" rows=`,
		`data-claim-toggle="backend"`,
		`<script src="/static/claim-form.js" defer></script>`,
		// Values of the disabled component survive the re-render.
		`value="80"`,
		"LEFT=over</textarea>",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("form lacks %q", want)
		}
	}

	// First GET: both components default to checked, nothing disabled.
	if html := renderClaimForm(t, views.ClaimFormState{}); strings.Contains(html, " disabled") {
		t.Error("initial form renders disabled inputs")
	}
}

func TestClaimExistsMessage(t *testing.T) {
	err := fmt.Errorf("open: %w", &ghclient.ClaimExistsError{
		Slug: "my-app", Path: "apps/my-app.yaml", Branch: "main",
	})

	msg, ok := claimExistsMessage(err)
	if !ok || msg != "Slug my-app ist bereits geclaimt (apps/my-app.yaml existiert auf main)" {
		t.Errorf("message = %q, %t", msg, ok)
	}

	if _, ok := claimExistsMessage(errors.New("other")); ok {
		t.Error("unrelated error mapped to claim-exists")
	}

	if _, ok := claimExistsMessage(nil); ok {
		t.Error("nil mapped to claim-exists")
	}
}
