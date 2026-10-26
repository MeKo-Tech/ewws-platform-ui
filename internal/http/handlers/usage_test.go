package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/ewwsauth"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/http/middleware"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/views"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

// serveAs runs req through the real Session middleware. A non-nil user
// is stored in a cookie minted by the same store, exactly as the OAuth
// callback does — no test-only hook in production code.
func serveAs(
	t *testing.T,
	h http.Handler,
	user *middleware.SessionUser,
	target string,
) *httptest.ResponseRecorder {
	t.Helper()

	store := middleware.NewSessionStore([]byte(strings.Repeat("k", 32)))
	req := httptest.NewRequest(http.MethodGet, target, nil)

	if user != nil {
		mint := httptest.NewRequest(http.MethodGet, "/", nil)
		mintRec := httptest.NewRecorder()
		sess := store.Get(mint)
		middleware.StoreUser(sess, user)

		if err := store.Save(mint, mintRec, sess); err != nil {
			t.Fatalf("save session: %v", err)
		}

		for _, c := range mintRec.Result().Cookies() {
			req.AddCookie(c)
		}
	}

	mux := http.NewServeMux()
	mux.Handle("GET /usage", h)
	mux.Handle("GET /usage/{client_id}", h)

	rec := httptest.NewRecorder()
	middleware.Session(store, discardLogger)(mux).ServeHTTP(rec, req)

	return rec
}

var adminUser = &middleware.SessionUser{Login: "admin", IsAdmin: true}

