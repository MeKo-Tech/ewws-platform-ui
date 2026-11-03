package handlers

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/ewwsauth"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

// twoEnvs wraps a prod and a staging activity client.
func twoEnvs(prod, staging *ewwsauth.ActivityClient) []UsageEnv {
	return []UsageEnv{
		{Key: UsageEnvProd, Label: "Prod", Client: prod},
		{Key: UsageEnvStaging, Label: "Staging", Client: staging},
	}
}

// routes answers fake ewws-auth paths from a map; unknown paths are 404.
func routes(t *testing.T, replies map[string]string) http.HandlerFunc {
	t.Helper()

	return func(w http.ResponseWriter, r *http.Request) {
		body, ok := replies[r.URL.Path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		_, _ = w.Write([]byte(body))
	}
}

func TestUsageEnvParamValidation(t *testing.T) {
	ac, apiCalls := fakeAuth(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("an invalid env must not reach ewws-auth")
		w.WriteHeader(http.StatusInternalServerError)
	})

	two := twoEnvs(ac, ac)

	cases := []struct {
		name   string
		h      http.Handler
		target string
	}{
		{
			"unknown env",
			Usage{Envs: two, Logger: discardLogger, FetchApps: noApps},
			"/usage?env=dev",
		},
		{
			"staging not configured",
			Usage{Envs: prodEnv(ac), Logger: discardLogger, FetchApps: noApps},
			"/usage?env=staging",
		},
		{
			"detail unknown env",
			UsageClient{Envs: two, Logger: discardLogger, FetchApps: noApps},
			"/usage/abc?env=qa",
		},
		{
			"detail has no all",
			UsageClient{Envs: two, Logger: discardLogger, FetchApps: noApps},
			"/usage/abc?env=all",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveAs(t, tc.h, adminUser, tc.target)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status %d, want 400", rec.Code)
			}

			if !strings.Contains(rec.Body.String(), "Unbekannte Umgebung") {
				t.Error("page does not explain the invalid env")
			}
		})
	}

	if n := apiCalls.Load(); n != 0 {
		t.Errorf("ewws-auth called %d times", n)
	}

	o := Usage{Envs: two, Logger: discardLogger}.overview(context.Background(), "dev")
	if o.State != views.UsageInvalid || len(o.EnvTabs) != 3 {
		t.Errorf("overview = %+v", o)
	}

	// "all" with a single environment is just that environment.
	sel, key, ok := selectEnvs(prodEnv(ac), "all", true)
	if !ok || key != UsageEnvProd || len(sel) != 1 {
		t.Errorf("selectEnvs(all, one env) = %v %q %v", sel, key, ok)
	}

	if _, key, _ := selectEnvs(two, "", true); key != UsageEnvProd {
		t.Errorf("default env = %q, want prod", key)
	}
}

func TestUsageEnvSwitchOnlyWithStaging(t *testing.T) {
	ac, _ := fakeAuth(t, routes(t, map[string]string{
		"/api/activity/clients": `{"today":"2026-11-02","clients":[]}`,
	}))

	o := Usage{Envs: prodEnv(ac), Logger: discardLogger, FetchApps: noApps}.
		overview(context.Background(), "")
	if len(o.EnvTabs) != 0 || o.ShowEnv {
		t.Errorf("single env: tabs %+v, ShowEnv %v", o.EnvTabs, o.ShowEnv)
	}

	o = Usage{Envs: twoEnvs(ac, ac), Logger: discardLogger, FetchApps: noApps}.
		overview(context.Background(), "staging")

	var active []string

	for _, tab := range o.EnvTabs {
		if tab.Active {
			active = append(active, tab.Label)
		}
	}

	if len(o.EnvTabs) != 3 || !slices.Equal(active, []string{"Staging"}) || o.ShowEnv {
		t.Errorf("tabs = %+v", o.EnvTabs)
	}
}

