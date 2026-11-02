package repobootstrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	gh "github.com/google/go-github/v68/github"
)

const vibeKeyPrefix = "https://meko-vibe-vault.vault.azure.net/keys/sops-key-vibe/"

// fakeRepo serves the slice of the GitHub API that Run touches. Files in
// `existing` answer 200 on GET; every PUT to contents is recorded.
type fakeRepo struct {
	mu       sync.Mutex
	existing map[string]bool
	created  map[string]string
}

func (f *fakeRepo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	const contents = "/repos/o/r/contents/"

	f.mu.Lock()
	defer f.mu.Unlock()

	switch {
	case strings.HasPrefix(r.URL.Path, contents):
		path := strings.TrimPrefix(r.URL.Path, contents)

		switch r.Method {
		case http.MethodGet:
			if f.existing[path] || f.created[path] != "" {
				_ = json.NewEncoder(w).Encode(map[string]string{"type": "file", "path": path})
				return
			}

			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		case http.MethodPut:
			var body struct {
				Content string `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			raw, err := base64.StdEncoding.DecodeString(body.Content)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			f.created[path] = string(raw)

			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{}`))
		}
	case r.URL.Path == "/repos/o/r/branches/main/protection":
		if r.Method == http.MethodGet {
			http.Error(w, `{"message":"Branch not protected"}`, http.StatusNotFound)
			return
		}

		_, _ = w.Write([]byte(`{}`))
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusTeapot)
	}
}

func runAgainst(t *testing.T, existing ...string) (*fakeRepo, []Action) {
	t.Helper()

	f := &fakeRepo{existing: map[string]bool{}, created: map[string]string{}}
	for _, p := range existing {
		f.existing[p] = true
	}

	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)

	client := gh.NewClient(srv.Client())

	base, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}

	client.BaseURL = base

	actions := Run(context.Background(), client, "o", "r", "demo-app")
	for _, a := range actions {
		if a.Status == StatusFailed {
			t.Fatalf("step %q failed: %s", a.Name, a.Message)
		}
	}

	return f, actions
}

// A repo created from go-react-template already carries release-please
// (as `.yaml`) and AGENTS.md. Bootstrap must not add a second
// release-please workflow, nor any file of the retired values.env flow.
func TestRunOnTemplateRepo(t *testing.T) {
	f, _ := runAgainst(t,
		".github/workflows/release-please.yaml",
		"release-please-config.json",
		".release-please-manifest.json",
		"AGENTS.md",
	)

	got := make([]string, 0, len(f.created))
	for p := range f.created {
		got = append(got, p)
	}

	slices.Sort(got)

	want := []string{".github/workflows/test-pr-title.yml", ".sops.yaml"}
	if !slices.Equal(got, want) {
		t.Fatalf("created %v, want %v", got, want)
	}
}

func TestRunOnEmptyRepoWritesNoLegacySecretFiles(t *testing.T) {
	f, _ := runAgainst(t)

	for p := range f.created {
		for _, prefix := range []string{"values.", "secrets/", "deploy/"} {
			if strings.HasPrefix(p, prefix) {
				t.Errorf("bootstrap created %s", p)
			}
		}
	}

	if _, ok := f.created[".sops.yaml"]; !ok {
		t.Fatal(".sops.yaml not created")
	}
}

// The .sops.yaml must be what `scripts/platform-secrets.sh init` writes:
// a single rule for secrets/<stage>.sops.yaml to sops-key-vibe, with no
// key other than path_regex and azure_keyvault and no mekorp-vault.
func TestSopsConfigRoutesSecretsToVibeVaultOnly(t *testing.T) {
	body, err := templates.ReadFile("templates/.sops.yaml")
	if err != nil {
		t.Fatal(err)
	}

	cfg := string(body)

	if strings.Contains(cfg, "mekorp-vault") {
		t.Error(".sops.yaml still references mekorp-vault")
	}

	if n := strings.Count(cfg, "- path_regex:"); n != 1 {
		t.Errorf(".sops.yaml has %d rules, want 1", n)
	}

	m := regexp.MustCompile(`(?m)^\s+azure_keyvault: (\S+)$`).FindStringSubmatch(cfg)
	if m == nil || !strings.HasPrefix(m[1], vibeKeyPrefix) || len(m[1]) == len(vibeKeyPrefix) {
		t.Errorf("azure_keyvault = %v, want %s<version>", m, vibeKeyPrefix)
	}

	rm := regexp.MustCompile(`(?m)^\s+- path_regex: (\S+)$`).FindStringSubmatch(cfg)
	if rm == nil {
		t.Fatal("no path_regex")
	}

	re := regexp.MustCompile(rm[1])
	for _, p := range []string{"secrets/staging.sops.yaml", "secrets/prod.sops.yaml"} {
		if !re.MatchString(p) {
			t.Errorf("path_regex %q does not match %s", rm[1], p)
		}
	}

	if re.MatchString("values.sops.env") {
		t.Errorf("path_regex %q still matches values.sops.env", rm[1])
	}
}

func TestNoLegacyTemplatesEmbedded(t *testing.T) {
	err := fs.WalkDir(templates, "templates", func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if strings.HasPrefix(p, "templates/values.") {
			t.Errorf("legacy template %s still embedded", p)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	agents, err := templates.ReadFile("templates/AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}

	for _, unwanted := range []string{"values.sops.env", "values.env", "image-updater", "mekorp-vault."} {
		if strings.Contains(string(agents), unwanted) {
			t.Errorf("AGENTS.md template still mentions %q", unwanted)
		}
	}
}