// fakeAuth is an ewws-auth stand-in; api answers /api/ requests and
// apiCalls counts them.
func fakeAuth(t *testing.T, api http.HandlerFunc) (*ewwsauth.ActivityClient, *atomic.Int32) {
	t.Helper()

	var apiCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/azure", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer","expires_in":3600}`))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		apiCalls.Add(1)
		api(w, r)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return ewwsauth.NewActivityClient(srv.URL, "id", "secret"), &apiCalls
}

func replyStatus(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

func noApps(context.Context) ([]registry.App, error) { return nil, nil }

func TestUsageAuthGuard(t *testing.T) {
	for _, h := range []http.Handler{
		Usage{Logger: discardLogger, FetchApps: noApps},
		UsageClient{Logger: discardLogger, FetchApps: noApps},
	} {
		rec := serveAs(t, h, nil, "/usage/x")
		if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/auth/login" {
			t.Errorf(
				"anonymous: %d %q, want 303 to /auth/login",
				rec.Code,
				rec.Header().Get("Location"),
			)
		}

		rec = serveAs(t, h, &middleware.SessionUser{Login: "dev"}, "/usage/x")
		if rec.Code != http.StatusForbidden {
			t.Errorf("non-admin: %d, want 403", rec.Code)
		}
	}
}

func TestUsageNotConfiguredRenders(t *testing.T) {
	for _, target := range []string{"/usage", "/usage/abc"} {
		var h http.Handler = Usage{Logger: discardLogger, FetchApps: noApps}
		if target != "/usage" {
			h = UsageClient{Logger: discardLogger, FetchApps: noApps}
		}

		rec := serveAs(t, h, adminUser, target)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", target, rec.Code)
		}

		if !strings.Contains(rec.Body.String(), "<html") {
			t.Errorf("%s: page not rendered", target)
		}
	}

	o := Usage{Logger: discardLogger}.overview(context.Background())
	if o.State != views.UsageNotConfigured {
		t.Errorf("overview state = %v, want UsageNotConfigured", o.State)
	}
}

func TestUsageErrorStates(t *testing.T) {
	cases := []struct {
		name    string
		api     http.HandlerFunc
		want    views.UsageState
		wantMsg string
	}{
		{
			name: "forbidden",
			api: replyStatus(
				http.StatusForbidden,
				`{"errors":[{"message":"unauthorized: missing one of the permissions [\"auth:activity:r\"]","extensions":{"code":"FORBIDDEN"}}]}`,
			),
			want:    views.UsageForbidden,
			wantMsg: "auth:activity:r",
		},
		{
			name: "invalid",
			api: replyStatus(http.StatusBadRequest,
				`{"errors":[{"message":"range too long","extensions":{"code":"INVALID"}}]}`),
			want:    views.UsageInvalid,
			wantMsg: "range too long",
		},
		{
			name: "internal",
			api: replyStatus(
				http.StatusInternalServerError,
				`{"errors":[{"message":"pq: relation secret_table does not exist","extensions":{"code":"INTERNAL"}}]}`,
			),
			want:    views.UsageError,
			wantMsg: usageErrorMessage,
		},
	}

	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ac, _ := fakeAuth(t, tc.api)

			o := Usage{
				Activity:  ac,
				Logger:    discardLogger,
				FetchApps: noApps,
			}.overview(
				context.Background(),
			)
			if o.State != tc.want || !strings.Contains(o.Message, tc.wantMsg) {
				t.Errorf(
					"overview = %v %q, want %v containing %q",
					o.State,
					o.Message,
					tc.want,
					tc.wantMsg,
				)
			}

			d := UsageClient{Activity: ac, Logger: discardLogger, FetchApps: noApps}.
				detail(context.Background(), "abc", "", "", "", now)
			if d.State != tc.want || !strings.Contains(d.Message, tc.wantMsg) {
				t.Errorf(
					"detail = %v %q, want %v containing %q",
					d.State,
					d.Message,
					tc.want,
					tc.wantMsg,
				)
			}

			if strings.Contains(o.Message, "secret_table") {
				t.Errorf("upstream error text leaked: %q", o.Message)
			}
		})
	}

	t.Run("unreachable", func(t *testing.T) {
		ac := ewwsauth.NewActivityClient("http://127.0.0.1:1", "id", "secret")

		o := Usage{Activity: ac, Logger: discardLogger}.overview(context.Background())
		if o.State != views.UsageError || o.Message != usageErrorMessage {
			t.Errorf("overview = %v %q, want UsageError", o.State, o.Message)
		}
	})
}

func TestUsageDetailValidatesLocally(t *testing.T) {
	ac, apiCalls := fakeAuth(t, func(w http.ResponseWriter, _ *http.Request) {
		t.Error("invalid input must not reach ewws-auth")
		w.WriteHeader(http.StatusInternalServerError)
	})

	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	h := UsageClient{Activity: ac, Logger: discardLogger, FetchApps: noApps}

	cases := []struct{ name, issuer, from, to string }{
		{name: "bad issuer", issuer: "github"},
		{name: "bad from", from: "26.10.2026"},
		{name: "bad to", to: "2026-13-01"},
		{name: "from after to", from: "2026-10-27", to: "2026-10-26"},
		{name: "367 days", from: "2025-10-25", to: "2026-10-26"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := h.detail(context.Background(), "a/b", tc.issuer, tc.from, tc.to, now)
			if d.State != views.UsageInvalid || d.Message == "" {
				t.Errorf("state = %v %q, want UsageInvalid with a message", d.State, d.Message)
			}

			if d.From != tc.from || d.To != tc.to || d.Issuer != tc.issuer {
				t.Errorf("raw inputs not echoed: %+v", d)
			}

			if d.FormAction != "/usage/a%2Fb" {
				t.Errorf("FormAction = %q", d.FormAction)
			}
		})
	}

	if n := apiCalls.Load(); n != 0 {
		t.Errorf("ewws-auth called %d times", n)
	}

	// 366 days counting both ends is still allowed.
	if _, _, msg := resolveUsageRange("2025-10-26", "2026-10-26", now); msg != "" {
		t.Errorf("366-day range rejected: %s", msg)
	}
}

func TestUsageDetailOK(t *testing.T) {
	var gotPath, gotQuery string

	ac, _ := fakeAuth(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.EscapedPath(), r.URL.RawQuery
		_, _ = w.Write([]byte(`{"issuer":"ewws-auth","client_id":"","registered":false,"name":"",
			"from":"2026-09-27","to":"2026-10-26","accounts":[
			{"account_id":"u1","name":"Erika","personio_id":42,"logins":3,"refreshes":1,"uses":9,
			 "active_days":2,"last_seen_at":"2026-10-25T08:30:00Z"},
			{"account_id":"u2","name":"Max","personio_id":null,"logins":1,"refreshes":0,"uses":1,
			 "active_days":1,"last_seen_at":"2026-10-20T00:00:00Z"}]}`))
	})

	h := UsageClient{
		Activity:  ac,
		Logger:    discardLogger,
		FetchApps: noApps,
		Now:       func() time.Time { return time.Date(2026, 10, 26, 23, 0, 0, 0, time.UTC) },
	}

	rec := serveAs(t, h, adminUser, "/usage/-?issuer=ewws-auth")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}

	if gotPath != "/api/activity/clients/-" {
		t.Errorf("path = %q", gotPath)
	}

	if gotQuery != "from=2026-09-27&issuer=ewws-auth&to=2026-10-26" {
		t.Errorf("query = %q (default range: last 30 days ending today UTC)", gotQuery)
	}

	d := h.detail(context.Background(), "-", "ewws-auth", "", "", h.Now())
	if d.State != views.UsageOK || d.ClientID != "" || d.FormAction != "/usage/-" {
		t.Fatalf("detail = %+v", d)
	}

	if len(d.Accounts) != 2 || d.Accounts[0].PersonioID != "42" || d.Accounts[1].PersonioID != "" {
		t.Errorf("accounts = %+v", d.Accounts)
	}
}

func TestBuildUsageOverview(t *testing.T) {
	window := func(logins int) ewwsauth.ActivityWindow {
		return ewwsauth.ActivityWindow{Users: 1, Logins: logins}
	}

	res := &ewwsauth.ClientsResult{
		Today: "2026-10-26",
		Clients: []ewwsauth.ClientActivity{
			{Issuer: "ewws-auth", ClientID: "bridge-prod", Name: "Bridge", Last30d: window(5)},
			{Issuer: "keycloak", ClientID: "", Last30d: window(50)},
			{Issuer: "ewws-auth", ClientID: "bridge-staging", Name: "Bridge", Last30d: window(5)},
			{Issuer: "ewws-auth", ClientID: "idle", Name: "Idle"}, // empty 30d window
			{Issuer: "ewws-auth", ClientID: "a/b c", Last30d: window(1)},
		},
	}

	apps := []registry.App{
		{Slug: "bridge", Auth: &registry.Auth{ClientIDs: map[string]string{
			"prod": "bridge-prod", "staging": "bridge-staging",
		}}},
		{Slug: "zeta"},
		{Slug: "alpha", Auth: &registry.Auth{Scope: "alpha"}},
		{Slug: "quiet", Auth: &registry.Auth{ClientIDs: map[string]string{
			"staging": "quiet-staging", "prod": "idle",
		}}},
	}

	o := buildUsageOverview(res, apps)

	if o.State != views.UsageOK || o.Today != "2026-10-26" {
		t.Fatalf("overview = %+v", o)
	}

	var order []string
	for _, r := range o.Rows {
		order = append(order, r.ClientID)
	}

	if want := []string{
		"",
		"bridge-prod",
		"bridge-staging",
		"a/b c",
		"idle",
	}; !slices.Equal(
		order,
		want,
	) {
		t.Errorf("row order = %q, want %q", order, want)
	}

	byID := map[string]views.UsageRow{}
	for _, r := range o.Rows {
		byID[r.ClientID] = r
	}

	if r := byID["bridge-prod"]; r.AppSlug != "bridge" ||
		!slices.Equal(r.AppStages, []string{"prod"}) {
		t.Errorf("bridge-prod row = %+v", r)
	}

	if r := byID[""]; r.AppSlug != "" || r.DetailURL != "/usage/-?issuer=keycloak" {
		t.Errorf("empty client row = %+v", r)
	}

	if got := byID["a/b c"].DetailURL; got != "/usage/a%2Fb%20c?issuer=ewws-auth" {
		t.Errorf("DetailURL = %q", got)
	}

	if !slices.Equal(o.NoLogin, []string{"alpha", "zeta"}) {
		t.Errorf("NoLogin = %q", o.NoLogin)
	}

	wantSilent := []views.UsageSilent{
		{AppSlug: "quiet", Stage: "staging", ClientID: "quiet-staging"},
		{AppSlug: "quiet", Stage: "prod", ClientID: "idle"},
	}
	if !slices.Equal(o.Silent, wantSilent) {
		t.Errorf("Silent = %+v, want %+v", o.Silent, wantSilent)
	}
}

func TestUsageRegistryFailureKeepsPage(t *testing.T) {
	ac, _ := fakeAuth(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"today":"2026-10-26","clients":[{"issuer":"ewws-auth",
			"client_id":"x","last_30d":{"users":1,"logins":1}}]}`))
	})

	h := Usage{
		Activity: ac,
		Logger:   discardLogger,
		FetchApps: func(context.Context) ([]registry.App, error) {
			return nil, errors.New("github down")
		},
	}

	o := h.overview(context.Background())
	if o.State != views.UsageOK || len(o.Rows) != 1 || o.Rows[0].AppSlug != "" {
		t.Errorf("overview = %+v", o)
	}

	if rec := serveAs(t, h, adminUser, "/usage"); rec.Code != http.StatusOK {
		t.Errorf("status %d, want 200", rec.Code)
	}
}