const (
	prodClients = `{"today":"2026-11-02","clients":[
		{"issuer":"ewws-auth","client_id":"bridge-prod","name":"Bridge","last_30d":{"users":3,"logins":30}},
		{"issuer":"ewws-auth","client_id":"tool-prod","name":"Tool","last_30d":{"users":1,"logins":1}}]}`
	stagingClients = `{"today":"2026-11-02","clients":[
		{"issuer":"ewws-auth","client_id":"bridge-staging","name":"Bridge","last_30d":{"users":2,"logins":4}},
		{"issuer":"keycloak","client_id":"","last_30d":{"users":1,"logins":2}}]}`
)

func bridgeApps(context.Context) ([]registry.App, error) {
	return []registry.App{
		{Slug: "bridge", Auth: &registry.Auth{ClientIDs: map[string]string{
			"prod": "bridge-prod", "staging": "bridge-staging",
		}}},
		{Slug: "tool", Auth: &registry.Auth{ClientIDs: map[string]string{
			"prod": "tool-prod", "staging": "tool-staging",
		}}},
		// One id for both stages: idle in prod, so it is active only
		// when staging has it.
		{Slug: "shared", Auth: &registry.Auth{ClientIDs: map[string]string{
			"prod": "shared-id", "staging": "shared-id",
		}}},
	}, nil
}

func TestUsageOverviewAllMergesEnvs(t *testing.T) {
	prod, _ := fakeAuth(t, routes(t, map[string]string{"/api/activity/clients": prodClients}))
	staging, _ := fakeAuth(t, routes(t, map[string]string{
		"/api/activity/clients": strings.Replace(
			stagingClients,
			`"clients":[`,
			`"clients":[{"issuer":"ewws-auth","client_id":"shared-id","last_30d":{"users":1,"logins":1}},`,
			1,
		),
	}))

	h := Usage{Envs: twoEnvs(prod, staging), Logger: discardLogger, FetchApps: bridgeApps}

	o := h.overview(context.Background(), "all")
	if o.State != views.UsageOK || !o.ShowEnv || o.Env != "all" || len(o.EnvNotices) != 0 {
		t.Fatalf("overview = %+v", o)
	}

	var got []string
	for _, r := range o.Rows {
		got = append(got, r.EnvLabel+"/"+r.ClientID)
	}

	// Bridge (34 logins over both) first, prod before staging; then the
	// remaining groups by their logins.
	want := []string{
		"Prod/bridge-prod", "Staging/bridge-staging",
		"Staging/", "Staging/shared-id", "Prod/tool-prod",
	}
	if !slices.Equal(got, want) {
		t.Errorf("rows = %q, want %q", got, want)
	}

	for _, r := range o.Rows {
		if !strings.Contains(r.DetailURL, "env="+r.Env) {
			t.Errorf("DetailURL %q lacks env %q", r.DetailURL, r.Env)
		}
	}

	// tool-staging has no staging activity; shared-id is active in
	// staging, so neither of its stages is silent.
	wantSilent := []views.UsageSilent{
		{EnvLabel: "Staging", AppSlug: "tool", Stage: "staging", ClientID: "tool-staging"},
	}
	if !slices.Equal(o.Silent, wantSilent) {
		t.Errorf("Silent = %+v, want %+v", o.Silent, wantSilent)
	}

	// Prod alone judges only prod ids: shared-id is idle there.
	o = h.overview(context.Background(), "prod")

	wantSilent = []views.UsageSilent{
		{EnvLabel: "Prod", AppSlug: "shared", Stage: "prod", ClientID: "shared-id"},
	}
	if !slices.Equal(o.Silent, wantSilent) {
		t.Errorf("prod Silent = %+v, want %+v", o.Silent, wantSilent)
	}
}

