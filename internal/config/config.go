// Package config holds the runtime configuration of the platform UI.
//
// Values are loaded from environment variables (12-factor). Defaults are
// applied for everything that can be sensibly defaulted; the few required
// secrets surface as errors when missing so the binary fails fast on boot.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the resolved configuration after LoadFromEnv.
type Config struct {
	Port               int
	BaseURL            string
	GitHubClientID     string
	GitHubClientSecret string
	GitHubAPIToken     string // server-side token for the private registry (PAT or App-installation)
	SessionSecret      []byte
	ArgoCDURL          string
	ArgoCDToken        string
	AppsRegistryRepo   string // "owner/repo"
	AllowedOrg         string
	AdminTeam          string
	LogFormat          string // "json" or "text"

	// DBPath is the on-disk location of the SQLite file backing the
	// compliance scanner. The chart mounts a PVC at /var/data so the
	// file survives pod restarts. Empty disables persistence (in-memory
	// only — fine for unit tests, not for production).
	DBPath string

	// ComplianceScanInterval — how often the periodic scanner walks the
	// registry. 0 disables the scanner entirely.
	ComplianceScanInterval time.Duration

	// PrometheusURL is the in-cluster base URL of the kube-prometheus-stack
	// query API. Empty disables the metrics scanner — the UI degrades to
	// showing zeros for traffic/resource panes, but stays functional.
	PrometheusURL string

	// MetricsScanInterval — how often the metrics scanner refreshes the
	// per-(slug,stage) Prometheus snapshot. Ignored when PrometheusURL is
	// empty.
	MetricsScanInterval time.Duration

	// DriftScanInterval — how often the drift scanner compares each
	// registry app's staging vs prod image tags against the GitHub
	// compare API. Ignored when no GitHub API token is set.
	DriftScanInterval time.Duration
}

// LoadFromEnv reads configuration from the environment.
//
// Required (errors when empty): GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET,
// SESSION_SECRET. ARGOCD_TOKEN and GITHUB_API_TOKEN are optional —
// without ARGOCD_TOKEN the live-status pane is hidden; without
// GITHUB_API_TOKEN the registry has to be public (private-repo requests
// return 404 unauthenticated).
func LoadFromEnv() (*Config, error) {
	c := &Config{
		Port:             defaultPort(),
		BaseURL:          getenvDefault("BASE_URL", "https://platform.apps.meko.work"),
		ArgoCDURL:        getenvDefault("ARGOCD_URL", "http://argo-cd-argocd-server.argocd.svc"),
		AppsRegistryRepo: getenvDefault("APPS_REGISTRY_REPO", "MeKo-Tech/ewws-apps-registry"),
		AllowedOrg:       getenvDefault("ALLOWED_ORG", "MeKo-Tech"),
		AdminTeam:        getenvDefault("ADMIN_TEAM", "ewws"),
		LogFormat:        getenvDefault("LOG_FORMAT", "json"),
	}

	c.GitHubClientID = os.Getenv("GITHUB_CLIENT_ID")
	c.GitHubClientSecret = os.Getenv("GITHUB_CLIENT_SECRET")
	c.GitHubAPIToken = os.Getenv("GITHUB_API_TOKEN")
	c.ArgoCDToken = os.Getenv("ARGOCD_TOKEN")

	c.DBPath = getenvDefault("DB_PATH", "/var/data/platform-ui.db")

	scanIntervalStr := getenvDefault("COMPLIANCE_SCAN_INTERVAL", "1h")

	scanInterval, err := time.ParseDuration(scanIntervalStr)
	if err != nil {
		return nil, fmt.Errorf("COMPLIANCE_SCAN_INTERVAL: %w", err)
	}

	c.ComplianceScanInterval = scanInterval

	c.PrometheusURL = os.Getenv("PROMETHEUS_URL")

	metricsIntervalStr := getenvDefault("METRICS_SCAN_INTERVAL", "5m")

	metricsInterval, err := time.ParseDuration(metricsIntervalStr)
	if err != nil {
		return nil, fmt.Errorf("METRICS_SCAN_INTERVAL: %w", err)
	}

	c.MetricsScanInterval = metricsInterval

	driftIntervalStr := getenvDefault("DRIFT_SCAN_INTERVAL", "30m")

	driftInterval, err := time.ParseDuration(driftIntervalStr)
	if err != nil {
		return nil, fmt.Errorf("DRIFT_SCAN_INTERVAL: %w", err)
	}

	c.DriftScanInterval = driftInterval

	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		return nil, errors.New("session secret is required (32-byte base64)")
	}

	raw, err := base64.StdEncoding.DecodeString(secret)
	if err != nil {
		return nil, fmt.Errorf("SESSION_SECRET must be base64: %w", err)
	}

	if len(raw) < 32 {
		return nil, fmt.Errorf("SESSION_SECRET must decode to >=32 bytes, got %d", len(raw))
	}

	c.SessionSecret = raw

	if err := c.validate(); err != nil {
		return nil, err
	}

	return c, nil
}

// OAuthReady reports whether the GitHub OAuth client is wired up; the
// /readyz endpoint uses this to gate readiness.
func (c *Config) OAuthReady() bool {
	return c.GitHubClientID != "" && c.GitHubClientSecret != ""
}

// OAuthRedirectURL is the absolute URL GitHub will call back to after
// the user grants consent.
func (c *Config) OAuthRedirectURL() string {
	return c.BaseURL + "/auth/callback"
}

func (c *Config) validate() error {
	if c.GitHubClientID == "" {
		return errors.New("github client id is required")
	}

	if c.GitHubClientSecret == "" {
		return errors.New("github client secret is required")
	}

	return nil
}

// ArgoCDEnabled reports whether the live-status integration is wired up.
func (c *Config) ArgoCDEnabled() bool {
	return c.ArgoCDToken != ""
}

// GitHubAPIReady reports whether the server has a token for the GitHub
// REST API. Without it, private-registry reads return 404.
func (c *Config) GitHubAPIReady() bool {
	return c.GitHubAPIToken != ""
}

// PrometheusEnabled reports whether the metrics integration is wired up.
// When false, the metrics scanner is a no-op and the UI shows zeros for
// traffic/resource panes.
func (c *Config) PrometheusEnabled() bool {
	return c.PrometheusURL != ""
}

func defaultPort() int {
	raw := os.Getenv("PORT")
	if raw == "" {
		return 8080
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 8080
	}

	return n
}

func getenvDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
