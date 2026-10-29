package registry

// The schemas under testdata/schema are vendored from MeKo-Tech/ewws-apps-registry
// at commit 8eb676bfa026c7b684ec1f16475e57bedc8e6c80 (schemas last changed in
// 471e49a91c68e6cc8acc56345f78f33df6bb0166, registry PR #145). Refresh them with:
//
//	sha=<commit>
//	gh api "repos/MeKo-Tech/ewws-apps-registry/contents/schemas/app.schema.json?ref=$sha" \
//	  -H 'Accept: application/vnd.github.raw' > internal/registry/testdata/schema/app.schema.json
//	gh api "repos/MeKo-Tech/ewws-apps-registry/contents/schemas/tag.schema.json?ref=$sha" \
//	  -H 'Accept: application/vnd.github.raw' > internal/registry/testdata/schema/tag.schema.json
//
// and update the commit above.
//
// Local edit on top of that commit: images.<c> (definitions.imageRef) no
// longer has `tag`, and tag.schema.json no longer names the claim fallback.
// This mirrors the registry PR that makes tags/<slug>/<stage>.yaml the only
// source of the image tag; refresh from the registry once it has landed.
//
// Golden files are regenerated with
//
//	go test ./internal/registry -run TestClaimGolden -update

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/dlclark/regexp2"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

var update = flag.Bool("update", false, "rewrite the golden files under testdata/golden")

const (
	appSchemaID = "https://github.com/MeKo-Tech/ewws-apps-registry/schemas/app.schema.json"
	tagSchemaID = "https://github.com/MeKo-Tech/ewws-apps-registry/schemas/tag.schema.json"
)

// ecmaRegexp adapts regexp2 (which supports the lookahead the schema uses,
// e.g. (?!latest$)) to jsonschema.Regexp; ajv uses JavaScript regexps.
type ecmaRegexp struct{ re *regexp2.Regexp }

func (r ecmaRegexp) MatchString(s string) bool {
	ok, err := r.re.MatchString(s)
	return err == nil && ok
}

func (r ecmaRegexp) String() string { return r.re.String() }

func ecmaRegexpEngine(pattern string) (jsonschema.Regexp, error) {
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}

	return ecmaRegexp{re: re}, nil
}

// compileSchemas compiles the vendored schemas like the registry's ajv call:
// draft-07, formats asserted (ajv-formats), ECMAScript regexps.
func compileSchemas(t *testing.T) (app, tag *jsonschema.Schema) {
	t.Helper()

	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft7)
	c.AssertFormat()
	c.UseRegexpEngine(ecmaRegexpEngine)

	for id, file := range map[string]string{
		appSchemaID: "testdata/schema/app.schema.json",
		tagSchemaID: "testdata/schema/tag.schema.json",
	} {
		f, err := os.Open(file)
		if err != nil {
			t.Fatalf("open %s: %v", file, err)
		}

		doc, err := jsonschema.UnmarshalJSON(f)
		_ = f.Close()

		if err != nil {
			t.Fatalf("parse %s: %v", file, err)
		}

		if err := c.AddResource(id, doc); err != nil {
			t.Fatalf("add %s: %v", file, err)
		}
	}

	app, err := c.Compile(appSchemaID)
	if err != nil {
		t.Fatalf("compile app schema: %v", err)
	}

	tag, err = c.Compile(tagSchemaID)
	if err != nil {
		t.Fatalf("compile tag schema: %v", err)
	}

	return app, tag
}

// yamlToJSON mirrors `yq -o=json`: the YAML document as JSON, quoted dates
// staying strings.
func yamlToJSON(t *testing.T, data []byte) (any, map[string]any) {
	t.Helper()

	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("yaml: %v\n%s", err, data)
	}

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json: %v", err)
	}

	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("jsonschema unmarshal: %v", err)
	}

	return inst, doc
}

func frontendOnlyInput() ClaimInput {
	return ClaimInput{
		Slug:                "material-order",
		OwnerTechnical:      "alice",
		OwnerOrganizational: "bob",
		RepoURL:             "https://github.com/MeKo-Tech/Material-order",
		HasFrontend:         true,
		Frontend: ComponentInput{
			ImageRepository: "ghcr.io/meko-tech/material-order/frontend",
			ImageTag:        "1.0.0",
			MemoryLimit:     "128Mi",
			CPULimit:        "100m",
		},
		Staging:   StageInput{Host: "material-order.apps.meko.dev", Enabled: true},
		Prod:      StageInput{Host: "material-order.apps.meko.work", Enabled: true},
		CreatedAt: "2026-10-23",
	}
}

