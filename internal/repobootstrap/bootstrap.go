// Package repobootstrap applies the platform's standard repo-config to a
// vibe-tenant's source repository when they claim a slug.
//
// Acts on the user's OAuth token (NOT a server-wide PAT) so audit logs
// in the user's repo show their own login. Requires the OAuth grant to
// include the `repo` + `workflow` scopes.
package repobootstrap

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "github.com/google/go-github/v68/github"
)

//go:embed templates
var templates embed.FS

// Action describes what the bootstrap helper did. The Claim handler
// surfaces these to the user on the success page.
type Action struct {
	Name    string // human-readable: "PR-title workflow", "Branch protection on main", …
	Status  Status // applied | skipped | failed
	Path    string // file path or API resource that was touched (when applicable)
	Message string // error message when Status == failed
}

// Status enumerates the outcome of one bootstrap step.
type Status string

const (
	StatusApplied Status = "applied" // newly created or updated by us
	StatusSkipped Status = "skipped" // already in place — no change needed
	StatusFailed  Status = "failed"  // GitHub returned an error
)

// Run executes every bootstrap step against (owner, repo). Errors from
// individual steps surface in the returned []Action — Run itself only
// errors when the client setup is unusable.
func Run(ctx context.Context, client *gh.Client, owner, repo, slug string) []Action {
	steps := []func(context.Context, *gh.Client, string, string, string) Action{
		ensurePRTitleWorkflow,
		ensureReleasePleaseWorkflow,
		ensureReleasePleaseConfig,
		ensureReleasePleaseManifest,
		ensureSopsConfig,
		ensureValuesEnv,
		ensureValuesSopsEnv,
		ensureAgentsMarkdown,
		ensureBranchProtectionMain,
	}

	out := make([]Action, 0, len(steps))
	for _, step := range steps {
		out = append(out, step(ctx, client, owner, repo, slug))
	}
	return out
}

// --- individual steps --------------------------------------------------------

func ensurePRTitleWorkflow(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	return ensureFile(ctx, c, owner, repo,
		".github/workflows/test-pr-title.yml",
		"templates/test-pr-title.yml",
		"chore: add Conventional-Commits PR-title check (platform-ui)",
		"PR-title workflow",
		nil,
	)
}

func ensureReleasePleaseWorkflow(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	return ensureFile(ctx, c, owner, repo,
		".github/workflows/release-please.yml",
		"templates/release-please.yml",
		"chore: add release-please workflow (platform-ui)",
		"release-please workflow",
		nil,
	)
}

func ensureReleasePleaseConfig(ctx context.Context, c *gh.Client, owner, repo, slug string) Action {
	subs := map[string]string{"__SLUG__": slug}
	return ensureFile(ctx, c, owner, repo,
		"release-please-config.json",
		"templates/release-please-config.json",
		"chore: add release-please-config.json (platform-ui)",
		"release-please config",
		subs,
	)
}

func ensureReleasePleaseManifest(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	return ensureFile(ctx, c, owner, repo,
		".release-please-manifest.json",
		"templates/release-please-manifest.json",
		"chore: add release-please manifest (platform-ui)",
		"release-please manifest",
		nil,
	)
}

// ensureSopsConfig writes the standard `.sops.yaml` that pins the three
// MeKo Azure Key Vault keys to the per-stage filename convention. Vibe
// coders don't edit this file; the platform UI manages it.
func ensureSopsConfig(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	return ensureFile(ctx, c, owner, repo,
		".sops.yaml",
		"templates/.sops.yaml",
		"chore: add .sops.yaml — MeKo Azure KV pinning (platform-ui)",
		"sops config",
		nil,
	)
}

// ensureValuesEnv writes a commented stub `values.env`. Plain (non-secret)
// app config goes here; per-stage overlays (`values.staging.env`,
// `values.prod.env`) are optional and not bootstrapped — the vibe coder
// adds them when needed.
func ensureValuesEnv(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	return ensureFile(ctx, c, owner, repo,
		"values.env",
		"templates/values.env",
		"chore: add values.env stub (platform-ui)",
		"values.env (plain env)",
		nil,
	)
}

