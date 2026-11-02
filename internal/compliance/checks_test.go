package compliance

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	gh "github.com/google/go-github/v68/github"
)

// clientWithFiles returns a client whose repo o/r holds exactly `files`.
func clientWithFiles(t *testing.T, files ...string) *gh.Client {
	t.Helper()

	have := map[string]bool{}
	for _, f := range files {
		have[f] = true
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/repos/o/r/contents/")
		if have[path] {
			_, _ = w.Write([]byte(`{"type":"file"}`))
			return
		}

		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	client := gh.NewClient(srv.Client())

	base, err := url.Parse(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}

	client.BaseURL = base

	return client
}

// go-react-template ships `.yaml` workflows; the checks must accept them
// just like the `.yml` files the bootstrap writes.
func TestWorkflowChecksAcceptBothSpellings(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		check func(context.Context, *gh.Client, string, string) (Status, string)
		file  string
		want  Status
	}{
		{"pr-title yml", checkPRTitleWorkflow, ".github/workflows/test-pr-title.yml", StatusPass},
		{"pr-title yaml", checkPRTitleWorkflow, ".github/workflows/test-pr-title.yaml", StatusPass},
		{"pr-title missing", checkPRTitleWorkflow, "README.md", StatusFail},
		{"release-please yml", checkReleasePleaseWorkflow, ".github/workflows/release-please.yml", StatusPass},
		{"release-please yaml", checkReleasePleaseWorkflow, ".github/workflows/release-please.yaml", StatusPass},
		{"release yaml", checkReleasePleaseWorkflow, ".github/workflows/release.yaml", StatusPass},
		{"release missing", checkReleasePleaseWorkflow, "README.md", StatusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := tc.check(ctx, clientWithFiles(t, tc.file), "o", "r")
			if got != tc.want {
				t.Errorf("status = %s (%s), want %s", got, msg, tc.want)
			}
		})
	}
}
