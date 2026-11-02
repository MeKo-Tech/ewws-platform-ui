package handlers

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/config"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/ewwsauth"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

const (
	usageDateLayout   = "2006-01-02"
	usageDefaultDays  = 30
	usageMaxRangeDays = 366

	usageForbiddenMessage = "Der Maschinen-Account der Plattform-UI hat in ewws-auth " +
		"nicht die Berechtigung auth:activity:r."
	usageErrorMessage = "ewws-auth ist nicht erreichbar oder hat mit einem Fehler geantwortet."

	// usageVisitsDays is the window of the "kein Login" visit numbers,
	// matching the 30-day window of the login table.
	usageVisitsDays = 30
	// visitsClientPrefix marks the client_id under which ewws-auth's
	// beacon records the visits of an app without login: "app:<slug>".
	visitsClientPrefix = "app:"

	visitsNotFoundHint = "Aufrufzahlen für Apps ohne Login liefert diese ewws-auth-Version " +
		"noch nicht (/api/activity/apps fehlt)."
	visitsForbiddenHint = "Der Maschinen-Account darf die Aufrufzahlen für Apps ohne Login " +
		"nicht lesen (auth:activity:r)."
	visitsErrorHint = "Aufrufzahlen für Apps ohne Login konnten nicht geladen werden."
)

// FetchAppsFunc loads the registry apps; tests stub it.
type FetchAppsFunc func(ctx context.Context) ([]registry.App, error)

// Usage renders GET /usage: login activity per client_id from ewws-auth,
// linked to the registry apps via auth.client_ids. Admins only.
type Usage struct {
	Cfg      *config.Config
	Activity *ewwsauth.ActivityClient // may be nil or disabled
	Logger   *slog.Logger
	// FetchApps defaults to registry.FetchAppsFromGitHub(Cfg.AppsRegistryRepo).
	FetchApps FetchAppsFunc
}

// UsageClient renders GET /usage/{client_id}: per-account activity of
// one client in a date range. Admins only.
type UsageClient struct {
	Cfg       *config.Config
	Activity  *ewwsauth.ActivityClient // may be nil or disabled
	Logger    *slog.Logger
	FetchApps FetchAppsFunc
	// Now defaults to time.Now; the default range ends on its UTC date.
	Now func() time.Time
}

// requireAdmin redirects anonymous users to the login and refuses
// non-admins. It returns the page props when the request may proceed.
func requireAdmin(w http.ResponseWriter, r *http.Request, title string) (views.PageProps, bool) {
	user := middleware.UserFromContext(r.Context())
	if user == nil {
		http.Redirect(w, r, "/auth/login", http.StatusSeeOther)
		return views.PageProps{}, false
	}

	if !user.IsAdmin {
		http.Error(w, "Nur für Admins.", http.StatusForbidden)
		return views.PageProps{}, false
	}

	return views.PageProps{Title: title, User: userFromCtx(r.Context())}, true
}

func (h Usage) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	props, ok := requireAdmin(w, r, "Nutzung")
	if !ok {
		return
	}

	render(w, r, views.Usage(props, h.overview(r.Context())))
}

// overview fetches the ewws-auth activity and links it to the registry.
// The registry is only read when the activity came back; its failure is
// logged and the page renders without app links.
func (h Usage) overview(ctx context.Context) views.UsageOverview {
	res, err := h.Activity.Clients(ctx)
	if err != nil {
		state, msg := usageErrorState(h.Logger, "usage overview", err)
		return views.UsageOverview{State: state, Message: msg}
	}

	apps := loadRegistryApps(ctx, h.Logger, h.Cfg, h.FetchApps)
	o := buildUsageOverview(res, apps)

	if len(o.NoLogin) > 0 {
		visits, err := h.Activity.Apps(ctx, usageVisitsDays)
		if err != nil {
			o.VisitsHint = visitsHint(ctx, h.Logger, err)
		} else {
			applyVisits(&o, visits)
		}
	}

	return o
}

