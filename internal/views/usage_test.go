package views

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
)

func renderComponent(t *testing.T, c templ.Component) string {
	t.Helper()

	var buf bytes.Buffer
	if err := c.Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}

	return buf.String()
}

var adminProps = PageProps{Title: "Nutzung", User: &SessionUser{Login: "admin", IsAdmin: true}}

func TestUsageForbiddenRendersNoTable(t *testing.T) {
	html := renderComponent(t, Usage(adminProps, UsageOverview{State: UsageForbidden}))

	if !strings.Contains(html, "Recht auth:activity:r fehlt") {
		t.Error("forbidden notice missing")
	}

	if strings.Contains(html, "<table") {
		t.Error("forbidden state must not render a table")
	}
}

func TestUsageNotConfigured(t *testing.T) {
	html := renderComponent(t, Usage(adminProps, UsageOverview{State: UsageNotConfigured}))

	for _, want := range []string{"nicht konfiguriert", "EWWS_AUTH_URL", "EWWS_AUTH_MACHINE_CLIENT_ID", "EWWS_AUTH_MACHINE_CLIENT_SECRET"} {
		if !strings.Contains(html, want) {
			t.Errorf("not-configured notice lacks %q", want)
		}
	}

	if strings.Contains(html, "<table") {
		t.Error("not-configured state must not render a table")
	}
}

func TestUsageErrorShowsMessage(t *testing.T) {
	html := renderComponent(
		t,
		Usage(adminProps, UsageOverview{State: UsageError, Message: "Zeitüberschreitung"}),
	)

	if !strings.Contains(html, "Zeitüberschreitung") {
		t.Error("error message missing")
	}
}

func TestUsageOKRowsAndSections(t *testing.T) {
	o := UsageOverview{
		State: UsageOK,
		Today: "2026-10-26",
		Rows: []UsageRow{{
			Issuer:     "ewws-auth",
			ClientID:   "abc-123",
			Registered: false,
			Name:       "Demo",
			AppSlug:    "demo",
			AppStages:  []string{"staging", "prod"},
			Last1d:     UsageWindow{Users: 1, Logins: 2},
			Last7d:     UsageWindow{Users: 3, Logins: 4},
			Last30d:    UsageWindow{Users: 5, Logins: 67},
			DetailURL:  "/usage/abc-123?issuer=ewws-auth",
		}},
		Silent:  []UsageSilent{{AppSlug: "quiet", Stage: "prod", ClientID: "q-1"}},
		NoLogin: []UsageNoLoginApp{{Slug: "public-app", DetailURL: "/usage/app:public-app"}},
	}
	html := renderComponent(t, Usage(adminProps, o))

	for _, want := range []string{
		`href="/app/demo"`,
		`href="/usage/abc-123?issuer=ewws-auth"`,
		"staging, prod",
		"nicht registriert",
		">67<",
		"2026-10-26",
		"Ohne Aktivität",
		`href="/app/quiet"`,
		"kein Login",
		`href="/app/public-app"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("overview lacks %q", want)
		}
	}
}

func TestUsageNoLoginVisits(t *testing.T) {
	o := UsageOverview{
		State: UsageOK,
		Rows: []UsageRow{{
			Issuer:    "ewws-auth",
			ClientID:  "app:fuegekompass",
			AppSlug:   "fuegekompass",
			Visits:    true,
			Last30d:   UsageWindow{Users: 3, Logins: 987},
			DetailURL: "/usage/app:fuegekompass?issuer=ewws-auth",
		}},
		NoLogin: []UsageNoLoginApp{
			{
				Slug: "fuegekompass", Accounts: 3, Devices: 5, Views: 42,
				LastSeenAt: time.Date(2026, 10, 30, 9, 15, 0, 0, time.UTC),
				DetailURL:  "/usage/app:fuegekompass",
			},
			{Slug: "lpnt", DetailURL: "/usage/app:lpnt"},
		},
		VisitsOK:   true,
		VisitsDays: 30,
	}
	html := renderComponent(t, Usage(adminProps, o))

	for _, want := range []string{
		"ohne Login",
		"Personen",
		"anonyme Geräte",
		"Aufrufe",
		`href="/usage/app:fuegekompass"`,
		">5<",
		">42<",
		"30.10.2026 10:15",
		`href="/app/lpnt"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("overview lacks %q", want)
		}
	}

	if strings.Contains(html, ">987<") {
		t.Error("a visits row must not show logins")
	}

	if strings.Contains(html, `href="/usage/app:lpnt"`) {
		t.Error("an app without persons must not link the drill-down")
	}
}

