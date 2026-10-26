package views

import "time"

// UsageState is the outcome of an ewws-auth activity lookup on /usage.
type UsageState int

const (
	// UsageNotConfigured — EWWS_AUTH_URL or the machine credentials are
	// unset; the page says "nicht konfiguriert".
	UsageNotConfigured UsageState = iota
	// UsageOK — the data fields are populated.
	UsageOK
	// UsageForbidden — ewws-auth answered 403: the platform UI's machine
	// account lacks the auth:activity:r grant.
	UsageForbidden
	// UsageInvalid — ewws-auth (or the handler) rejected the request as
	// invalid (400), e.g. a bad date range. Message says why.
	UsageInvalid
	// UsageError — anything else (token fetch failed, 401, 5xx, timeout).
	// Message is a short, user-safe description; it never contains a token.
	UsageError
)

// UsageWindow is the activity of one window (1d / 7d / 30d).
type UsageWindow struct {
	Users  int
	Logins int
}

// UsageRow is one client_id in the overview table.
type UsageRow struct {
	Issuer     string // "ewws-auth" or "keycloak"
	ClientID   string // "" = logins that named no client
	Registered bool   // client_id is registered in ewws-auth
	Name       string // display name from ewws-auth; may be ""
	// AppSlug is the registry app whose auth.client_ids names ClientID,
	// "" when no claim does. AppStages lists the stages it is named for
	// ("staging", "prod").
	AppSlug   string
	AppStages []string
	Last1d    UsageWindow
	Last7d    UsageWindow
	Last30d   UsageWindow
	// DetailURL is the server-built link to the per-client page,
	// e.g. "/usage/abc?issuer=ewws-auth".
	DetailURL string
}

// UsageOverview feeds GET /usage.
type UsageOverview struct {
	State   UsageState
	Message string // set for UsageInvalid / UsageError
	Today   string // YYYY-MM-DD (UTC) as reported by ewws-auth
	Rows    []UsageRow
	// NoLogin lists registry apps without auth.client_ids ("kein Login").
	// Deliberate for many apps — rendered neutrally, not as an error.
	NoLogin []string
	// Silent lists registry client_ids that have no activity at all in
	// ewws-auth's 30-day window.
	Silent []UsageSilent
}

// UsageSilent is a registry client_id ewws-auth reported no activity for.
type UsageSilent struct {
	AppSlug  string
	Stage    string
	ClientID string
}

// UsageAccount is one user in the per-client detail.
type UsageAccount struct {
	AccountID  string
	Name       string
	PersonioID string // "" when the account has none
	Logins     int
	Refreshes  int
	Uses       int
	ActiveDays int
	LastSeenAt time.Time
}

// UsageDetail feeds GET /usage/{client_id}.
type UsageDetail struct {
	State      UsageState
	Message    string
	Issuer     string // "" = all issuers
	ClientID   string // "" = logins that named no client
	Registered bool
	Name       string
	AppSlug    string
	From       string // YYYY-MM-DD, inclusive
	To         string // YYYY-MM-DD, inclusive
	Accounts   []UsageAccount
	// FormAction is the path the date-range form submits to (GET).
	FormAction string
}
