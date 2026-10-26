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
		// The URL alone is shared with the usage view (machine credential),
		// so it no longer counts as a partial scope-catalog config.
		{name: "url only", url: "https://login.ewws.meko.dev", wantWarning: false},
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
			t.Setenv("EWWS_AUTH_MACHINE_CLIENT_ID", "")
			t.Setenv("EWWS_AUTH_MACHINE_CLIENT_SECRET", "")

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

			if cfg.UsageWarning != "" {
				t.Errorf("usage warning without machine vars: %q", cfg.UsageWarning)
			}
		})
	}
}

func TestUsageConfig(t *testing.T) {
	const (
		authURL       = "https://login.ewws.meko.dev"
		machineSecret = "machine-s3cr3t-value"
	)

	cases := []struct {
		name                  string
		url, id, pw           string // scope catalog
		machineID, machinePW  string
		wantUsage, wantScope  bool
		wantUsageWarn, wantSW bool // usage warning, scope-catalog warning
	}{
		{name: "nothing set"},
		{name: "url only", url: authURL},
		{
			name:      "url and machine pair",
			url:       authURL,
			machineID: "entra-app",
			machinePW: machineSecret,
			wantUsage: true,
		},
		{
			name:      "everything",
			url:       authURL,
			id:        "platform-ui",
			pw:        "pw",
			machineID: "entra-app",
			machinePW: machineSecret,
			wantUsage: true,
			wantScope: true,
		},
		{
			name:          "machine id without secret",
			url:           authURL,
			machineID:     "entra-app",
			wantUsageWarn: true,
		},
		{
			name:          "machine secret without id",
			url:           authURL,
			machinePW:     machineSecret,
			wantUsageWarn: true,
		},
		{
			name:          "machine pair without url",
			machineID:     "entra-app",
			machinePW:     machineSecret,
			wantUsageWarn: true,
		},
		{
			name:          "partial machine pair keeps complete catalog",
			url:           authURL,
			id:            "platform-ui",
			pw:            "pw",
			machineID:     "entra-app",
			wantScope:     true,
			wantUsageWarn: true,
		},
		{
			name:      "partial catalog keeps complete machine pair",
			url:       authURL,
			id:        "platform-ui",
			machineID: "entra-app",
			machinePW: machineSecret,
			wantUsage: true,
			wantSW:    true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("EWWS_AUTH_URL", tc.url)
			t.Setenv("EWWS_AUTH_CLIENT_ID", tc.id)
			t.Setenv("EWWS_AUTH_CLIENT_SECRET", tc.pw)
			t.Setenv("EWWS_AUTH_MACHINE_CLIENT_ID", tc.machineID)
			t.Setenv("EWWS_AUTH_MACHINE_CLIENT_SECRET", tc.machinePW)

			cfg, err := LoadFromEnv()
			if err != nil {
				t.Fatalf("LoadFromEnv must not fail on ewws-auth config: %v", err)
			}

			if got := cfg.UsageEnabled(); got != tc.wantUsage {
				t.Errorf("UsageEnabled() = %v, want %v", got, tc.wantUsage)
			}

			if got := cfg.EwwsAuthEnabled(); got != tc.wantScope {
				t.Errorf("EwwsAuthEnabled() = %v, want %v", got, tc.wantScope)
			}

			if got := cfg.UsageWarning != ""; got != tc.wantUsageWarn {
				t.Errorf("usage warning = %q, want present=%v", cfg.UsageWarning, tc.wantUsageWarn)
			}

			if got := cfg.EwwsAuthWarning != ""; got != tc.wantSW {
				t.Errorf("catalog warning = %q, want present=%v", cfg.EwwsAuthWarning, tc.wantSW)
			}

			if cfg.EwwsAuthURL != tc.url {
				t.Errorf("EwwsAuthURL = %q, want %q (never cleared)", cfg.EwwsAuthURL, tc.url)
			}

			if strings.Contains(cfg.UsageWarning, machineSecret) {
				t.Errorf("usage warning leaks the secret: %q", cfg.UsageWarning)
			}

			if !tc.wantUsage &&
				(cfg.EwwsAuthMachineClientID != "" || cfg.EwwsAuthMachineClientSecret != "") {
				t.Error("disabled usage view must not keep machine credentials")
			}
		})
	}
}
