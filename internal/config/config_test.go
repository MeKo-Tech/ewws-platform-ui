package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func setRequiredEnv(t *testing.T) {
	t.Helper()

	t.Setenv("GITHUB_CLIENT_ID", "id")
	t.Setenv("GITHUB_CLIENT_SECRET", "secret")
	t.Setenv("SESSION_SECRET", base64.StdEncoding.EncodeToString(make([]byte, 32)))
}

func TestEwwsAuthConfig(t *testing.T) {
	cases := []struct {
		name        string
		url, id, pw string
		wantEnabled bool
		wantWarning bool
	}{
		{name: "unset", wantEnabled: false, wantWarning: false},
		{
			name:        "complete",
			url:         "https://login.ewws.meko.dev",
			id:          "platform-ui",
			pw:          "pw",
			wantEnabled: true,
		},
		{name: "url only", url: "https://login.ewws.meko.dev", wantWarning: true},
		{name: "credentials only", id: "platform-ui", pw: "pw", wantWarning: true},
		{
			name:        "secret missing",
			url:         "https://login.ewws.meko.dev",
			id:          "platform-ui",
			wantWarning: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("EWWS_AUTH_URL", tc.url)
			t.Setenv("EWWS_AUTH_CLIENT_ID", tc.id)
			t.Setenv("EWWS_AUTH_CLIENT_SECRET", tc.pw)

			cfg, err := LoadFromEnv()
			if err != nil {
				t.Fatalf("LoadFromEnv must not fail on ewws-auth config: %v", err)
			}

			if got := cfg.EwwsAuthEnabled(); got != tc.wantEnabled {
				t.Errorf("EwwsAuthEnabled() = %v, want %v", got, tc.wantEnabled)
			}

			if got := cfg.EwwsAuthWarning != ""; got != tc.wantWarning {
				t.Errorf("warning = %q, want present=%v", cfg.EwwsAuthWarning, tc.wantWarning)
			}

			if tc.pw != "" && strings.Contains(cfg.EwwsAuthWarning, tc.pw) {
				t.Errorf("warning leaks the secret: %q", cfg.EwwsAuthWarning)
			}
		})
	}
}
