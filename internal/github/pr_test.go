package github

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	gh "github.com/google/go-github/v68/github"
)

// fakeGitHub records the Git Data API calls of openClaimPR.
type fakeGitHub struct {
	tree struct {
		BaseTree string `json:"base_tree"`
		Tree     []struct {
			Path    string `json:"path"`
			Mode    string `json:"mode"`
			Type    string `json:"type"`
			Content string `json:"content"`
		} `json:"tree"`
	}
	commit struct {
		Message string   `json:"message"`
		Tree    string   `json:"tree"`
		Parents []string `json:"parents"`
		Author  struct {
			Name  string `json:"name"`
			Email string `json:"email"`
		} `json:"author"`
	}
	ref struct {
		Ref string `json:"ref"`
		SHA string `json:"sha"`
	}
	pr struct {
		Title string `json:"title"`
		Head  string `json:"head"`
		Base  string `json:"base"`
	}
	refStatus int
}

func (f *fakeGitHub) handler(t *testing.T) http.Handler {
	t.Helper()

	decode := func(r *http.Request, v any) {
		if err := json.NewDecoder(r.Body).Decode(v); err != nil {
			t.Errorf("%s %s: decode body: %v", r.Method, r.URL.Path, err)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc(
		"GET /repos/o/r/git/ref/heads/main",
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write(
				[]byte(`{"ref":"refs/heads/main","object":{"type":"commit","sha":"base-sha"}}`),
			)
		},
	)
	mux.HandleFunc(
		"GET /repos/o/r/git/commits/base-sha",
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"sha":"base-sha","tree":{"sha":"base-tree"}}`))
		},
	)
	mux.HandleFunc("POST /repos/o/r/git/trees", func(w http.ResponseWriter, r *http.Request) {
		decode(r, &f.tree)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sha":"new-tree"}`))
	})
	mux.HandleFunc("POST /repos/o/r/git/commits", func(w http.ResponseWriter, r *http.Request) {
		decode(r, &f.commit)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"sha":"new-commit"}`))
	})
	mux.HandleFunc("POST /repos/o/r/git/refs", func(w http.ResponseWriter, r *http.Request) {
		decode(r, &f.ref)

		if f.refStatus != 0 {
			w.WriteHeader(f.refStatus)
			_, _ = w.Write([]byte(`{"message":"Reference already exists"}`))

			return
		}

		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(
			[]byte(`{"ref":"refs/heads/feat/claim-my-app","object":{"sha":"new-commit"}}`),
		)
	})
	mux.HandleFunc("POST /repos/o/r/pulls", func(w http.ResponseWriter, r *http.Request) {
		decode(r, &f.pr)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":42,"html_url":"https://github.com/o/r/pull/42"}`))
	})

	return mux
}

func newTestClient(t *testing.T, f *fakeGitHub) *gh.Client {
	t.Helper()

	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)

	client := gh.NewClient(srv.Client())

	base, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}

	client.BaseURL = base

	return client
}

func claimFiles(t *testing.T, prodEnabled bool) map[string][]byte {
	t.Helper()

	in := &registry.ClaimInput{
		Slug:       "my-app",
		HasBackend: true,
		Backend: registry.ComponentInput{
			ImageRepository: "ghcr.io/meko-tech/my-app",
			ImageTag:        "1.0.0",
		},
		Staging: registry.StageInput{Host: "my-app.staging.apps.meko.work", Enabled: true},
		Prod:    registry.StageInput{Host: "my-app.apps.meko.work", Enabled: prodEnabled},
	}

	files, err := in.TagFiles()
	if err != nil {
		t.Fatal(err)
	}

	files["apps/my-app.yaml"] = []byte("slug: my-app\n")

	return files
}

func TestOpenClaimPROneCommit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		prodEnabled bool
		wantPaths   []string
	}{
		{"both stages", true, []string{
			"apps/my-app.yaml", "tags/my-app/prod.yaml", "tags/my-app/staging.yaml",
		}},
		{"prod disabled", false, []string{"apps/my-app.yaml", "tags/my-app/staging.yaml"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGitHub{}

			res, err := openClaimPR(context.Background(), newTestClient(t, f), PRRequest{
				Repo:        "o/r",
				Slug:        "my-app",
				Files:       claimFiles(t, tc.prodEnabled),
				Branch:      "feat/claim-my-app",
				Title:       "feat: claim my-app",
				AuthorLogin: "octo",
			})
			if err != nil {
				t.Fatalf("openClaimPR: %v", err)
			}

			if res.Number != 42 || res.URL != "https://github.com/o/r/pull/42" {
				t.Errorf("result = %+v", res)
			}

			if f.tree.BaseTree != "base-tree" {
				t.Errorf("base_tree = %q, want base-tree", f.tree.BaseTree)
			}

			var paths []string

			for _, e := range f.tree.Tree {
				paths = append(paths, e.Path)

				if e.Mode != "100644" || e.Type != "blob" || e.Content == "" {
					t.Errorf("tree entry %s = %+v", e.Path, e)
				}

				if strings.HasPrefix(e.Path, "tags/") &&
					!strings.Contains(e.Content, `backend: "1.0.0"`) {
					t.Errorf("tag file %s lacks the initial tag:\n%s", e.Path, e.Content)
				}
			}

			if !slices.Equal(paths, tc.wantPaths) {
				t.Errorf("tree paths = %v, want %v", paths, tc.wantPaths)
			}

			if f.commit.Message != "feat: claim my-app" || f.commit.Tree != "new-tree" {
				t.Errorf("commit = %+v", f.commit)
			}

			if !slices.Equal(f.commit.Parents, []string{"base-sha"}) {
				t.Errorf("commit parents = %v, want [base-sha]", f.commit.Parents)
			}

			if f.commit.Author.Name != "octo" ||
				f.commit.Author.Email != "octo@users.noreply.github.com" {
				t.Errorf("commit author = %+v", f.commit.Author)
			}

			if f.ref.Ref != "refs/heads/feat/claim-my-app" || f.ref.SHA != "new-commit" {
				t.Errorf("ref = %+v", f.ref)
			}

			if f.pr.Head != "feat/claim-my-app" || f.pr.Base != "main" ||
				f.pr.Title != "feat: claim my-app" {
				t.Errorf("pr = %+v", f.pr)
			}
		})
	}
}

func TestOpenClaimPRBranchExists(t *testing.T) {
	f := &fakeGitHub{refStatus: http.StatusUnprocessableEntity}

	_, err := openClaimPR(context.Background(), newTestClient(t, f), PRRequest{
		Repo:   "o/r",
		Slug:   "my-app",
		Files:  claimFiles(t, true),
		Branch: "feat/claim-my-app",
		Title:  "feat: claim my-app",
	})
	if err == nil || !strings.Contains(err.Error(), "branch already exists") {
		t.Fatalf("err = %v, want branch already exists", err)
	}

	if f.pr.Head != "" {
		t.Error("PR was opened although the branch could not be created")
	}
}

func TestPRRequestValidateFiles(t *testing.T) {
	req := PRRequest{Repo: "o/r", Slug: "s", Branch: "b", Title: "t"}
	if err := req.validate(); err == nil {
		t.Error("no files must be rejected")
	}

	req.Files = map[string][]byte{"apps/s.yaml": nil}
	if err := req.validate(); err == nil {
		t.Error("empty file must be rejected")
	}
}
