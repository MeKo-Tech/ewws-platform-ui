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
	// Env and EnvLabel name the ewws-auth deployment the row comes from
	// ("prod"/"Prod", "staging"/"Staging"). Not to be confused with
	// Issuer, the token issuer inside that deployment.
	Env        string
	EnvLabel   string
	Issuer     string // "ewws-auth" or "keycloak"
	ClientID   string // "" = logins that named no client
	Registered bool   // client_id is registered in ewws-auth
	Name       string // display name from ewws-auth; may be ""
	// AppSlug is the registry app whose auth.client_ids names ClientID,
	// "" when no claim does. AppStages lists the stages it is named for
	// ("staging", "prod").
	AppSlug   string
	AppStages []string
	// Visits marks an "app:<slug>" row: beacon-recorded visits of an app
	// without login, counted as uses (its logins are always 0).
	Visits  bool
	Last1d  UsageWindow
	Last7d  UsageWindow
	Last30d UsageWindow
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
	// NoLogin lists registry apps without auth.client_ids ("kein Login"),
	// sorted by slug. Deliberate for many apps — rendered neutrally, not
	// as an error.
	NoLogin []UsageNoLoginApp
	// VisitsOK is true when ewws-auth delivered the beacon visits of the
	// apps without login (GET /api/activity/apps); the NoLogin entries
	// then carry numbers. False renders the plain list plus VisitsHint.
	VisitsOK bool
	// VisitsDays is the window of the visit numbers, in days.
	VisitsDays int
	// VisitsHint explains why the visit numbers are missing ("" when
	// VisitsOK or when there are no apps without login).
	VisitsHint string
	// Silent lists registry client_ids that have no activity at all in
	// ewws-auth's 30-day window.
	Silent []UsageSilent

	// Env is the selected environment: "prod", "staging" or "all".
	Env string
	// EnvTabs is the environment switch; empty (not rendered) when only
	// one ewws-auth is configured.
	EnvTabs []UsageEnvTab
	// ShowEnv adds the "Umgebung" column (Env = "all").
	ShowEnv bool
	// EnvNotices are per-environment failures in Env = "all": that
	// environment's rows are missing, the others render.
	EnvNotices []UsageEnvNotice

	// Persons is the per-person comparison across environments (only in
	// Env = "all"), sorted by total use, descending. PersonEnvs labels
	// the columns of each UsagePerson.PerEnv, in the same order.
	Persons     []UsagePerson
	PersonEnvs  []string
	PersonsFrom string // YYYY-MM-DD, inclusive
	PersonsTo   string
	// PersonsHint explains missing or incomplete per-person numbers.
	PersonsHint string
}

// UsageEnvTab is one link of the environment switch.
type UsageEnvTab struct {
	Label  string
	URL    string
	Active bool
}

// UsageEnvNotice is the failure of one environment's activity lookup.
type UsageEnvNotice struct {
	EnvLabel string
	State    UsageState
	Message  string
}

// UsagePerson is one person's activity per environment.
type UsagePerson struct {
	Name      string // display name, else the account id
	AccountID string // set when Name is empty
	PerEnv    []UsagePersonEnv
	Total     int // logins + refreshes + uses over all environments
	// Dominant is the label of the environment with the larger share,
	// "" when equal.
	Dominant string
}

// UsagePersonEnv is one person's use in one environment, summed over all
// clients of that environment.
type UsagePersonEnv struct {
	Logins    int
	Refreshes int
	Uses      int
	Total     int // Logins + Refreshes + Uses
	// ActiveDays is the largest active-day count over the person's
	// clients — a lower bound of the distinct days in that environment.
	ActiveDays int
}

// UsageNoLoginApp is a registry app without auth.client_ids together with
// the visits ewws-auth's beacon recorded for it (zero when none).
type UsageNoLoginApp struct {
	EnvLabel   string // set in Env = "all"
	Slug       string
	Accounts   int       // distinct identified persons ("Personen")
	Devices    int       // distinct anonymous devices ("anonyme Geräte")
	Views      int       // all recorded visits ("Aufrufe")
	LastSeenAt time.Time // zero = never seen
	// DetailURL is the per-person drill-down, the client detail of
	// "app:<slug>", e.g. "/usage/app:fuegekompass".
	DetailURL string
}

// UsageSilent is a registry client_id ewws-auth reported no activity for.
type UsageSilent struct {
	// EnvLabel names the environment the stage logs in to ("Prod",
	// "Staging"); "" when only one ewws-auth is configured.
	EnvLabel string
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
	// Env is the environment key ("prod", "staging"); EnvLabel is shown
	// when several environments are configured.
	Env      string
	EnvLabel string
	// BackURL is the overview link, carrying the environment.
	BackURL string
}
