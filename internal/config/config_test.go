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

func TestStagingUsageConfig(t *testing.T) {
	const (
		prodURL    = "https://login.meko.work"
		stagingURL = "https://login.ewws.meko.dev"
		secret     = "machine-s3cr3t-value"
	)

	cases := []struct {
		name                   string
		url, machineID, machPW string // prod
		stURL, stID, stPW      string // staging
		wantProd, wantStaging  bool
		wantID, wantPW         string // effective staging credential
		wantProdWarn, wantWarn bool
	}{
		{name: "nothing set"},
		{
			name: "prod only, unchanged", url: prodURL, machineID: "entra", machPW: secret,
			wantProd: true,
		},
		{
			name: "staging falls back to the machine pair", url: prodURL,
			machineID: "entra", machPW: secret, stURL: stagingURL,
			wantProd: true, wantStaging: true, wantID: "entra", wantPW: secret,
		},
		{
			name: "own staging pair wins", url: prodURL, machineID: "entra", machPW: secret,
			stURL: stagingURL, stID: "entra-st", stPW: "st-pw",
			wantProd: true, wantStaging: true, wantID: "entra-st", wantPW: "st-pw",
		},
		{
			name: "half own pair does not fall back", url: prodURL,
			machineID: "entra", machPW: secret, stURL: stagingURL, stID: "entra-st",
			wantProd: true, wantWarn: true,
		},
		{
			name: "staging url without any credential", url: prodURL, stURL: stagingURL,
			wantWarn: true,
		},
		{
			name: "staging pair without url", url: prodURL, machineID: "entra", machPW: secret,
			stID: "entra-st", stPW: "st-pw",
			wantProd: true, wantWarn: true,
		},
		{
			// The fallback uses the raw machine pair: staging works even
			// though prod lacks EWWS_AUTH_URL.
			name: "staging only", machineID: "entra", machPW: secret, stURL: stagingURL,
			wantStaging: true, wantID: "entra", wantPW: secret, wantProdWarn: true,
		},
		{
			name: "half machine pair disables both", url: prodURL, machineID: "entra",
			stURL: stagingURL, wantProdWarn: true, wantWarn: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredEnv(t)
			t.Setenv("EWWS_AUTH_URL", tc.url)
			t.Setenv("EWWS_AUTH_CLIENT_ID", "")
			t.Setenv("EWWS_AUTH_CLIENT_SECRET", "")
			t.Setenv("EWWS_AUTH_MACHINE_CLIENT_ID", tc.machineID)
			t.Setenv("EWWS_AUTH_MACHINE_CLIENT_SECRET", tc.machPW)
			t.Setenv("EWWS_AUTH_STAGING_URL", tc.stURL)
			t.Setenv("EWWS_AUTH_STAGING_MACHINE_CLIENT_ID", tc.stID)
			t.Setenv("EWWS_AUTH_STAGING_MACHINE_CLIENT_SECRET", tc.stPW)

			cfg, err := LoadFromEnv()
			if err != nil {
				t.Fatalf("LoadFromEnv: %v", err)
			}

			if got := cfg.UsageEnabled(); got != tc.wantProd {
				t.Errorf("UsageEnabled() = %v, want %v", got, tc.wantProd)
			}

			if got := cfg.StagingUsageEnabled(); got != tc.wantStaging {
				t.Errorf("StagingUsageEnabled() = %v, want %v", got, tc.wantStaging)
			}

			if cfg.EwwsAuthStagingMachineClientID != tc.wantID ||
				cfg.EwwsAuthStagingMachineClientSecret != tc.wantPW {
				t.Errorf("staging credential = %q / %q, want %q / %q",
					cfg.EwwsAuthStagingMachineClientID, cfg.EwwsAuthStagingMachineClientSecret,
					tc.wantID, tc.wantPW)
			}

			if got := cfg.UsageWarning != ""; got != tc.wantProdWarn {
				t.Errorf("usage warning = %q, want present=%v", cfg.UsageWarning, tc.wantProdWarn)
			}

			if got := cfg.StagingUsageWarning != ""; got != tc.wantWarn {
				t.Errorf("staging warning = %q, want present=%v",
					cfg.StagingUsageWarning, tc.wantWarn)
			}

			for _, s := range []string{secret, "st-pw"} {
				if strings.Contains(cfg.StagingUsageWarning, s) {
					t.Errorf("staging warning leaks a secret: %q", cfg.StagingUsageWarning)
				}
			}

			if cfg.EwwsAuthEnabled() {
				t.Error("scope catalog must stay off without its own pair")
			}
		})
	}
}
