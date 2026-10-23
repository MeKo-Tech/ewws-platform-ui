package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	gh "github.com/google/go-github/v68/github"
)

// PRRequest is the input to OpenClaimPR.
type PRRequest struct {
	Repo string // "owner/repo"
	Slug string
	// Files maps a repository path (e.g. apps/<slug>.yaml,
	// tags/<slug>/staging.yaml) to its content; all of them land in one
	// commit.
	Files       map[string][]byte
	Branch      string // e.g. feat/claim-<slug>
	Title       string
	Body        string
	AuthorLogin string
	AuthorEmail string
	BaseBranch  string // usually "main"
}

// PRResult is the URL of the PR we just opened.
type PRResult struct {
	URL    string
	Number int
}

// ErrClaimExists is matched (errors.Is) by a *ClaimExistsError: the slug
// is already claimed on the base branch.
var ErrClaimExists = errors.New("claim already exists")

// ClaimExistsError names the path that already exists on the base branch.
// CreateTree would silently replace it, so openClaimPR refuses before
// writing anything.
type ClaimExistsError struct {
	Slug   string
	Path   string
	Branch string
}

func (e *ClaimExistsError) Error() string {
	return fmt.Sprintf("slug %s is already claimed: %s exists on %s", e.Slug, e.Path, e.Branch)
}

func (e *ClaimExistsError) Unwrap() error { return ErrClaimExists }

// OpenClaimPR commits all req.Files in a single commit on a new feature
// branch and opens a PR — all via the user's OAuth client.
func OpenClaimPR(ctx context.Context, httpClient *http.Client, req PRRequest) (*PRResult, error) {
	return openClaimPR(ctx, gh.NewClient(httpClient), req)
}

