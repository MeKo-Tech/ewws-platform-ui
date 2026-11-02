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

// `all:` is required: a plain `templates` pattern skips dotfiles, so
// `templates/.sops.yaml` was never embedded and the sops step always
// failed with "template read".
//
//go:embed all:templates
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
		".github/workflows/test-pr-title.yaml",
	)
}

func ensureReleasePleaseWorkflow(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	return ensureFile(ctx, c, owner, repo,
		".github/workflows/release-please.yml",
		"templates/release-please.yml",
		"chore: add release-please workflow (platform-ui)",
		"release-please workflow",
		nil,
		// go-react-template ships `.yaml`; a second `.yml` copy would cut
		// every release twice.
		".github/workflows/release-please.yaml",
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

// ensureSopsConfig writes the `.sops.yaml` that go-react-template's
// `scripts/platform-secrets.sh init` writes: one rule routing
// `secrets/<stage>.sops.yaml` to `sops-key-vibe` in meko-vibe-vault, and
// nothing else. `just secrets-set` refuses to encrypt unless that rule is
// the first match, so an existing `.sops.yaml` is left alone — the script
// tells the user what to fix.
//
// Bootstrap creates no `secrets/<stage>.sops.yaml` and no
// `deploy/sealed/` file: encrypting needs Key Vault access and sealing
// needs real values, both of which only the app author has. The
// secrets tooling creates them on first use.
func ensureSopsConfig(ctx context.Context, c *gh.Client, owner, repo, _ string) Action {
	return ensureFile(ctx, c, owner, repo,
		".sops.yaml",
		"templates/.sops.yaml",
		"chore: add .sops.yaml — route secrets/ to meko-vibe-vault (platform-ui)",
		"sops config",
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
		AllowForcePushes: gh.Ptr(false),
		AllowDeletions:   gh.Ptr(false),
	}

	if _, _, err := c.Repositories.UpdateBranchProtection(
		ctx,
		owner,
		repo,
		branch,
		req,
	); err != nil {
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
// win). It also skips when one of `equivalents` exists, e.g. the `.yaml`
// spelling of a workflow the template ships.
func ensureFile(
	ctx context.Context,
	c *gh.Client,
	owner, repo, path, templatePath, commitMsg, displayName string,
	subs map[string]string,
	equivalents ...string,
) Action {
	a := Action{Name: displayName, Path: path}

	// Does the file (or an equivalent) already exist?
	for _, p := range append([]string{path}, equivalents...) {
		_, _, resp, err := c.Repositories.GetContents(ctx, owner, repo, p, nil)
		if err == nil {
			a.Path = p
			a.Status = StatusSkipped

			return a
		}

		if !isNotFound(resp, err) {
			a.Path = p
			a.Status = StatusFailed
			a.Message = err.Error()

			return a
		}
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
		Message: gh.Ptr(commitMsg),
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