func bothInput() ClaimInput {
	in := frontendOnlyInput()
	in.HasBackend = true
	in.Backend = ComponentInput{
		ImageRepository: "ghcr.io/meko-tech/material-order/backend",
		ImageTag:        "sha-c758f9c",
		MemoryLimit:     "256Mi",
		CPULimit:        "1",
	}

	return in
}

func fullInput() ClaimInput {
	in := bothInput()
	in.AuthScope = "ewlt"
	in.Websocket = true
	in.IngressAuth = "ewws-auth"
	in.SharedSecrets = []string{"smtp-relay", "app-db", "app-api"}
	in.Backend.Port = 8080
	in.Backend.Env = []EnvVar{
		{Name: "PORT", Value: "8080"},
		{Name: "FEATURE_X", Value: "true"},
		{Name: "EMPTY", Value: ""},
		{Name: "SMTP_PASSWORD", Secret: "smtp-relay", Key: "password"},
	}
	in.Backend.EnvFromSecrets = []string{"app-db", "app-api"}
	in.Backend.LivenessPath = "/healthz"
	in.Backend.ReadinessPath = "/readyz?full=1"
	in.Frontend.ReadinessPath = "/"
	in.Staging.BackendEnv = []EnvVar{
		{Name: "LOG_LEVEL", Value: "debug"},
		{Name: "DB_PASSWORD", Secret: "app-db", Key: "DB_PASSWORD"},
	}
	in.Prod.FrontendEnv = []EnvVar{{Name: "PUBLIC_ENV", Value: "prod"}}

	return in
}

func claimCases() []struct {
	name string
	in   ClaimInput
} {
	withAuth := frontendOnlyInput()
	withAuth.AuthScope = "material.order"

	withSecrets := bothInput()
	withSecrets.SharedSecrets = []string{"smtp-relay", "ldap-bind", "app-db"}
	withSecrets.Backend.Env = []EnvVar{
		{Name: "LDAP_PASSWORD", Secret: "ldap-bind", Key: "bind.password"},
		{Name: "DB", Secret: "app-db", Key: "PASSWORD"},
	}
	withSecrets.Backend.EnvFromSecrets = []string{"smtp-relay", "app-db"}

	prodDisabled := bothInput()
	prodDisabled.Prod.Enabled = false

	stagingDisabled := frontendOnlyInput()
	stagingDisabled.Staging.Enabled = false

	stageEnv := bothInput()
	stageEnv.SharedSecrets = []string{"app-db"}
	stageEnv.Staging.BackendEnv = []EnvVar{
		{Name: "MODE", Value: "staging"},
		{Name: "DB", Secret: "app-db", Key: "PASSWORD"},
	}
	stageEnv.Prod.FrontendEnv = []EnvVar{{Name: "MODE", Value: "123"}}

	probes := bothInput()
	probes.Backend.Port = 8081
	probes.Backend.LivenessPath = "/health"
	probes.Backend.StartupPath = "/startup"
	probes.Frontend.Port = 65535
	probes.Frontend.ReadinessPath = "/ready"

	// Backend settings filled in but the backend not enabled: ignored.
	ignored := frontendOnlyInput()
	ignored.Backend = ComponentInput{
		ImageRepository: "x",
		ImageTag:        "latest",
		Port:            80,
		Env:             []EnvVar{{Name: "1BAD", Value: "x"}},
	}
	ignored.Staging.BackendEnv = []EnvVar{{Name: "A", Value: "b"}}

	return []struct {
		name string
		in   ClaimInput
	}{
		{"frontend-only", frontendOnlyInput()},
		{"backend-frontend", bothInput()},
		{"backend-frontend-full", fullInput()},
		{"with-auth-scope", withAuth},
		{"with-secrets", withSecrets},
		{"prod-disabled", prodDisabled},
		{"staging-disabled", stagingDisabled},
		{"per-stage-env", stageEnv},
		{"probes-and-port", probes},
		{"disabled-component-settings-ignored", ignored},
	}
}

