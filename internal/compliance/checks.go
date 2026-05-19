package compliance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v68/github"
)

// Check is one named test that runs against a single repo.
type Check struct {
	Name string
	Run  func(ctx context.Context, c *gh.Client, owner, repo string) (Status, string)
}

// StandardChecks is the suite the periodic scanner runs. Order is the
// display order on the dashboard tile.
func StandardChecks() []Check {
	return []Check{
		{Name: "pr_title_workflow", Run: checkPRTitleWorkflow},
		{Name: "release_please_workflow", Run: checkReleasePleaseWorkflow},
		{Name: "branch_protection_main", Run: checkBranchProtectionMain},
	}
}

// checkPRTitleWorkflow returns pass when `.github/workflows/test-pr-title.yml`
// exists on the default branch — the conventional-commits gate the rest of
// the org uses to keep release-please happy.
func checkPRTitleWorkflow(ctx context.Context, c *gh.Client, owner, repo string) (Status, string) {
	const path = ".github/workflows/test-pr-title.yml"

	_, _, resp, err := c.Repositories.GetContents(ctx, owner, repo, path, nil)
	if isNotFound(resp, err) {
		return StatusFail, "missing .github/workflows/test-pr-title.yml — Conventional-Commits PR-title gate not enforced"
	}
	if err != nil {
		return StatusError, fmt.Sprintf("github: %v", err)
	}
	return StatusPass, ""
}

// checkReleasePleaseWorkflow looks for the release-please workflow file.
// Either of the two common filenames counts.
func checkReleasePleaseWorkflow(ctx context.Context, c *gh.Client, owner, repo string) (Status, string) {
	candidates := []string{
		".github/workflows/release-please.yml",
		".github/workflows/release.yml",
	}

	var lastErr error
	for _, p := range candidates {
		_, _, resp, err := c.Repositories.GetContents(ctx, owner, repo, p, nil)
		if err == nil {
			return StatusPass, ""
		}
		if !isNotFound(resp, err) {
			lastErr = err
		}
	}

	if lastErr != nil {
		return StatusError, fmt.Sprintf("github: %v", lastErr)
	}
	return StatusFail, "missing release-please workflow (.github/workflows/{release-please,release}.yml) — no automated version bumps"
}

// checkBranchProtectionMain checks that `main` is protected. We don't
// enforce a specific rule set yet — just "is there a protection record
// at all". Future iterations can require specific knobs (signed commits,
// status checks, etc.).
func checkBranchProtectionMain(ctx context.Context, c *gh.Client, owner, repo string) (Status, string) {
	_, resp, err := c.Repositories.GetBranchProtection(ctx, owner, repo, "main")
	if isNotFound(resp, err) {
		return StatusFail, "no branch-protection rule on `main` — force-push + delete allowed"
	}
	if err != nil {
		return StatusError, fmt.Sprintf("github: %v", err)
	}
	return StatusPass, ""
}

// isNotFound returns true when the GitHub API replied with 404 — used to
// distinguish "absent" (a compliance fail) from "transient error".
func isNotFound(resp *gh.Response, err error) bool {
	if err == nil {
		return false
	}

	if resp != nil && resp.Response != nil && resp.StatusCode == http.StatusNotFound {
		return true
	}

	// go-github wraps 404s in an ErrorResponse; the StatusCode check above
	// usually catches it. Belt-and-braces fallback on message contents.
	var errResp *gh.ErrorResponse
	if errors.As(err, &errResp) {
		if errResp.Response != nil && errResp.Response.StatusCode == http.StatusNotFound {
			return true
		}
	}

	return strings.Contains(err.Error(), "404")
}