func TestUsageOverviewAllOneEnvFails(t *testing.T) {
	prod, _ := fakeAuth(t, routes(t, map[string]string{"/api/activity/clients": prodClients}))
	staging, _ := fakeAuth(t, replyStatus(http.StatusForbidden, `{"errors":[]}`))

	h := Usage{Envs: twoEnvs(prod, staging), Logger: discardLogger, FetchApps: bridgeApps}

	o := h.overview(context.Background(), "all")
	if o.State != views.UsageOK || len(o.Rows) != 2 {
		t.Fatalf("overview = %+v", o)
	}

	if len(o.EnvNotices) != 1 || o.EnvNotices[0].EnvLabel != "Staging" ||
		o.EnvNotices[0].State != views.UsageForbidden {
		t.Errorf("notices = %+v", o.EnvNotices)
	}

	// Staging ids cannot be judged without staging data.
	for _, s := range o.Silent {
		if s.Stage == "staging" {
			t.Errorf("staging id judged without staging data: %+v", s)
		}
	}

	rec := serveAs(t, h, adminUser, "/usage?env=all")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "bridge-prod") || !strings.Contains(body, "auth:activity:r") ||
		!strings.Contains(body, "Umgebung") {
		t.Error("page lacks the prod rows, the staging error or the env column")
	}

	// Both failing: the page shows both errors, no table.
	h.Envs = twoEnvs(staging, staging)
	if o := h.overview(context.Background(), "all"); o.State != views.UsageForbidden ||
		len(o.EnvNotices) != 2 {
		t.Errorf("both failing = %+v", o)
	}
}

func TestUsageOverviewAllVisitsPerEnv(t *testing.T) {
	clients := `{"today":"2026-11-02","clients":[]}`
	prod, _ := fakeAuth(t, routes(t, map[string]string{
		"/api/activity/clients": clients,
		"/api/activity/apps":    `{"days":30,"apps":[{"slug":"lpnt","accounts":1,"devices":2,"views":3}]}`,
	}))
	staging, _ := fakeAuth(t, routes(t, map[string]string{
		"/api/activity/clients": clients,
		"/api/activity/apps":    `{"days":30,"apps":[{"slug":"lpnt","accounts":5,"devices":0,"views":8}]}`,
	}))

	o := Usage{Envs: twoEnvs(prod, staging), Logger: discardLogger, FetchApps: noLoginApps}.
		overview(context.Background(), "all")
	if !o.VisitsOK || o.VisitsHint != "" {
		t.Fatalf("overview = %+v", o)
	}

	var got []string
	for _, a := range o.NoLogin {
		got = append(got, fmt.Sprintf("%s/%s/%d/%s", a.Slug, a.EnvLabel, a.Views, a.DetailURL))
	}

	want := []string{
		"fuegekompass/Prod/0//usage/app:fuegekompass?env=prod",
		"fuegekompass/Staging/0//usage/app:fuegekompass?env=staging",
		"lpnt/Prod/3//usage/app:lpnt?env=prod",
		"lpnt/Staging/8//usage/app:lpnt?env=staging",
	}
	if !slices.Equal(got, want) {
		t.Errorf("NoLogin = %q, want %q", got, want)
	}
}

