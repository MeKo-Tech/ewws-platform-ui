package ewwsauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	fakeClientID = "entra-app-1234"
	fakeSecret   = "SECRET-zq9x-do-not-leak"
	fakeToken    = "TOKEN-k7m2-do-not-leak"
)

// activityServer fakes ewws-auth: /login/azure plus the activity API.
// api handles everything under /api/ after the Bearer check.
func activityServer(t *testing.T, tokenCalls *atomic.Int32, api http.HandlerFunc) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/azure", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)

		if err := r.ParseForm(); err != nil {
			t.Errorf("parse token form: %v", err)
		}

		if _, _, ok := r.BasicAuth(); ok {
			t.Error("token request uses Basic auth, want credentials in the body")
		}

		for k, want := range map[string]string{
			"grant_type":    "client_credentials",
			"client_id":     fakeClientID,
			"client_secret": fakeSecret,
		} {
			if got := r.PostForm.Get(k); got != want {
				t.Errorf("token form %s = %q, want %q", k, got, want)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"` + fakeToken +
			`","token_type":"Bearer","expires_in":3600,"refresh_token":"r","state":""}`))
	})
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer "+fakeToken {
			t.Errorf("Authorization = %q, want the Bearer machine token", got)
		}

		api(w, r)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestActivityClientsAndClient(t *testing.T) {
	var tokenCalls atomic.Int32

	srv := activityServer(t, &tokenCalls, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.EscapedPath() {
		case "/api/activity/clients":
			_, _ = w.Write([]byte(`{"today":"2026-10-26","clients":[{"issuer":"ewws-auth",
				"client_id":"abc","registered":true,"name":"Bridge",
				"last_1d":{"users":1,"logins":2,"refreshes":3,"uses":4},
				"last_7d":{"users":5,"logins":6,"refreshes":7,"uses":8},
				"last_30d":{"users":9,"logins":10,"refreshes":11,"uses":12}}]}`))
		case "/api/activity/clients/a%2Fb%20c":
			q := r.URL.Query()
			if q.Get("issuer") != "keycloak" || q.Get("from") != "2026-10-01" ||
				q.Get("to") != "2026-10-26" {
				t.Errorf("query = %q", r.URL.RawQuery)
			}

			_, _ = w.Write([]byte(`{"issuer":"keycloak","client_id":"a/b c","registered":false,
				"name":"","from":"2026-10-01","to":"2026-10-26","accounts":[
				{"account_id":"u1","name":"Erika","personio_id":42,"logins":1,"refreshes":2,
				 "uses":3,"active_days":4,"last_seen_at":"2026-10-25T08:30:00Z"},
				{"account_id":"u2","name":"Max","personio_id":null,"logins":0,"refreshes":0,
				 "uses":1,"active_days":1,"last_seen_at":"2026-10-20T00:00:00Z"}]}`))
		case "/api/activity/clients/-":
			if r.URL.RawQuery != "" {
				t.Errorf("empty params must not be sent, got %q", r.URL.RawQuery)
			}

			_, _ = w.Write([]byte(`{"issuer":"","client_id":"","from":"2026-09-27",
				"to":"2026-10-26","accounts":[]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.EscapedPath())
			w.WriteHeader(http.StatusNotFound)
		}
	})

	c := NewActivityClient(srv.URL+"/", fakeClientID, fakeSecret)
	if !c.Enabled() {
		t.Fatal("client with all values must be enabled")
	}

	ctx := context.Background()

	clients, err := c.Clients(ctx)
	if err != nil {
		t.Fatalf("Clients: %v", err)
	}

	if clients.Today != "2026-10-26" || len(clients.Clients) != 1 {
		t.Fatalf("Clients = %+v", clients)
	}

	if got := clients.Clients[0]; got.ClientID != "abc" || !got.Registered ||
		got.Last30d.Logins != 10 || got.Last1d.Uses != 4 || got.Last7d.Users != 5 {
		t.Errorf("row = %+v", got)
	}

	detail, err := c.Client(ctx, "keycloak", "a/b c", "2026-10-01", "2026-10-26")
	if err != nil {
		t.Fatalf("Client: %v", err)
	}

	if len(detail.Accounts) != 2 {
		t.Fatalf("accounts = %+v", detail.Accounts)
	}

	a := detail.Accounts[0]
	if a.PersonioID == nil || *a.PersonioID != 42 || a.ActiveDays != 4 ||
		!a.LastSeenAt.Equal(time.Date(2026, 10, 25, 8, 30, 0, 0, time.UTC)) {
		t.Errorf("account = %+v", a)
	}

	if detail.Accounts[1].PersonioID != nil {
		t.Error("null personio_id must decode to nil")
	}

	if _, err := c.Client(ctx, "", "", "", ""); err != nil {
		t.Fatalf("Client(empty id): %v", err)
	}

	if n := tokenCalls.Load(); n != 1 {
		t.Errorf("token fetched %d times, want 1 (cached across calls)", n)
	}
}

func TestActivityErrors(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		sentinel error
		wantCode string
		wantMsg  string
	}{
		{
			name:     "forbidden",
			status:   http.StatusForbidden,
			body:     `{"errors":[{"message":"unauthorized: missing one of the permissions [\"auth:activity:r\"]","extensions":{"code":"FORBIDDEN"}}]}`,
			sentinel: ErrForbidden,
			wantCode: "FORBIDDEN",
			wantMsg:  `missing one of the permissions ["auth:activity:r"]`,
		},
		{
			name:     "unauthenticated",
			status:   http.StatusUnauthorized,
			body:     `{"errors":[{"message":"bad token ` + fakeToken + `","extensions":{"code":"UNAUTHENTICATED"}}]}`,
			sentinel: ErrUnauthenticated,
			wantCode: "UNAUTHENTICATED",
			wantMsg:  "bad token [redacted]",
		},
		{
			name:     "invalid",
			status:   http.StatusBadRequest,
			body:     `{"errors":[{"message":"from must not be after to","extensions":{"code":"INVALID"}}]}`,
			wantCode: "INVALID",
			wantMsg:  "from must not be after to",
		},
		{
			name:   "internal, non-json body echoing secrets",
			status: http.StatusInternalServerError,
			body:   "panic: " + fakeToken + " " + fakeSecret,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var tokenCalls atomic.Int32

			srv := activityServer(t, &tokenCalls, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})

			_, err := NewActivityClient(
				srv.URL,
				fakeClientID,
				fakeSecret,
			).Clients(context.Background())

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}

			if apiErr.Status != tc.status || apiErr.Code != tc.wantCode {
				t.Errorf("APIError = %+v", apiErr)
			}

			if !strings.Contains(apiErr.Message, tc.wantMsg) {
				t.Errorf("message = %q, want it to contain %q", apiErr.Message, tc.wantMsg)
			}

			if tc.sentinel != nil && !errors.Is(err, tc.sentinel) {
				t.Errorf("errors.Is(%v, %v) = false", err, tc.sentinel)
			}

			if tc.sentinel != ErrForbidden && errors.Is(err, ErrForbidden) {
				t.Error("non-403 must not match ErrForbidden")
			}

			assertNoSecrets(t, err)
		})
	}
}

func TestActivityTokenFailureDoesNotLeak(t *testing.T) {
	var apiCalls atomic.Int32

	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/azure", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"bad secret ` +
			fakeSecret + `, last token ` + fakeToken + `"}`))
	})
	mux.HandleFunc("/api/", func(http.ResponseWriter, *http.Request) { apiCalls.Add(1) })

	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, err := NewActivityClient(srv.URL, fakeClientID, fakeSecret).Clients(context.Background())
	if err == nil {
		t.Fatal("want error when the token exchange fails")
	}

	if !strings.Contains(err.Error(), "status 401") {
		t.Errorf("err = %q, want the token endpoint's status", err)
	}

	if apiCalls.Load() != 0 {
		t.Error("activity API called without a token")
	}

	assertNoSecrets(t, err)
}

func TestActivityNotConfigured(t *testing.T) {
	var nilClient *ActivityClient
	if nilClient.Enabled() {
		t.Error("nil client must be disabled")
	}

	if _, err := nilClient.Clients(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("nil client err = %v, want ErrNotConfigured", err)
	}

	for _, c := range []*ActivityClient{
		NewActivityClient("", fakeClientID, fakeSecret),
		NewActivityClient("https://login.example", "", fakeSecret),
		NewActivityClient("https://login.example", fakeClientID, ""),
	} {
		if c.Enabled() {
			t.Error("client with a missing value must be disabled")
		}

		if _, err := c.Client(
			context.Background(),
			"",
			"x",
			"",
			"",
		); !errors.Is(
			err,
			ErrNotConfigured,
		) {
			t.Errorf("err = %v, want ErrNotConfigured", err)
		}
	}
}

func assertNoSecrets(t *testing.T, err error) {
	t.Helper()

	for _, s := range []string{fakeToken, fakeSecret} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("error string leaks %q: %q", s, err)
		}
	}
}