// openClaimPR uses the Git Data API so several files land in one commit:
//
//  1. Look up the SHA of the base branch (refs/heads/<base>).
//     Refuse with a *ClaimExistsError if any of req.Files or the
//     directory tags/<slug> already exists there.
//  2. Read that commit's tree SHA.
//  3. Create a tree on top of it with every file as a blob.
//  4. Create a commit with that tree and the base commit as parent.
//  5. Create branch ref refs/heads/<branch> at the new commit.
//  6. Open the PR.
func openClaimPR(ctx context.Context, client *gh.Client, req PRRequest) (*PRResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	owner, repo, err := splitRepo(req.Repo)
	if err != nil {
		return nil, err
	}

	baseSHA, err := getBranchSHA(ctx, client, owner, repo, req.BaseBranch)
	if err != nil {
		return nil, fmt.Errorf("base branch %q: %w", req.BaseBranch, err)
	}

	paths := make([]string, 0, len(req.Files))
	for p := range req.Files {
		paths = append(paths, p)
	}

	slices.Sort(paths)

	// CreateTree replaces existing paths without complaint, so a claim of
	// an already-claimed slug would overwrite its registry entry.
	for _, p := range append(slices.Clone(paths), "tags/"+req.Slug) {
		exists, err := pathExists(ctx, client, owner, repo, p, baseSHA)
		if err != nil {
			return nil, fmt.Errorf("check %s on %s: %w", p, req.BaseBranch, err)
		}

		if exists {
			return nil, &ClaimExistsError{Slug: req.Slug, Path: p, Branch: req.BaseBranch}
		}
	}

	baseCommit, _, err := client.Git.GetCommit(ctx, owner, repo, baseSHA)
	if err != nil {
		return nil, fmt.Errorf("base commit %s: %w", baseSHA, err)
	}

	entries := make([]*gh.TreeEntry, 0, len(paths))
	for _, p := range paths {
		entries = append(entries, &gh.TreeEntry{
			Path:    gh.Ptr(p),
			Mode:    gh.Ptr("100644"),
			Type:    gh.Ptr("blob"),
			Content: gh.Ptr(string(req.Files[p])),
		})
	}

	tree, _, err := client.Git.CreateTree(
		ctx, owner, repo, baseCommit.GetTree().GetSHA(), entries,
	)
	if err != nil {
		return nil, fmt.Errorf("create tree: %w", err)
	}

	commit, _, err := client.Git.CreateCommit(ctx, owner, repo, &gh.Commit{
		Message: gh.Ptr(fmt.Sprintf("feat: claim %s", req.Slug)),
		Tree:    &gh.Tree{SHA: tree.SHA},
		Parents: []*gh.Commit{{SHA: gh.Ptr(baseSHA)}},
		Author: &gh.CommitAuthor{
			Name:  gh.Ptr(authorName(req)),
			Email: gh.Ptr(authorEmail(req)),
			Date:  &gh.Timestamp{Time: time.Now().UTC()},
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("create commit: %w", err)
	}

	if err := createBranch(ctx, client, owner, repo, req.Branch, commit.GetSHA()); err != nil {
		return nil, fmt.Errorf("create branch %q: %w", req.Branch, err)
	}

	prReq := &gh.NewPullRequest{
		Title: gh.Ptr(req.Title),
		Head:  gh.Ptr(req.Branch),
		Base:  gh.Ptr(req.BaseBranch),
		Body:  gh.Ptr(req.Body),
	}

	pr, _, err := client.PullRequests.Create(ctx, owner, repo, prReq)
	if err != nil {
		return nil, fmt.Errorf("open PR: %w", err)
	}

	return &PRResult{
		URL:    pr.GetHTMLURL(),
		Number: pr.GetNumber(),
	}, nil
}

func (r *PRRequest) validate() error {
	if r.Repo == "" {
		return errors.New("repo is required")
	}

	if r.Slug == "" {
		return errors.New("slug is required")
	}

	if len(r.Files) == 0 {
		return errors.New("at least one file is required")
	}

	for p, content := range r.Files {
		if p == "" || strings.HasPrefix(p, "/") {
			return fmt.Errorf("file path %q must be a relative repository path", p)
		}

		if len(content) == 0 {
			return fmt.Errorf("file %s is empty", p)
		}
	}

	if r.Branch == "" {
		return errors.New("branch is required")
	}

	if r.Title == "" {
		return errors.New("title is required")
	}

	if r.BaseBranch == "" {
		r.BaseBranch = "main"
	}

	return nil
}

func splitRepo(full string) (string, string, error) {
	parts := strings.SplitN(full, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("repo %q must be owner/repo", full)
	}

	return parts[0], parts[1], nil
}

func getBranchSHA(ctx context.Context, c *gh.Client, owner, repo, branch string) (string, error) {
	ref, _, err := c.Git.GetRef(ctx, owner, repo, "refs/heads/"+branch)
	if err != nil {
		return "", err
	}

	return ref.GetObject().GetSHA(), nil
}

// pathExists reports whether path (file or directory) exists at ref.
func pathExists(ctx context.Context, c *gh.Client, owner, repo, path, ref string) (bool, error) {
	_, _, resp, err := c.Repositories.GetContents(
		ctx, owner, repo, path, &gh.RepositoryContentGetOptions{Ref: ref},
	)
	if err == nil {
		return true, nil
	}

	if resp != nil && resp.StatusCode == http.StatusNotFound {
		return false, nil
	}

	return false, err
}

func createBranch(ctx context.Context, c *gh.Client, owner, repo, branch, fromSHA string) error {
	newRef := &gh.Reference{
		Ref: gh.Ptr("refs/heads/" + branch),
		Object: &gh.GitObject{
			SHA: gh.Ptr(fromSHA),
		},
	}

	_, resp, err := c.Git.CreateRef(ctx, owner, repo, newRef)
	if err == nil {
		return nil
	}

	// 422 Unprocessable typically means "ref already exists" — surface it
	// as a friendlier error so the caller can retry with a fresh suffix.
	if resp != nil && resp.StatusCode == http.StatusUnprocessableEntity {
		return fmt.Errorf("branch already exists")
	}

	return err
}

func authorName(req PRRequest) string {
	if req.AuthorLogin != "" {
		return req.AuthorLogin
	}

	return "ewws-platform-ui"
}

func authorEmail(req PRRequest) string {
	if req.AuthorEmail != "" {
		return req.AuthorEmail
	}

	return req.AuthorLogin + "@users.noreply.github.com"
}