func TestUsageDetailUsesEnvClient(t *testing.T) {
	prod, _ := fakeAuth(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("env=staging must not call prod")
		w.WriteHeader(http.StatusInternalServerError)
	})

	var gotPath string

	staging, _ := fakeAuth(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"issuer":"ewws-auth","client_id":"x","accounts":[
			{"account_id":"u1","name":"Erika","logins":1}]}`))
	})

	h := UsageClient{Envs: twoEnvs(prod, staging), Logger: discardLogger, FetchApps: noApps}
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

	d := h.detail(context.Background(), "x", "staging", "", "", "", now)
	if d.State != views.UsageOK || len(d.Accounts) != 1 || gotPath != "/api/activity/clients/x" {
		t.Fatalf("detail = %+v, path %q", d, gotPath)
	}

	if d.Env != "staging" || d.EnvLabel != "Staging" || d.BackURL != "/usage?env=staging" {
		t.Errorf("env fields = %q %q %q", d.Env, d.EnvLabel, d.BackURL)
	}

	rec := serveAs(t, h, adminUser, "/usage/x?env=staging")
	if body := rec.Body.String(); rec.Code != http.StatusOK ||
		!strings.Contains(body, `name="env" value="staging"`) {
		t.Errorf("status %d; range form must carry env", rec.Code)
	}
}

func TestUsagePersonsAcrossEnvs(t *testing.T) {
	var gotQueries atomic.Value

	prod, _ := fakeAuth(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activity/clients":
			_, _ = w.Write([]byte(prodClients))
		case "/api/activity/clients/bridge-prod":
			gotQueries.Store(r.URL.RawQuery)

			_, _ = w.Write([]byte(`{"accounts":[
				{"account_id":"p-1","name":"Erika","personio_id":42,"logins":2,"refreshes":3,"uses":5,"active_days":4},
				{"account_id":"p-2","name":"Max","logins":1,"active_days":1}]}`))
		case "/api/activity/clients/tool-prod":
			_, _ = w.Write([]byte(`{"accounts":[
				{"account_id":"p-1","name":"Erika","personio_id":42,"logins":1,"active_days":6}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	staging, _ := fakeAuth(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/activity/clients":
			_, _ = w.Write([]byte(stagingClients))
		case "/api/activity/clients/bridge-staging":
			// Other account ids in another ewws-auth: Erika is matched by
			// Personio ID, Max by name.
			_, _ = w.Write([]byte(`{"accounts":[
				{"account_id":"s-9","name":"Erika M.","personio_id":42,"logins":1,"active_days":1},
				{"account_id":"s-8","name":"max","logins":10,"uses":20,"active_days":3},
				{"account_id":"s-7","name":"","logins":2}]}`))
		case "/api/activity/clients/-":
			w.WriteHeader(http.StatusInternalServerError)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})

	o := Usage{Envs: twoEnvs(prod, staging), Logger: discardLogger, FetchApps: noApps}.
		overview(context.Background(), "all")

	if q, _ := gotQueries.Load().(string); q != "from=2026-10-04&to=2026-11-02" {
		t.Errorf("drill-down query = %q (30 days ending on ewws-auth's today, both issuers)", q)
	}

	if !slices.Equal(o.PersonEnvs, []string{"Prod", "Staging"}) {
		t.Fatalf("PersonEnvs = %q", o.PersonEnvs)
	}

	type row struct {
		name, dominant        string
		prod, staging, total  int
		prodDays, stagingDays int
	}

	var got []row
	for _, p := range o.Persons {
		got = append(got, row{
			name: orStr(p.Name, p.AccountID), dominant: p.Dominant,
			prod: p.PerEnv[0].Total, staging: p.PerEnv[1].Total, total: p.Total,
			prodDays: p.PerEnv[0].ActiveDays, stagingDays: p.PerEnv[1].ActiveDays,
		})
	}

	want := []row{
		{
			name:        "Max",
			dominant:    "Staging",
			prod:        1,
			staging:     30,
			total:       31,
			prodDays:    1,
			stagingDays: 3,
		},
		{
			name:        "Erika",
			dominant:    "Prod",
			prod:        11,
			staging:     1,
			total:       12,
			prodDays:    6,
			stagingDays: 1,
		},
		{name: "s-7", dominant: "Staging", staging: 2, total: 2},
	}
	if !slices.Equal(got, want) {
		t.Errorf("persons = %+v\nwant      %+v", got, want)
	}

	if !strings.Contains(o.PersonsHint, "1 von 4") {
		t.Errorf("PersonsHint = %q, want the failed call reported", o.PersonsHint)
	}

	// Only env=all compares persons.
	if o := (Usage{Envs: twoEnvs(prod, staging), Logger: discardLogger, FetchApps: noApps}).
		overview(context.Background(), "prod"); o.Persons != nil {
		t.Errorf("single env computed persons: %+v", o.Persons)
	}
}