// visitsHint maps a failed GET /api/activity/apps to the hint shown
// above the plain "kein Login" list. A 404 is expected until ewws-auth
// ships the endpoint and is not logged as a warning.
func visitsHint(ctx context.Context, logger *slog.Logger, err error) string {
	var apiErr *ewwsauth.APIError

	notFound := errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound

	if logger != nil {
		level := slog.LevelWarn
		if notFound {
			level = slog.LevelInfo
		}

		logger.Log(ctx, level, "usage overview: ewws-auth app visits",
			slog.Any("err", err))
	}

	switch {
	case notFound:
		return visitsNotFoundHint
	case errors.Is(err, ewwsauth.ErrForbidden):
		return visitsForbiddenHint
	default:
		return visitsErrorHint
	}
}

// applyVisits copies the beacon numbers onto the "kein Login" apps.
// Slugs ewws-auth reports that are not among them are ignored.
func applyVisits(o *views.UsageOverview, res *ewwsauth.AppsResult) {
	bySlug := make(map[string]ewwsauth.AppVisits, len(res.Apps))
	for _, a := range res.Apps {
		bySlug[a.Slug] = a
	}

	for i := range o.NoLogin {
		a, ok := bySlug[o.NoLogin[i].Slug]
		if !ok {
			continue
		}

		o.NoLogin[i].Accounts = a.Accounts
		o.NoLogin[i].Devices = a.Devices
		o.NoLogin[i].Views = a.Views

		if a.LastSeenAt != nil {
			o.NoLogin[i].LastSeenAt = *a.LastSeenAt
		}
	}

	o.VisitsOK = true
	o.VisitsDays = cmp.Or(res.Days, usageVisitsDays)
}

func (h UsageClient) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	props, ok := requireAdmin(w, r, "Nutzung")
	if !ok {
		return
	}

	now := time.Now
	if h.Now != nil {
		now = h.Now
	}

	q := r.URL.Query()
	d := h.detail(
		r.Context(),
		r.PathValue("client_id"),
		q.Get("issuer"),
		q.Get("from"),
		q.Get("to"),
		now(),
	)

	render(w, r, views.UsageDetailPage(props, d))
}

// detail validates the request locally (no ewws-auth call on invalid
// input) and fetches the per-account activity. pathID is the
// unescaped path value; "-" stands for the empty client_id.
func (h UsageClient) detail(
	ctx context.Context,
	pathID, issuer, fromRaw, toRaw string,
	now time.Time,
) views.UsageDetail {
	clientID := pathID
	if clientID == "-" {
		clientID = ""
	}

	d := views.UsageDetail{
		Issuer:     issuer,
		ClientID:   clientID,
		From:       fromRaw,
		To:         toRaw,
		FormAction: "/usage/" + url.PathEscape(cmp.Or(clientID, "-")),
	}

	if !h.Activity.Enabled() {
		d.State = views.UsageNotConfigured
		return d
	}

	if msg := validateIssuer(issuer); msg != "" {
		d.State, d.Message = views.UsageInvalid, msg
		return d
	}

	from, to, msg := resolveUsageRange(fromRaw, toRaw, now)
	if msg != "" {
		d.State, d.Message = views.UsageInvalid, msg
		return d
	}

	d.From, d.To = from, to

	res, err := h.Activity.Client(ctx, issuer, clientID, from, to)
	if err != nil {
		d.State, d.Message = usageErrorState(h.Logger, "usage detail", err)
		return d
	}

	d.State = views.UsageOK
	d.Registered = res.Registered
	d.Name = res.Name
	d.From = cmp.Or(res.From, from)
	d.To = cmp.Or(res.To, to)
	d.Accounts = make([]views.UsageAccount, 0, len(res.Accounts))

	for _, a := range res.Accounts {
		personio := ""
		if a.PersonioID != nil {
			personio = strconv.Itoa(*a.PersonioID)
		}

		d.Accounts = append(d.Accounts, views.UsageAccount{
			AccountID:  a.AccountID,
			Name:       a.Name,
			PersonioID: personio,
			Logins:     a.Logins,
			Refreshes:  a.Refreshes,
			Uses:       a.Uses,
			ActiveDays: a.ActiveDays,
			LastSeenAt: a.LastSeenAt,
		})
	}

	if clientID != "" {
		apps := loadRegistryApps(ctx, h.Logger, h.Cfg, h.FetchApps)
		d.AppSlug, _ = linkClient(clientLinks(apps), registrySlugs(apps), clientID)
	}

	return d
}

