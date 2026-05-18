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
)

// Config is the resolved configuration after LoadFromEnv.
type Config struct {
	Port               int
	BaseURL            string
	GitHubClientID     string
	GitHubClientSecret string
	SessionSecret      []byte
	ArgoCDURL          string
	ArgoCDToken        string
	AppsRegistryRepo   string // "owner/repo"
	AllowedOrg         string
	AdminTeam          string
	LogFormat          string // "json" or "text"
}

// LoadFromEnv reads configuration from the environment.
//
// Required (errors when empty): GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET,
// SESSION_SECRET, ARGOCD_TOKEN. Everything else falls back to a default.
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
	c.ArgoCDToken = os.Getenv("ARGOCD_TOKEN")

	secret := os.Getenv("SESSION_SECRET")
	if secret == "" {
		return nil, errors.New("SESSION_SECRET is required (32-byte base64)")
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
		return errors.New("GITHUB_CLIENT_ID is required")
	}

	if c.GitHubClientSecret == "" {
		return errors.New("GITHUB_CLIENT_SECRET is required")
	}

	if c.ArgoCDToken == "" {
		return errors.New("ARGOCD_TOKEN is required")
	}

	return nil
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
