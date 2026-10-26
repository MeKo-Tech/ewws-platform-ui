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
		NoLogin: []string{"public-app"},
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