func validateIssuer(issuer string) string {
	switch issuer {
	case "", "ewws-auth", "keycloak":
		return ""
	default:
		return fmt.Sprintf("Unbekannter Issuer „%s“ (erlaubt: ewws-auth, keycloak).", issuer)
	}
}

// resolveUsageRange applies the defaults (to = today UTC, from = to − 29
// days, i.e. 30 days inclusive) and checks the range the way ewws-auth
// does. msg is a German user-facing reason when the range is invalid.
func resolveUsageRange(fromRaw, toRaw string, now time.Time) (from, to, msg string) {
	toDate := now.UTC().Truncate(24 * time.Hour)

	if toRaw != "" {
		t, err := time.Parse(usageDateLayout, toRaw)
		if err != nil {
			return "", "", fmt.Sprintf("Ungültiges Enddatum „%s“ (erwartet JJJJ-MM-TT).", toRaw)
		}

		toDate = t
	}

	fromDate := toDate.AddDate(0, 0, -(usageDefaultDays - 1))

	if fromRaw != "" {
		f, err := time.Parse(usageDateLayout, fromRaw)
		if err != nil {
			return "", "", fmt.Sprintf("Ungültiges Startdatum „%s“ (erwartet JJJJ-MM-TT).", fromRaw)
		}

		fromDate = f
	}

	if fromDate.After(toDate) {
		return "", "", "Das Startdatum liegt nach dem Enddatum."
	}

	if days := int(toDate.Sub(fromDate).Hours()/24) + 1; days > usageMaxRangeDays {
		return "", "", fmt.Sprintf(
			"Der Zeitraum umfasst %d Tage; erlaubt sind höchstens %d.",
			days,
			usageMaxRangeDays,
		)
	}

	return fromDate.Format(usageDateLayout), toDate.Format(usageDateLayout), ""
}

// usageErrorState maps a client error to the page state and a
// user-safe message. Upstream error text never reaches the page except
// the message of a 400, which is ewws-auth's own validation reason.
func usageErrorState(logger *slog.Logger, op string, err error) (views.UsageState, string) {
	if errors.Is(err, ewwsauth.ErrNotConfigured) {
		return views.UsageNotConfigured, ""
	}

	// The client's errors never carry the token or credentials.
	if logger != nil {
		logger.Warn(op+": ewws-auth activity", slog.Any("err", err))
	}

	if errors.Is(err, ewwsauth.ErrForbidden) {
		return views.UsageForbidden, usageForbiddenMessage
	}

	var apiErr *ewwsauth.APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
		return views.UsageInvalid, cmp.Or(apiErr.Message, "Ungültige Anfrage.")
	}

	return views.UsageError, usageErrorMessage
}

func loadRegistryApps(
	ctx context.Context,
	logger *slog.Logger,
	cfg *config.Config,
	fetch FetchAppsFunc,
) []registry.App {
	if fetch == nil {
		if cfg == nil {
			return nil
		}

		repo := cfg.AppsRegistryRepo
		fetch = func(ctx context.Context) ([]registry.App, error) {
			return registry.FetchAppsFromGitHub(ctx, repo)
		}
	}

	apps, err := fetch(ctx)
	if err != nil {
		if logger != nil {
			logger.Warn("usage: registry fetch failed; rendering without app links",
				slog.Any("err", err))
		}

		return nil
	}

	return apps
}

type clientLink struct {
	slug, stage string
}

// clientLinks maps each registry client_id to the (app, stage) pairs
// naming it, sorted by slug and then stage (staging before prod).
func clientLinks(apps []registry.App) map[string][]clientLink {
	out := map[string][]clientLink{}

	for _, a := range apps {
		if a.Auth == nil {
			continue
		}

		for stage, id := range a.Auth.ClientIDs {
			if id != "" {
				out[id] = append(out[id], clientLink{slug: a.Slug, stage: stage})
			}
		}
	}

	for _, links := range out {
		slices.SortFunc(links, compareLinks)
	}

	return out
}

func registrySlugs(apps []registry.App) map[string]bool {
	out := make(map[string]bool, len(apps))
	for _, a := range apps {
		out[a.Slug] = true
	}

	return out
}