func TestClaimMatchesRegistrySchema(t *testing.T) {
	t.Parallel()

	appSchema, tagSchema := compileSchemas(t)

	for _, tc := range claimCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if err := tc.in.Validate(nil); err != nil {
				t.Fatalf("Validate: %v", err)
			}

			out, err := tc.in.ToApp().RenderYAML()
			if err != nil {
				t.Fatalf("RenderYAML: %v", err)
			}

			inst, doc := yamlToJSON(t, out)
			if err := appSchema.Validate(inst); err != nil {
				t.Fatalf("app schema: %v\n%s", err, out)
			}

			assertScriptRules(t, &tc.in, doc, out)

			tags, err := tc.in.TagFiles()
			if err != nil {
				t.Fatalf("TagFiles: %v", err)
			}

			assertTagFiles(t, &tc.in, tags, tagSchema)
		})
	}
}

// assertScriptRules checks the rules scripts/validate-claims.sh adds on top
// of the schema.
func assertScriptRules(t *testing.T, in *ClaimInput, doc map[string]any, out []byte) {
	t.Helper()

	var enabled []string
	if in.HasBackend {
		enabled = append(enabled, componentBackend)
	}

	if in.HasFrontend {
		enabled = append(enabled, componentFrontend)
	}

	for _, block := range []string{"images", "resources"} {
		if got := keys(doc[block]); !slices.Equal(got, enabled) {
			t.Errorf("%s keys = %v, want %v", block, got, enabled)
		}
	}

	for _, c := range enabled {
		img, _ := doc["images"].(map[string]any)[c].(map[string]any)
		if got := keys(img); !slices.Equal(got, []string{"repository"}) {
			t.Errorf("images.%s keys = %v, want [repository] (the tag lives in tags/)", c, got)
		}
	}

	for _, c := range []string{componentBackend, componentFrontend} {
		if slices.Contains(enabled, c) {
			continue
		}

		if comps, ok := doc["components"].(map[string]any); ok && comps[c] != nil {
			t.Errorf("components.%s set for a disabled component", c)
		}

		for _, s := range []string{stageStaging, stageProd} {
			stage := doc["stages"].(map[string]any)[s].(map[string]any)
			if comps, ok := stage["components"].(map[string]any); ok && comps[c] != nil {
				t.Errorf("stages.%s.components.%s set for a disabled component", s, c)
			}
		}
	}

	active := 0

	for _, s := range doc["stages"].(map[string]any) {
		if e, ok := s.(map[string]any)["enabled"]; !ok || e != false {
			active++
		}
	}

	if active == 0 {
		t.Error("no stage is enabled")
	}

	known := map[string]bool{}
	for _, s := range in.SharedSecrets {
		known[s] = true
	}

	for _, ref := range secretRefs(doc) {
		if !known[ref] {
			t.Errorf("env refers to secret %q that is not in shared_secrets", ref)
		}
	}

	if bytes.Contains(out, []byte("auto_deploy_branch")) {
		t.Errorf("rendered claim contains auto_deploy_branch:\n%s", out)
	}

	if bytes.Contains(out, []byte("enabled: true")) {
		t.Errorf("rendered claim contains enabled: true:\n%s", out)
	}
}

// secretRefs collects the secret of every {secret, key} env value.
func secretRefs(doc map[string]any) []string {
	var refs []string

	collect := func(comps any) {
		m, _ := comps.(map[string]any)
		for _, comp := range m {
			env, _ := comp.(map[string]any)["env"].(map[string]any)
			for _, v := range env {
				if ref, ok := v.(map[string]any); ok {
					refs = append(refs, ref["secret"].(string))
				}
			}
		}
	}

	collect(doc["components"])

	for _, s := range doc["stages"].(map[string]any) {
		collect(s.(map[string]any)["components"])
	}

	return refs
}

func assertTagFiles(
	t *testing.T,
	in *ClaimInput,
	tags map[string][]byte,
	tagSchema *jsonschema.Schema,
) {
	t.Helper()

	var wantPaths []string
	if in.Staging.Enabled {
		wantPaths = append(wantPaths, "tags/"+in.Slug+"/staging.yaml")
	}

	if in.Prod.Enabled {
		wantPaths = append(wantPaths, "tags/"+in.Slug+"/prod.yaml")
	}

	sort.Strings(wantPaths)

	if got := keys(map[string]any(toAny(tags))); !slices.Equal(got, wantPaths) {
		t.Errorf("tag files = %v, want %v", got, wantPaths)
	}

	for path, content := range tags {
		inst, doc := yamlToJSON(t, content)
		if err := tagSchema.Validate(inst); err != nil {
			t.Errorf("%s: tag schema: %v\n%s", path, err, content)
		}

		got := keys(doc["imageTags"])

		var want []string
		if in.HasBackend {
			want = append(want, componentBackend)
		}

		if in.HasFrontend {
			want = append(want, componentFrontend)
		}

		if !slices.Equal(got, want) {
			t.Errorf("%s: imageTags keys = %v, want %v", path, got, want)
		}
	}
}

