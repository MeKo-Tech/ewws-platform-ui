package views

import "github.com/MeKo-Tech/ewws-platform-ui/internal/ewwsauth"

// AuthScopeState is the outcome of the ewws-auth scope lookup on the
// detail page.
type AuthScopeState int

const (
	// AuthScopeDisabled — no ewws-auth credentials configured.
	AuthScopeDisabled AuthScopeState = iota
	// AuthScopeOK — Scope is populated.
	AuthScopeOK
	// AuthScopeNotFound — ewws-auth answered 404 for the claimed scope.
	AuthScopeNotFound
	// AuthScopeUnreachable — any other error or timeout.
	AuthScopeUnreachable
)

// AuthScopePanel feeds the "ewws-auth-Scope" section of the detail page.
// A nil panel means the claim has no scope and no section is rendered.
type AuthScopePanel struct {
	Name  string // scope name from the claim
	State AuthScopeState
	Scope *ewwsauth.Scope // set when State == AuthScopeOK
}

// scopeURLGroup is the URLs of one environment.
type scopeURLGroup struct {
	Environment string
	URLs        []ewwsauth.ScopeURL
}

// groupScopeURLs groups URLs by environment: prod first, then staging,
// then any other environment in first-seen order.
func groupScopeURLs(urls []ewwsauth.ScopeURL) []scopeURLGroup {
	byEnv := map[string][]ewwsauth.ScopeURL{}

	var order []string

	for _, u := range urls {
		if _, seen := byEnv[u.Environment]; !seen {
			order = append(order, u.Environment)
		}

		byEnv[u.Environment] = append(byEnv[u.Environment], u)
	}

	groups := make([]scopeURLGroup, 0, len(order))

	for _, env := range []string{"prod", "staging"} {
		if us, ok := byEnv[env]; ok {
			groups = append(groups, scopeURLGroup{Environment: env, URLs: us})
		}
	}

	for _, env := range order {
		if env == "prod" || env == "staging" {
			continue
		}

		groups = append(groups, scopeURLGroup{Environment: env, URLs: byEnv[env]})
	}

	return groups
}
