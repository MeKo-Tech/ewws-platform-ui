package handlers

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/ewwsauth"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

// Environment keys of /usage?env=. An environment is one ewws-auth
// deployment; each records only its own logins. Not to be confused with
// the issuer (ewws-auth vs keycloak) inside one deployment.
const (
	UsageEnvProd    = "prod"
	UsageEnvStaging = "staging"
	usageEnvAll     = "all"

	// personsMaxInFlight bounds the per-client drill-downs fetched for
	// the "Personen nach Umgebung" section; personsCallTimeout bounds
	// each of them.
	personsMaxInFlight = 6
	personsCallTimeout = 8 * time.Second
)

// UsageEnv is one ewws-auth deployment /usage reads activity from.
type UsageEnv struct {
	Key    string // UsageEnvProd or UsageEnvStaging
	Label  string // "Prod", "Staging"
	Client *ewwsauth.ActivityClient
}

// envClients is one environment's successful overview reply.
type envClients struct {
	Env UsageEnv
	Res *ewwsauth.ClientsResult
}

// selectEnvs resolves ?env= against the configured environments. ""
// selects the first (prod, or the only one configured); "all" — when
// allowed — selects every environment and, with a single one configured,
// is just that one. ok is false for an unknown key.
func selectEnvs(envs []UsageEnv, raw string, allowAll bool) (sel []UsageEnv, key string, ok bool) {
	switch {
	case len(envs) == 0:
		return nil, "", false
	case raw == "":
		return envs[:1], envs[0].Key, true
	case raw == usageEnvAll && allowAll:
		if len(envs) == 1 {
			return envs, envs[0].Key, true
		}

		return envs, usageEnvAll, true
	}

	for _, e := range envs {
		if e.Key == raw {
			return []UsageEnv{e}, e.Key, true
		}
	}

	return nil, "", false
}

func invalidEnvMessage(envs []UsageEnv, raw string, allowAll bool) string {
	keys := make([]string, 0, len(envs)+1)
	for _, e := range envs {
		keys = append(keys, e.Key)
	}

	if allowAll && len(envs) > 1 {
		keys = append(keys, usageEnvAll)
	}

	return fmt.Sprintf("Unbekannte Umgebung „%s“ (erlaubt: %s).", raw, strings.Join(keys, ", "))
}

// envTabs builds the environment switch; nil with a single environment.
func envTabs(envs []UsageEnv, active string) []views.UsageEnvTab {
	if len(envs) < 2 {
		return nil
	}

	tabs := make([]views.UsageEnvTab, 0, len(envs)+1)
	for _, e := range envs {
		tabs = append(tabs, views.UsageEnvTab{
			Label:  e.Label,
			URL:    "/usage?env=" + url.QueryEscape(e.Key),
			Active: e.Key == active,
		})
	}

	return append(tabs, views.UsageEnvTab{
		Label:  "Beide",
		URL:    "/usage?env=" + usageEnvAll,
		Active: active == usageEnvAll,
	})
}

// stageEnv maps a registry stage to the environment whose ewws-auth
// serves it: prod logins land in prod, dev and staging in staging.
func stageEnv(stage string) string {
	if stage == "prod" {
		return UsageEnvProd
	}

	return UsageEnvStaging
}

// usageDetailURL is the link to the per-client page of one environment.
func usageDetailURL(clientID, issuer, env string) string {
	q := url.Values{}
	if issuer != "" {
		q.Set("issuer", issuer)
	}

	if env != "" {
		q.Set("env", env)
	}

	u := "/usage/" + url.PathEscape(cmp.Or(clientID, "-"))
	if len(q) > 0 {
		u += "?" + q.Encode()
	}

	return u
}

// fetchPerEnv calls f for every environment concurrently. results[i]
// and errs[i] belong to envs[i]; one environment failing never affects
// another.
func fetchPerEnv[T any](
	ctx context.Context,
	envs []UsageEnv,
	f func(context.Context, *ewwsauth.ActivityClient) (T, error),
) (results []T, errs []error) {
	results = make([]T, len(envs))
	errs = make([]error, len(envs))

	var wg sync.WaitGroup

	for i, e := range envs {
		wg.Go(func() {
			results[i], errs[i] = f(ctx, e.Client)
		})
	}

	wg.Wait()

	return results, errs
}

// personJob is one per-client drill-down for the person comparison.
type personJob struct {
	set      int // index into the envClients slice
	clientID string
}

