// Package drift computes per-component image-tag drift between an app's
// staging and prod stages, using the GitHub compare API as the source of
// truth ("staging is N commits ahead of prod").
//
// Tag values (e.g. `v1.2.3` or a 40-char SHA) flow in from the registry
// App.Images.Backend.Tag.{Staging,Prod}; the compare API accepts either
// shape directly — no resolve-tag-to-SHA round-trip required for tags
// that exist on the remote.
package drift

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	gh "github.com/google/go-github/v68/github"
)

// Fetcher resolves "how many commits is staging ahead of prod" via the
// GitHub /repos/{o}/{r}/compare endpoint.
type Fetcher struct {
	GH     *gh.Client
	Logger *slog.Logger
}

// CommitsAhead returns how many commits stagingRef has that prodRef does
// not. Returns 0 if the refs are identical, either is empty, or one of
// the tags doesn't exist on the remote (likely a brand-new app whose
// prod tag was never set). Any other error (rate-limit, 5xx) bubbles up.
func (f *Fetcher) CommitsAhead(
	ctx context.Context,
	owner, repo, prodRef, stagingRef string,
) (int, error) {
	if f == nil || f.GH == nil {
		return 0, errors.New("drift: GH client not configured")
	}

	if owner == "" || repo == "" || prodRef == "" || stagingRef == "" {
		return 0, nil
	}

	if prodRef == stagingRef {
		return 0, nil
	}

	compare, resp, err := f.GH.Repositories.CompareCommits(
		ctx,
		owner,
		repo,
		prodRef,
		stagingRef,
		nil,
	)
	if isNotFound(resp, err) {
		f.logf("warn", "compare 404 — one of the tags is missing, treating as zero drift",
			"owner", owner, "repo", repo, "prod", prodRef, "staging", stagingRef)

		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("compare %s/%s %s...%s: %w", owner, repo, prodRef, stagingRef, err)
	}

	if compare == nil || compare.AheadBy == nil {
		return 0, nil
	}

	return *compare.AheadBy, nil
}

func (f *Fetcher) logf(level, msg string, kv ...any) {
	if f.Logger == nil {
		return
	}

	switch level {
	case "info":
		f.Logger.Info(msg, kv...)
	case "warn":
		f.Logger.Warn(msg, kv...)
	case "error":
		f.Logger.Error(msg, kv...)
	}
}

// isNotFound returns true when the GitHub API replied with 404 — mirrors
// the helper in the compliance package.
func isNotFound(resp *gh.Response, err error) bool {
	if err == nil {
		return false
	}

	if resp != nil && resp.Response != nil && resp.StatusCode == http.StatusNotFound {
		return true
	}

	var errResp *gh.ErrorResponse
	if errors.As(err, &errResp) {
		if errResp.Response != nil && errResp.Response.StatusCode == http.StatusNotFound {
			return true
		}
	}

	return false
}
