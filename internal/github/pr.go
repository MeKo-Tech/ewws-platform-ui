package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	gh "github.com/google/go-github/v68/github"
)

// PRRequest is the input to OpenClaimPR.
type PRRequest struct {
	Repo        string // "owner/repo"
	Slug        string
	YAMLContent []byte
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

// OpenClaimPR creates a feature branch from `main`, commits the new
// `apps/<slug>.yaml`, and opens a PR — all via the user's OAuth client.
//
// Steps:
//  1. Look up the SHA of `main` (refs/heads/main).
//  2. Create branch ref `refs/heads/<branch>` pointing at that SHA.
//  3. PUT the file (Contents API does branch + commit in one call).
//  4. Open PR.
func OpenClaimPR(ctx context.Context, httpClient *http.Client, req PRRequest) (*PRResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}

	owner, repo, err := splitRepo(req.Repo)
	if err != nil {
		return nil, err
	}

	client := gh.NewClient(httpClient)

	baseSHA, err := getBranchSHA(ctx, client, owner, repo, req.BaseBranch)
	if err != nil {
		return nil, fmt.Errorf("base branch %q: %w", req.BaseBranch, err)
	}

	if err := createBranch(ctx, client, owner, repo, req.Branch, baseSHA); err != nil {
		return nil, fmt.Errorf("create branch %q: %w", req.Branch, err)
	}

	path := fmt.Sprintf("apps/%s.yaml", req.Slug)

	commitMsg := fmt.Sprintf("feat: claim %s", req.Slug)

	commit := &gh.RepositoryContentFileOptions{
		Message: gh.Ptr(commitMsg),
		Content: req.YAMLContent,
		Branch:  gh.Ptr(req.Branch),
		Author: &gh.CommitAuthor{
			Name:  gh.Ptr(authorName(req)),
			Email: gh.Ptr(authorEmail(req)),
			Date:  &gh.Timestamp{Time: time.Now().UTC()},
		},
	}

	if _, _, err := client.Repositories.CreateFile(ctx, owner, repo, path, commit); err != nil {
		return nil, fmt.Errorf("create file %s: %w", path, err)
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
		return errors.New("Repo is required")
	}

	if r.Slug == "" {
		return errors.New("Slug is required")
	}

	if len(r.YAMLContent) == 0 {
		return errors.New("YAMLContent is required")
	}

	if r.Branch == "" {
		return errors.New("Branch is required")
	}

	if r.Title == "" {
		return errors.New("Title is required")
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