func orStr(a, b string) string {
	if a != "" {
		return a
	}

	return b
}

func TestUsagePersonsBoundedConcurrency(t *testing.T) {
	const nClients = 20

	var (
		inFlight, peak atomic.Int32
		clients        []string
	)

	for i := range nClients {
		clients = append(clients, fmt.Sprintf(
			`{"issuer":"ewws-auth","client_id":"c%d","last_30d":{"users":1,"logins":1}}`, i))
	}

	ac, apiCalls := fakeAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/activity/clients" {
			_, _ = w.Write([]byte(`{"today":"2026-11-02","clients":[` +
				strings.Join(clients, ",") + `]}`))

			return
		}

		n := inFlight.Add(1)
		defer inFlight.Add(-1)

		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}

		time.Sleep(20 * time.Millisecond)

		_, _ = w.Write([]byte(`{"accounts":[{"account_id":"a","name":"A","logins":1}]}`))
	})

	o := Usage{Envs: twoEnvs(ac, ac), Logger: discardLogger, FetchApps: noApps}.
		overview(context.Background(), "all")

	if p := peak.Load(); p > personsMaxInFlight {
		t.Errorf("peak in-flight drill-downs = %d, want ≤ %d", p, personsMaxInFlight)
	}

	// 2 overviews + 20 drill-downs per env.
	if n := apiCalls.Load(); n != 2+2*nClients {
		t.Errorf("api calls = %d, want %d", n, 2+2*nClients)
	}

	if len(o.Persons) != 1 || o.Persons[0].Total != 2*nClients || o.Persons[0].Dominant != "" {
		t.Errorf("persons = %+v", o.Persons)
	}
}

// Drill-downs that hang must not hold the page past the shared budget:
// queued calls do not start, running ones are cancelled, and the
// overview renders with the persons hint. Without the budget every call
// would wait for its own personsCallTimeout, 8 s × 40 calls / 6 slots.
func TestUsagePersonsSharedBudget(t *testing.T) {
	const nClients = 20

	old := personsBudget
	personsBudget = 200 * time.Millisecond

	t.Cleanup(func() { personsBudget = old })

	var clients []string
	for i := range nClients {
		clients = append(clients, fmt.Sprintf(
			`{"issuer":"ewws-auth","client_id":"c%d","last_30d":{"users":1,"logins":1}}`, i))
	}

	ac, _ := fakeAuth(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/activity/clients" {
			_, _ = w.Write([]byte(`{"today":"2026-11-02","clients":[` +
				strings.Join(clients, ",") + `]}`))

			return
		}

		<-r.Context().Done() // hang until the caller gives up
	})

	h := Usage{Envs: twoEnvs(ac, ac), Logger: discardLogger, FetchApps: noApps}

	start := time.Now()
	o := h.overview(context.Background(), "all")

	if elapsed := time.Since(start); elapsed > personsBudget+2*time.Second {
		t.Fatalf("overview took %v, want about the %v budget", elapsed, personsBudget)
	}

	if o.State != views.UsageOK || len(o.Rows) != 2*nClients {
		t.Fatalf("overview not rendered: state %v, %d rows", o.State, len(o.Rows))
	}

	if !strings.Contains(o.PersonsHint, "von 40") || !strings.Contains(o.PersonsHint, "Zeitlimit") {
		t.Errorf("PersonsHint = %q", o.PersonsHint)
	}

	rec := serveAs(t, h, adminUser, "/usage?env=all")
	if body := rec.Body.String(); rec.Code != http.StatusOK ||
		!strings.Contains(body, "c19") || !strings.Contains(body, "Zeitlimit") {
		t.Errorf("status %d; page lacks the overview rows or the hint", rec.Code)
	}
}
