package registry

import (
	"fmt"
	"net/url"
	"strings"
)

// ParseGitHubURL extracts (owner, repo) from a `https://github.com/<owner>/<repo>`
// URL. Tolerates trailing slashes and `.git` suffixes.
func ParseGitHubURL(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", "", fmt.Errorf("parse url: %w", err)
	}

	if u.Host != "github.com" {
		return "", "", fmt.Errorf("not a github.com URL: %s", raw)
	}

	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("path missing owner/repo: %s", u.Path)
	}

	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	if owner == "" || repo == "" {
		return "", "", fmt.Errorf("empty owner or repo: %s", raw)
	}

	return owner, repo, nil
}