// ensureValuesSopsEnv writes the unencrypted scaffold for the sops-managed
// secrets file. The header explicitly tells the vibe coder to run
// `sops --encrypt --in-place values.sops.env` before committing real
// secrets. We don't ship pre-encrypted content because the platform UI
// doesn't have KV credentials.
func ensureValuesSopsEnv(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	return ensureFile(ctx, c, owner, repo,
		"values.sops.env",
		"templates/values.sops.env",
		"chore: add values.sops.env scaffold — encrypt before committing (platform-ui)",
		"values.sops.env (encrypted env)",
		nil,
	)
}

// ensureAgentsMarkdown writes the agent-facing instructions any AI tool
// running in the repo can read for context: deploy flow, repo
// conventions, links to /docs.
func ensureAgentsMarkdown(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	return ensureFile(ctx, c, owner, repo,
		"AGENTS.md",
		"templates/AGENTS.md",
		"chore: add AGENTS.md — AI-agent instructions (platform-ui)",
		"AGENTS.md (AI-agent instructions)",
		nil,
	)
}

func ensureBranchProtectionMain(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	const branch = "main"
	action := Action{Name: "Branch protection on `main`", Path: branch}

	// Already protected? Treat as skip.
	if _, resp, err := c.Repositories.GetBranchProtection(ctx, owner, repo, branch); err == nil {
		action.Status = StatusSkipped
		return action
	} else if !isNotFound(resp, err) {
		action.Status = StatusFailed
		action.Message = err.Error()
		return action
	}

	// Minimal protection: require PR + no force-push + no delete.
	req := &gh.ProtectionRequest{
		RequiredPullRequestReviews: &gh.PullRequestReviewsEnforcementRequest{
			RequiredApprovingReviewCount: 0,
		},
		EnforceAdmins:    false,
		AllowForcePushes: gh.Bool(false),
		AllowDeletions:   gh.Bool(false),
	}

	if _, _, err := c.Repositories.UpdateBranchProtection(ctx, owner, repo, branch, req); err != nil {
		action.Status = StatusFailed
		action.Message = err.Error()
		return action
	}

	action.Status = StatusApplied
	return action
}

// --- file helpers ------------------------------------------------------------

// ensureFile creates `path` in the repo's default branch from an embedded
// template, substituting placeholders. Skips if the file is already
// present (we don't try to be smart about content drift — manual edits
// win).
func ensureFile(
	ctx context.Context,
	c *gh.Client,
	owner, repo, path, templatePath, commitMsg, displayName string,
	subs map[string]string,
) Action {
	a := Action{Name: displayName, Path: path}

	// Does the file already exist?
	if _, _, resp, err := c.Repositories.GetContents(ctx, owner, repo, path, nil); err == nil {
		a.Status = StatusSkipped
		return a
	} else if !isNotFound(resp, err) {
		a.Status = StatusFailed
		a.Message = err.Error()
		return a
	}

	body, err := templates.ReadFile(templatePath)
	if err != nil {
		a.Status = StatusFailed
		a.Message = fmt.Sprintf("template read: %v", err)
		return a
	}

	content := body
	if len(subs) > 0 {
		s := string(body)
		for k, v := range subs {
			s = strings.ReplaceAll(s, k, v)
		}
		content = []byte(s)
	}

	opts := &gh.RepositoryContentFileOptions{
		Message: gh.String(commitMsg),
		Content: content,
	}

	if _, _, err := c.Repositories.CreateFile(ctx, owner, repo, path, opts); err != nil {
		a.Status = StatusFailed
		a.Message = err.Error()
		return a
	}

	a.Status = StatusApplied
	return a
}

// isNotFound returns true when GitHub replied with 404. Same logic as
// the compliance scanner's; duplicated to keep the package self-contained.
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
	return strings.Contains(err.Error(), "404")
}