func toAny(m map[string][]byte) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}

	return out
}

func keys(v any) []string {
	m, _ := v.(map[string]any)

	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}

	sort.Strings(out)

	return out
}

// TestSchemaHarnessRejects proves the harness is strict where it matters:
// lookahead patterns, formats and additionalProperties are enforced.
func TestSchemaHarnessRejects(t *testing.T) {
	t.Parallel()

	appSchema, tagSchema := compileSchemas(t)

	in := frontendOnlyInput()

	good, err := in.ToApp().RenderYAML()
	if err != nil {
		t.Fatalf("RenderYAML: %v", err)
	}

	for name, mutate := range map[string]func(string) string{
		"image tag in the claim": func(s string) string {
			return strings.Replace(s,
				"repository: ghcr.io/meko-tech/material-order/frontend\n",
				"repository: ghcr.io/meko-tech/material-order/frontend\n"+
					"    tag:\n      staging: \"1.0.0\"\n      prod: \"1.0.0\"\n", 1)
		},
		"invalid date": func(s string) string {
			return strings.Replace(s, `"2026-10-23"`, `"2026-02-30"`, 1)
		},
		"invalid hostname": func(s string) string {
			return strings.Replace(s, "material-order.apps.meko.dev", "-bad_host.apps.meko.dev", 1)
		},
		"deprecated key typo": func(s string) string {
			return strings.Replace(s, "slug:", "unknown: x\nslug:", 1)
		},
		"removed external secrets": func(s string) string {
			return strings.Replace(s, "slug:", "secrets:\n  - name: app-db\n    data:\n      A: a\nslug:", 1)
		},
	} {
		inst, _ := yamlToJSON(t, []byte(mutate(string(good))))
		if err := appSchema.Validate(inst); err == nil {
			t.Errorf("%s: app schema accepted the claim", name)
		}
	}

	inst, _ := yamlToJSON(t, []byte("imageTags:\n  frontend: \"latest\"\n"))
	if err := tagSchema.Validate(inst); err == nil {
		t.Error("tag schema accepted latest")
	}
}

func TestClaimGolden(t *testing.T) {
	t.Parallel()

	for name, in := range map[string]ClaimInput{
		"frontend-only":         frontendOnlyInput(),
		"backend-frontend-full": fullInput(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			out, err := in.ToApp().RenderYAML()
			if err != nil {
				t.Fatalf("RenderYAML: %v", err)
			}

			files := map[string][]byte{name + ".yaml": out}

			tags, err := in.TagFiles()
			if err != nil {
				t.Fatalf("TagFiles: %v", err)
			}

			for path, content := range tags {
				stage := strings.TrimSuffix(filepath.Base(path), ".yaml")
				files[name+".tags."+stage+".yaml"] = content
			}

			for file, got := range files {
				path := filepath.Join("testdata", "golden", file)

				if *update {
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}

					if err := os.WriteFile(path, got, 0o600); err != nil {
						t.Fatal(err)
					}

					continue
				}

				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden (run with -update): %v", err)
				}

				if !bytes.Equal(got, want) {
					t.Errorf("%s differs from golden\n--- got\n%s\n--- want\n%s", file, got, want)
				}
			}
		})
	}
}

func TestValidateRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		mutate func(*ClaimInput)
		want   string
	}{
		"latest tag": {
			func(c *ClaimInput) { c.Frontend.ImageTag = "latest" },
			"imageTags.frontend may not be latest",
		},
		"bad tag": {
			func(c *ClaimInput) { c.Frontend.ImageTag = ".1" },
			"imageTags.frontend",
		},
		"no component": {
			func(c *ClaimInput) { c.HasFrontend = false },
			"enable at least one component",
		},
		"both stages disabled": {
			func(c *ClaimInput) { c.Staging.Enabled, c.Prod.Enabled = false, false },
			"enable at least one stage",
		},
		"missing prod host on disabled stage": {
			func(c *ClaimInput) { c.Prod = StageInput{} },
			"stages.prod.host is required",
		},
		"upper-case host": {
			func(c *ClaimInput) { c.Staging.Host = "Material.apps.meko.dev" },
			"stages.staging.host",
		},
		"unknown secret ref": {
			func(c *ClaimInput) {
				c.Frontend.Env = []EnvVar{{Name: "X", Secret: "nope", Key: "k"}}
			},
			"components.frontend.env.X: secret \"nope\" is not in shared_secrets",
		},
		"unknown stage secret ref": {
			func(c *ClaimInput) {
				c.Prod.FrontendEnv = []EnvVar{{Name: "X", Secret: "nope", Key: "k"}}
			},
			"stages.prod.components.frontend.env.X",
		},
		"unknown env_from": {
			func(c *ClaimInput) { c.Frontend.EnvFromSecrets = []string{"nope"} },
			"components.frontend.env_from: secret \"nope\"",
		},
		"bad secret key": {
			func(c *ClaimInput) {
				c.SharedSecrets = []string{"smtp-relay"}
				c.Frontend.Env = []EnvVar{{Name: "X", Secret: "smtp-relay", Key: "..x"}}
			},
			"secret key \"..x\"",
		},
		"bad env name": {
			func(c *ClaimInput) { c.Frontend.Env = []EnvVar{{Name: "1BAD", Value: "x"}} },
			"\"1BAD\" is not a valid variable name",
		},
		"duplicate env name": {
			func(c *ClaimInput) {
				c.Frontend.Env = []EnvVar{{Name: "A", Value: "x"}, {Name: "A", Value: "y"}}
			},
			"A is set twice",
		},
		"cpu over cap": {
			func(c *ClaimInput) { c.Frontend.CPULimit = "4001m" },
			"resources.frontend.cpu_limit (4001m) exceeds the maximum of 4",
		},
		"memory over cap": {
			func(c *ClaimInput) { c.Frontend.MemoryLimit = "5Gi" },
			"resources.frontend.memory_limit (5Gi) exceeds the maximum of 4Gi",
		},
		"zero cpu": {
			func(c *ClaimInput) { c.Frontend.CPULimit = "0" },
			"must be greater than zero",
		},
		"low port": {
			func(c *ClaimInput) { c.Frontend.Port = 80 },
			"components.frontend.port 80",
		},
		"bad probe path": {
			func(c *ClaimInput) { c.Frontend.LivenessPath = "health" },
			"components.frontend.probes.liveness.path",
		},
		"bad owner": {
			func(c *ClaimInput) { c.OwnerTechnical = "alice-" },
			"owners.technical",
		},
		"http repo": {
			func(c *ClaimInput) { c.RepoURL = "http://github.com/x/y" },
			"repo.url",
		},
		"repo url with space in path": {
			func(c *ClaimInput) { c.RepoURL = "https://github.com/MeKo-Tech/my app" },
			"repo.url",
		},
		"repo url with space in query": {
			func(c *ClaimInput) { c.RepoURL = "https://github.com/MeKo-Tech/app?x=a b" },
			"repo.url",
		},
		"bad auth scope": {
			func(c *ClaimInput) { c.AuthScope = "-x" },
			"auth.scope",
		},
		"bad ingress auth": {
			func(c *ClaimInput) { c.IngressAuth = "basic" },
			"features.ingress_auth",
		},
		"missing image repository": {
			func(c *ClaimInput) { c.Frontend.ImageRepository = "" },
			"images.frontend.repository is required",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			in := frontendOnlyInput()
			tc.mutate(&in)

			err := in.Validate(nil)
			if err == nil {
				t.Fatalf("Validate accepted the input")
			}

			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Validate() = %q, want it to contain %q", err, tc.want)
			}
		})
	}
}

func TestValidateRepoURLPercentEncoded(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{
		"https://github.com/MeKo-Tech/my%20app",
		"https://github.com/MeKo-Tech/app?x=a%20b",
	} {
		if err := validateRepoURL(raw); err != nil {
			t.Errorf("validateRepoURL(%q) = %v, want nil", raw, err)
		}
	}
}

// TestParseTagFile reads a seeded tag file back the way the drift scanner
// does: the tag file, not the claim, carries each stage's image tag.
func TestParseTagFile(t *testing.T) {
	t.Parallel()

	in := fullInput()

	files, err := in.TagFiles()
	if err != nil {
		t.Fatalf("TagFiles: %v", err)
	}

	f, err := ParseTagFile(files["tags/"+in.Slug+"/prod.yaml"])
	if err != nil {
		t.Fatalf("ParseTagFile: %v", err)
	}

	want := ComponentTags{Backend: in.Backend.ImageTag, Frontend: in.Frontend.ImageTag}
	if f.ImageTags != want {
		t.Errorf("imageTags = %+v, want %+v", f.ImageTags, want)
	}
}