// collectPersons fills the "Personen nach Umgebung" section. The overview
// API only has per-client counts, so it fetches the per-account
// drill-down (issuer: both) of every client_id with activity in the
// 30-day window, per environment, at most personsMaxInFlight at a time,
// each bounded by personsCallTimeout. A failed call leaves its client
// out and is reported in PersonsHint.
func collectPersons(
	ctx context.Context,
	logger *slog.Logger,
	o *views.UsageOverview,
	sets []envClients,
) {
	var jobs []personJob

	ranges := make([][2]string, len(sets))

	for i, s := range sets {
		from, to, msg := resolveUsageRange("", s.Res.Today, time.Now())
		if msg != "" {
			from, to, _ = resolveUsageRange("", "", time.Now())
		}

		ranges[i] = [2]string{from, to}
		seen := map[string]bool{}

		for _, c := range s.Res.Clients {
			if c.Last30d == (ewwsauth.ActivityWindow{}) || seen[c.ClientID] {
				continue
			}

			seen[c.ClientID] = true
			jobs = append(jobs, personJob{set: i, clientID: c.ClientID})
		}

		o.PersonEnvs = append(o.PersonEnvs, s.Env.Label)
	}

	if len(sets) > 0 {
		o.PersonsFrom, o.PersonsTo = ranges[0][0], ranges[0][1]
	}

	results := make([]*ewwsauth.ClientResult, len(jobs))
	sem := make(chan struct{}, personsMaxInFlight)

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		failed int
	)

	for i, j := range jobs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			cctx, cancel := context.WithTimeout(ctx, personsCallTimeout)
			defer cancel()

			r := ranges[j.set]

			res, err := sets[j.set].Env.Client.Client(cctx, "", j.clientID, r[0], r[1])
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()

				if logger != nil {
					logger.Warn("usage persons: ewws-auth client activity",
						slog.String("env", sets[j.set].Env.Key),
						slog.String("client_id", j.clientID),
						slog.Any("err", err))
				}

				return
			}

			results[i] = res
		})
	}

	wg.Wait()

	o.Persons = aggregatePersons(len(sets), jobs, results, sets)

	if failed > 0 {
		o.PersonsHint = fmt.Sprintf(
			"%d von %d Abrufen bei ewws-auth sind fehlgeschlagen; die Zahlen sind unvollständig.",
			failed, len(jobs),
		)
	}
}

// aggregatePersons sums each person's activity per environment over all
// clients. A person is matched across clients and environments by
// Personio ID, else by display name (case-insensitive), else by account
// id. results[i] belongs to jobs[i]; nil entries (failed calls) are
// skipped.
func aggregatePersons(
	nEnvs int,
	jobs []personJob,
	results []*ewwsauth.ClientResult,
	sets []envClients,
) []views.UsagePerson {
	byKey := map[string]*views.UsagePerson{}

	for i, res := range results {
		if res == nil {
			continue
		}

		env := jobs[i].set

		for _, a := range res.Accounts {
			key := personKey(a)

			p, ok := byKey[key]
			if !ok {
				p = &views.UsagePerson{PerEnv: make([]views.UsagePersonEnv, nEnvs)}
				byKey[key] = p
			}

			if p.Name == "" && a.Name != "" {
				p.Name = a.Name
			}

			if p.AccountID == "" {
				p.AccountID = a.AccountID
			}

			e := &p.PerEnv[env]
			e.Logins += a.Logins
			e.Refreshes += a.Refreshes
			e.Uses += a.Uses
			e.Total += a.Logins + a.Refreshes + a.Uses
			e.ActiveDays = max(e.ActiveDays, a.ActiveDays)
		}
	}

	out := make([]views.UsagePerson, 0, len(byKey))

	for _, p := range byKey {
		best, bestTotal, tie := -1, -1, false

		for i, e := range p.PerEnv {
			p.Total += e.Total

			switch {
			case e.Total > bestTotal:
				best, bestTotal, tie = i, e.Total, false
			case e.Total == bestTotal:
				tie = true
			}
		}

		if best >= 0 && !tie && bestTotal > 0 {
			p.Dominant = sets[best].Env.Label
		}

		out = append(out, *p)
	}

	slices.SortFunc(out, func(a, b views.UsagePerson) int {
		return cmp.Or(
			cmp.Compare(b.Total, a.Total),
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.AccountID, b.AccountID),
		)
	})

	return out
}

func personKey(a ewwsauth.AccountActivity) string {
	switch {
	case a.PersonioID != nil:
		return "p:" + strconv.Itoa(*a.PersonioID)
	case strings.TrimSpace(a.Name) != "":
		return "n:" + strings.ToLower(strings.TrimSpace(a.Name))
	default:
		return "a:" + a.AccountID
	}
}
