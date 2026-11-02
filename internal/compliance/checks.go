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

// checkPRTitleWorkflow returns pass when `.github/workflows/test-pr-title.y(a)ml`
// exists on the default branch — the conventional-commits gate the rest of
// the org uses to keep release-please happy.
func checkPRTitleWorkflow(ctx context.Context, c *gh.Client, owner, repo string) (Status, string) {
	return checkAnyFile(
		ctx,
		c,
		owner,
		repo,
		[]string{
			".github/workflows/test-pr-title.yml",
			".github/workflows/test-pr-title.yaml",
		},
		"missing .github/workflows/test-pr-title.y(a)ml — Conventional-Commits PR-title gate not enforced",
	)
}

// checkReleasePleaseWorkflow looks for the release-please workflow file.
// Both common filenames count, in either spelling — go-react-template
// ships `.yaml`, the bootstrap writes `.yml`.
func checkReleasePleaseWorkflow(
	ctx context.Context,
	c *gh.Client,
	owner, repo string,
) (Status, string) {
	return checkAnyFile(
		ctx,
		c,
		owner,
		repo,
		[]string{
			".github/workflows/release-please.yml",
			".github/workflows/release-please.yaml",
			".github/workflows/release.yml",
			".github/workflows/release.yaml",
		},
		"missing release-please workflow (.github/workflows/{release-please,release}.y(a)ml) — no automated version bumps",
	)
}

// checkAnyFile passes when one of `candidates` exists, fails with
// `failMsg` when all of them are absent, and errors when GitHub returned
// something other than 404 and nothing was found.
func checkAnyFile(
	ctx context.Context,
	c *gh.Client,
	owner, repo string,
	candidates []string,
	failMsg string,
) (Status, string) {
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

	return StatusFail, failMsg
}

// checkBranchProtectionMain checks that `main` is protected. We don't
// enforce a specific rule set yet — just "is there a protection record
// at all". Future iterations can require specific knobs (signed commits,
// status checks, etc.).
func checkBranchProtectionMain(
	ctx context.Context,
	c *gh.Client,
	owner, repo string,
) (Status, string) {
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