func TestUsageNoLoginVisitsHint(t *testing.T) {
	o := UsageOverview{
		State:      UsageOK,
		NoLogin:    []UsageNoLoginApp{{Slug: "lpnt", DetailURL: "/usage/app:lpnt"}},
		VisitsHint: "Aufrufzahlen fehlen.",
	}
	html := renderComponent(t, Usage(adminProps, o))

	for _, want := range []string{"Aufrufzahlen fehlen.", `href="/app/lpnt"`, "kein Login"} {
		if !strings.Contains(html, want) {
			t.Errorf("overview lacks %q", want)
		}
	}

	if strings.Contains(html, "anonyme Geräte") {
		t.Error("visit table must not render without visit numbers")
	}
}

func TestUsageOKEmpty(t *testing.T) {
	html := renderComponent(t, Usage(adminProps, UsageOverview{State: UsageOK}))

	if !strings.Contains(html, "Keine Aktivität in den letzten 30 Tagen.") {
		t.Error("empty state missing")
	}

	if strings.Contains(html, "kein Login") || strings.Contains(html, "Ohne Aktivität") {
		t.Error("empty side sections must not render")
	}
}

func TestUsageRowWithoutClientID(t *testing.T) {
	o := UsageOverview{
		State: UsageOK,
		Rows:  []UsageRow{{Issuer: "keycloak", DetailURL: "/usage/?issuer=keycloak"}},
	}
	html := renderComponent(t, Usage(adminProps, o))

	if !strings.Contains(html, "(ohne client_id)") {
		t.Error("empty client_id label missing")
	}
}

