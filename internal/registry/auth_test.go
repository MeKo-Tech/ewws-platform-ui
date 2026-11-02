package registry

import (
	"regexp"
	"testing"
)

const claimBase = `apiVersion: platform.meko.dev/v1
kind: App
slug: material-order
owners:
  technical: alice
  organizational: bob
repo:
  url: https://github.com/MeKo-Tech/Material-order
stages:
  staging:
    host: material-order.staging.example
    auto_deploy_branch: main
  prod:
    host: material-order.example
    auto_deploy_branch: release
`

func TestParseYAMLAuthScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		yaml string
		want string
	}{
		{name: "without auth", yaml: claimBase, want: ""},
		{name: "with auth scope", yaml: claimBase + "auth:\n  scope: ewlt\n", want: "ewlt"},
		{name: "empty auth block", yaml: claimBase + "auth: {}\n", want: ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			app, err := ParseYAML([]byte(tc.yaml))
			if err != nil {
				t.Fatalf("ParseYAML: %v", err)
			}

			if got := app.AuthScope(); got != tc.want {
				t.Errorf("AuthScope() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAuthScopeNilSafe(t *testing.T) {
	t.Parallel()

	var app App
	if got := app.AuthScope(); got != "" {
		t.Errorf("AuthScope() on zero App = %q, want empty", got)
	}
}

func TestRenderYAMLOmitsAuthWhenUnset(t *testing.T) {
	t.Parallel()

	in := frontendOnlyInput()

	out, err := in.ToApp().RenderYAML()
	if err != nil {
		t.Fatalf("RenderYAML: %v", err)
	}

	// Top-level key only: a nested key such as features.ingress_auth in an
	// existing claim legitimately contains "auth:".
	if regexp.MustCompile(`(?m)^auth:`).Match(out) {
		t.Errorf("rendered claim contains an auth key although none was set:\n%s", out)
	}

	back, err := ParseYAML(out)
	if err != nil {
		t.Fatalf("ParseYAML(round trip): %v", err)
	}

	if back.Auth != nil {
		t.Errorf("round trip Auth = %+v, want nil", back.Auth)
	}
}

func TestRenderYAMLKeepsAuthScope(t *testing.T) {
	t.Parallel()

	in := frontendOnlyInput()
	in.AuthScope = "ewlt"

	out, err := in.ToApp().RenderYAML()
	if err != nil {
		t.Fatalf("RenderYAML: %v", err)
	}

	back, err := ParseYAML(out)
	if err != nil {
		t.Fatalf("ParseYAML(round trip): %v", err)
	}

	if got := back.AuthScope(); got != "ewlt" {
		t.Errorf("round trip AuthScope() = %q, want %q", got, "ewlt")
	}
}

func TestRepoDisplayName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{in: "https://github.com/MeKo-Tech/Material-order", want: "MeKo-Tech/Material-order"},
		{in: "https://github.com/MeKo-Tech/Material-order/", want: "MeKo-Tech/Material-order"},
		{in: "https://github.com/MeKo-Tech/Material-order.git", want: "MeKo-Tech/Material-order"},
		{in: "  https://github.com/MeKo-Tech/x  ", want: "MeKo-Tech/x"},
		{in: "https://gitlab.com/foo/bar", want: "https://gitlab.com/foo/bar"},
		{in: "https://github.com/MeKo-Tech", want: "https://github.com/MeKo-Tech"},
		{in: "", want: ""},
	}

	for _, tc := range cases {
		if got := RepoDisplayName(tc.in); got != tc.want {
			t.Errorf("RepoDisplayName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Existing claims may still carry features.ingress_auth. Reading them must
// neither fail nor lose the value or the neighbouring features.
func TestParseYAMLKeepsIngressAuth(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"none", "github-org", "ewws-auth"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()

			app, err := ParseYAML([]byte(claimBase +
				"features:\n  websocket: true\n  ingress_auth: " + value + "\n"))
			if err != nil {
				t.Fatalf("ParseYAML: %v", err)
			}

			if app.Features.IngressAuth != value || !app.Features.Websocket {
				t.Errorf("Features = %+v, want ingress_auth %q and websocket", app.Features, value)
			}
		})
	}
}

func TestRenderYAMLOmitsIngressAuth(t *testing.T) {
	t.Parallel()

	in := fullInput()

	out, err := in.ToApp().RenderYAML()
	if err != nil {
		t.Fatalf("RenderYAML: %v", err)
	}

	if regexp.MustCompile(`(?m)^\s*ingress_auth:`).Match(out) {
		t.Errorf("rendered claim contains ingress_auth:\n%s", out)
	}
}
