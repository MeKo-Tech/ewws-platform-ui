// Package github wraps the bits of go-github we use:
//   - Fetching the logged-in user's info
//   - Checking team membership (MeKo-Tech:ewws)
//   - Creating a feature branch + commit + PR in ewws-apps-registry
//
// All client operations take an *http.Client preconfigured with the
// user's OAuth token — we never hold app-wide PAT credentials.
package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	gh "github.com/google/go-github/v68/github"
)

// UserInfo is the minimal user profile we keep in the session.
type UserInfo struct {
	Login     string
	Name      string
	Email     string
	AvatarURL string
}

// FetchUser calls GET /user with the supplied OAuth-authenticated client.
func FetchUser(ctx context.Context, httpClient *http.Client) (*UserInfo, error) {
	client := gh.NewClient(httpClient)

	u, _, err := client.Users.Get(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("github: get user: %w", err)
	}

	return &UserInfo{
		Login:     u.GetLogin(),
		Name:      u.GetName(),
		Email:     u.GetEmail(),
		AvatarURL: u.GetAvatarURL(),
	}, nil
}

// IsTeamMember returns true if the user is a member of `org/team`.
//
// We rely on the user's own token + GET /orgs/{org}/teams/{team}/memberships/{user}.
// 200 with state="active" → member. 404 → not a member.
func IsTeamMember(
	ctx context.Context,
	httpClient *http.Client,
	org, team, login string,
) (bool, error) {
	if org == "" || team == "" || login == "" {
		return false, errors.New("github: org/team/login required")
	}

	client := gh.NewClient(httpClient)

	membership, resp, err := client.Teams.GetTeamMembershipBySlug(ctx, org, team, login)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusNotFound {
			return false, nil
		}

		return false, fmt.Errorf("github: team membership: %w", err)
	}

	return membership.GetState() == "active", nil
}

// IsOrgMember checks GET /orgs/{org}/members/{login}.
//
// Returns true for 204, false for 404, error otherwise.
func IsOrgMember(ctx context.Context, httpClient *http.Client, org, login string) (bool, error) {
	client := gh.NewClient(httpClient)

	isMember, _, err := client.Organizations.IsMember(ctx, org, login)
	if err != nil {
		return false, fmt.Errorf("github: org membership: %w", err)
	}

	return isMember, nil
}