func TestUsageDetailOK(t *testing.T) {
	d := UsageDetail{
		State:      UsageOK,
		Issuer:     "ewws-auth",
		ClientID:   "abc-123",
		Registered: true,
		Name:       "Demo",
		AppSlug:    "demo",
		From:       "2026-09-26",
		To:         "2026-10-26",
		FormAction: "/usage/abc-123",
		Accounts: []UsageAccount{
			{
				AccountID:  "acc-1",
				Name:       "Erika Muster",
				PersonioID: "4711",
				Logins:     3,
				LastSeenAt: time.Date(2026, 10, 26, 8, 5, 0, 0, time.UTC),
			},
			{AccountID: "acc-2"},
		},
	}
	html := renderComponent(t, UsageDetailPage(adminProps, d))

	for _, want := range []string{
		"Erika Muster",
		"26.10.2026 09:05", // CET (UTC+1) after the DST switch on 25.10.
		"acc-2",
		`action="/usage/abc-123"`,
		`name="from" value="2026-09-26"`,
		`name="to" value="2026-10-26"`,
		`name="issuer" value="ewws-auth"`,
		"Anzeigen",
		"höchstens 366 Tage",
		`href="/app/demo"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
}

func TestUsageDetailStates(t *testing.T) {
	forbidden := renderComponent(t, UsageDetailPage(adminProps, UsageDetail{State: UsageForbidden}))
	if !strings.Contains(forbidden, "Recht auth:activity:r fehlt") ||
		strings.Contains(forbidden, "<table") {
		t.Error("detail forbidden: want notice and no table")
	}

	invalid := renderComponent(t, UsageDetailPage(adminProps, UsageDetail{
		State: UsageInvalid, Message: "Zeitraum zu lang", FormAction: "/usage/x",
	}))
	if !strings.Contains(invalid, "Zeitraum zu lang") || !strings.Contains(invalid, `name="from"`) {
		t.Error("detail invalid: want message and the form kept visible")
	}

	empty := renderComponent(t, UsageDetailPage(adminProps, UsageDetail{State: UsageOK}))
	if !strings.Contains(empty, "Keine Logins im Zeitraum.") || !strings.Contains(empty, "alle") {
		t.Error("detail empty: want empty state and issuer \"alle\"")
	}

	if !strings.Contains(empty, "nicht registriert") {
		t.Error("detail ok, unregistered: want \"nicht registriert\"")
	}
}

// The registration status is only known after a successful lookup; an
// error or invalid range must not present the zero value as "nicht
// registriert".
func TestUsageDetailRegisteredOnlyWhenOK(t *testing.T) {
	for _, st := range []UsageState{UsageNotConfigured, UsageForbidden, UsageInvalid, UsageError} {
		html := renderComponent(t, UsageDetailPage(adminProps, UsageDetail{
			State: st, Message: "x", FormAction: "/usage/x",
		}))
		if strings.Contains(html, "registriert") {
			t.Errorf("state %v: registration status rendered without a lookup", st)
		}
	}
}

func TestNavUsageLinkAdminOnly(t *testing.T) {
	cases := map[string]struct {
		user *SessionUser
		want bool
	}{
		"anonymous": {nil, false},
		"member":    {&SessionUser{Login: "m"}, false},
		"admin":     {&SessionUser{Login: "a", IsAdmin: true}, true},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			html := renderComponent(t, header(tc.user))
			if got := strings.Contains(html, `href="/usage"`); got != tc.want {
				t.Errorf(`href="/usage" present = %v, want %v`, got, tc.want)
			}
		})
	}
}

func TestUsageEnvSwitchAndColumns(t *testing.T) {
	single := renderComponent(t, Usage(adminProps, UsageOverview{
		State: UsageOK,
		Rows:  []UsageRow{{ClientID: "x", EnvLabel: "Prod", DetailURL: "/usage/x?env=prod"}},
	}))
	if strings.Contains(single, "Beide") || strings.Contains(single, "Umgebung") {
		t.Error("single environment must render neither switch nor env column")
	}

	html := renderComponent(t, Usage(adminProps, UsageOverview{
		State:   UsageOK,
		Env:     "all",
		ShowEnv: true,
		EnvTabs: []UsageEnvTab{
			{Label: "Prod", URL: "/usage?env=prod"},
			{Label: "Staging", URL: "/usage?env=staging"},
			{Label: "Beide", URL: "/usage?env=all", Active: true},
		},
		EnvNotices: []UsageEnvNotice{{EnvLabel: "Staging", State: UsageError, Message: "weg"}},
		Rows: []UsageRow{
			{ClientID: "b-prod", EnvLabel: "Prod", DetailURL: "/usage/b-prod?env=prod"},
		},
		PersonEnvs: []string{"Prod", "Staging"},
		Persons: []UsagePerson{{
			Name:     "Erika",
			PerEnv:   []UsagePersonEnv{{Total: 11, ActiveDays: 6}, {Total: 1, ActiveDays: 1}},
			Total:    12,
			Dominant: "Prod",
		}},
	}))

	for _, want := range []string{
		`aria-current="page">Beide</a>`,
		`href="/usage?env=staging"`,
		"<th rowspan=\"2\">Umgebung</th>",
		`href="/usage/b-prod?env=prod"`,
		"Personen nach Umgebung",
		"Erika",
		"<strong>Staging:</strong>",
		"weg",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("page lacks %q", want)
		}
	}
}

func TestUsageDetailEnv(t *testing.T) {
	html := renderComponent(t, UsageDetailPage(adminProps, UsageDetail{
		State: UsageOK, ClientID: "x", Env: "staging", EnvLabel: "Staging",
		BackURL: "/usage?env=staging", FormAction: "/usage/x",
	}))

	for _, want := range []string{
		`href="/usage?env=staging"`,
		"<dt>Umgebung</dt>",
		`name="env" value="staging"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
}