// linkClient resolves the registry app a client_id belongs to: the
// first app naming it in auth.client_ids (with the stages it is named
// for), else — for "app:<slug>", the beacon visits of an app without
// login — the registry app with that slug (no stages). "" when neither
// applies.
func linkClient(
	links map[string][]clientLink,
	slugs map[string]bool,
	clientID string,
) (slug string, stages []string) {
	if clientID == "" {
		return "", nil
	}

	if l := links[clientID]; len(l) > 0 {
		slug = l[0].slug

		for _, x := range l {
			if x.slug == slug {
				stages = append(stages, x.stage)
			}
		}

		return slug, stages
	}

	if s, ok := strings.CutPrefix(clientID, visitsClientPrefix); ok && slugs[s] {
		return s, nil
	}

	return "", nil
}

func compareLinks(a, b clientLink) int {
	return cmp.Or(
		cmp.Compare(a.slug, b.slug),
		cmp.Compare(stageRank(a.stage), stageRank(b.stage)),
		cmp.Compare(a.stage, b.stage),
	)
}

func stageRank(stage string) int {
	switch stage {
	case "staging":
		return 0
	case "prod":
		return 1
	default:
		return 2
	}
}

// buildUsageOverview turns the ewws-auth rows plus the registry into the
// overview model:
//   - each row gets the registry app (and its stages) naming its client_id;
//     should several apps name the same id, the first slug wins;
//   - "app:<slug>" rows (beacon visits) link to the app with that slug;
//   - NoLogin lists apps without any auth.client_ids;
//   - Silent lists registry client_ids without 30-day activity — no row
//     at all, or only rows whose 30-day window is empty.
func buildUsageOverview(res *ewwsauth.ClientsResult, apps []registry.App) views.UsageOverview {
	links := clientLinks(apps)
	slugs := registrySlugs(apps)
	active := map[string]bool{}

	o := views.UsageOverview{State: views.UsageOK, Today: res.Today}

	for _, c := range res.Clients {
		if c.Last30d != (ewwsauth.ActivityWindow{}) {
			active[c.ClientID] = true
		}

		row := views.UsageRow{
			Issuer:     c.Issuer,
			ClientID:   c.ClientID,
			Registered: c.Registered,
			Name:       c.Name,
			Last1d:     views.UsageWindow{Users: c.Last1d.Users, Logins: c.Last1d.Logins},
			Last7d:     views.UsageWindow{Users: c.Last7d.Users, Logins: c.Last7d.Logins},
			Last30d:    views.UsageWindow{Users: c.Last30d.Users, Logins: c.Last30d.Logins},
			DetailURL: "/usage/" + url.PathEscape(cmp.Or(c.ClientID, "-")) +
				"?issuer=" + url.QueryEscape(c.Issuer),
		}

		row.AppSlug, row.AppStages = linkClient(links, slugs, c.ClientID)
		row.Visits = strings.HasPrefix(c.ClientID, visitsClientPrefix)

		o.Rows = append(o.Rows, row)
	}

	slices.SortStableFunc(o.Rows, func(a, b views.UsageRow) int {
		return cmp.Or(
			cmp.Compare(b.Last30d.Logins, a.Last30d.Logins),
			cmp.Compare(a.Name, b.Name),
			cmp.Compare(a.ClientID, b.ClientID),
			cmp.Compare(a.Issuer, b.Issuer),
		)
	})

	for _, a := range apps {
		if a.Auth == nil || !hasClientID(a.Auth.ClientIDs) {
			o.NoLogin = append(o.NoLogin, views.UsageNoLoginApp{
				Slug:      a.Slug,
				DetailURL: "/usage/" + url.PathEscape(visitsClientPrefix+a.Slug),
			})
		}
	}

	slices.SortFunc(o.NoLogin, func(a, b views.UsageNoLoginApp) int {
		return cmp.Compare(a.Slug, b.Slug)
	})

	for id, l := range links {
		if active[id] {
			continue
		}

		for _, x := range l {
			o.Silent = append(
				o.Silent,
				views.UsageSilent{AppSlug: x.slug, Stage: x.stage, ClientID: id},
			)
		}
	}

	slices.SortFunc(o.Silent, func(a, b views.UsageSilent) int {
		return compareLinks(clientLink{a.AppSlug, a.Stage}, clientLink{b.AppSlug, b.Stage})
	})

	return o
}

func hasClientID(ids map[string]string) bool {
	for _, id := range ids {
		if id != "" {
			return true
		}
	}

	return false
}
