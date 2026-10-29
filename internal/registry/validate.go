package registry

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Patterns and limits from the registry's schemas/app.schema.json. The
// schema patterns that use lookahead (owner login, image tag, secret key)
// are checked by hand below, because Go's regexp has no lookahead.
var (
	memoryRegex       = mustCompileSchema(`^[0-9]+(\.[0-9]+)?(Ki|Mi|Gi|Ti|K|M|G|T)?$`)
	cpuRegex          = mustCompileSchema(`^[0-9]+(\.[0-9]+)?m?$`)
	hostLabelRegex    = mustCompileSchema(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	imageTagRegex     = mustCompileSchema(`^[A-Za-z0-9_][A-Za-z0-9_.\-]{0,127}$`)
	probePathRegex    = mustCompileSchema(`^/[A-Za-z0-9_./?=&%~+-]*$`)
	envNameRegex      = mustCompileSchema(`^[A-Za-z_][A-Za-z0-9_]*$`)
	k8sNameRegex      = mustCompileSchema(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	secretKeyRegex    = mustCompileSchema(`^[-._a-zA-Z0-9]+$`)
	authScopeRegex    = mustCompileSchema(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
	sharedSecretRegex = mustCompileSchema(`^[a-z][a-z0-9-]{1,30}[a-z0-9]$`)
)

const (
	maxEnvVars      = 50
	maxEnvNameLen   = 128
	maxEnvValueLen  = 4096
	maxSecretKeyLen = 253
	maxEnvFrom      = 10
	maxProbePathLen = 200
	maxAuthScopeLen = 128
	maxMemoryBytes  = 4 * 1024 * 1024 * 1024 // 4Gi
	maxCPUCores     = 4
	minPort         = 1024
	maxPort         = 65535
)

// Validate enforces everything the registry's validate-apps job would
// reject for a claim the form can produce: the JSON schema (patterns,
// formats, limits) and the extra rules of scripts/validate-claims.sh
// (enabled components and stages, quantity caps, known secrets).
func (c *ClaimInput) Validate(reserved *ReservedSlugs) error {
	if !SlugRegex.MatchString(c.Slug) {
		return fmt.Errorf("slug %q does not match ^[a-z][a-z0-9-]{2,30}[a-z0-9]$", c.Slug)
	}

	if reserved.IsReserved(c.Slug) {
		return fmt.Errorf("slug %q is reserved", c.Slug)
	}

	if err := validateLogin("owners.technical", c.OwnerTechnical); err != nil {
		return err
	}

	if err := validateLogin("owners.organizational", c.OwnerOrganizational); err != nil {
		return err
	}

	if err := validateRepoURL(c.RepoURL); err != nil {
		return err
	}

	if !c.HasBackend && !c.HasFrontend {
		return errors.New("enable at least one component (backend or frontend)")
	}

	if !c.Staging.Enabled && !c.Prod.Enabled {
		return errors.New("enable at least one stage (staging or prod)")
	}

	if err := validateHost("stages.staging.host", c.Staging.Host); err != nil {
		return err
	}

	if err := validateHost("stages.prod.host", c.Prod.Host); err != nil {
		return err
	}

	known, err := c.validateSecrets()
	if err != nil {
		return err
	}

	for _, comp := range c.enabledComponents() {
		if err := comp.in.validate(comp.name, known); err != nil {
			return err
		}
	}

	for _, s := range []struct {
		name string
		in   *StageInput
	}{{stageStaging, &c.Staging}, {stageProd, &c.Prod}} {
		if c.HasBackend {
			field := "stages." + s.name + ".components.backend.env"
			if err := validateEnv(field, s.in.BackendEnv, known); err != nil {
				return err
			}
		}

		if c.HasFrontend {
			field := "stages." + s.name + ".components.frontend.env"
			if err := validateEnv(field, s.in.FrontendEnv, known); err != nil {
				return err
			}
		}
	}

	if c.AuthScope != "" {
		if len(c.AuthScope) > maxAuthScopeLen || !authScopeRegex.MatchString(c.AuthScope) {
			return fmt.Errorf(
				"auth.scope %q must match ^[A-Za-z0-9][A-Za-z0-9_.-]*$ (at most %d characters)",
				c.AuthScope, maxAuthScopeLen,
			)
		}
	}

	switch c.IngressAuth {
	case "", "none", "github-org", "ewws-auth":
	default:
		return fmt.Errorf(
			"features.ingress_auth %q must be one of none, github-org, ewws-auth",
			c.IngressAuth,
		)
	}

	if c.CreatedAt != "" {
		if _, err := time.Parse(time.DateOnly, c.CreatedAt); err != nil {
			return fmt.Errorf("created_at %q must be a date YYYY-MM-DD", c.CreatedAt)
		}
	}

	return nil
}

type namedComponent struct {
	name string
	in   *ComponentInput
}

func (c *ClaimInput) enabledComponents() []namedComponent {
	var out []namedComponent

	if c.HasBackend {
		out = append(out, namedComponent{componentBackend, &c.Backend})
	}

	if c.HasFrontend {
		out = append(out, namedComponent{componentFrontend, &c.Frontend})
	}

	return out
}

// validateSecrets checks shared_secrets and returns the set of secret
// names env references and env_from may use.
func (c *ClaimInput) validateSecrets() (map[string]bool, error) {
	known := map[string]bool{}

	for _, name := range c.SharedSecrets {
		if !sharedSecretRegex.MatchString(name) {
			return nil, fmt.Errorf(
				"shared_secrets: %q must match ^[a-z][a-z0-9-]{1,30}[a-z0-9]$", name,
			)
		}

		if known[name] {
			return nil, fmt.Errorf("shared_secrets: %q is listed twice", name)
		}

		known[name] = true
	}

	return known, nil
}

func (in *ComponentInput) validate(name string, known map[string]bool) error {
	if n := utf8.RuneCountInString(in.ImageRepository); n < 3 || n > 253 {
		return fmt.Errorf("images.%s.repository is required (3 to 253 characters)", name)
	}

	if err := validateImageTag("imageTags."+name, in.ImageTag); err != nil {
		return err
	}

	if err := validateQuantity(
		"resources."+name+".memory_limit", in.MemoryLimit, true, maxMemoryBytes, "4Gi",
	); err != nil {
		return err
	}

	if err := validateQuantity(
		"resources."+name+".cpu_limit", in.CPULimit, false, maxCPUCores, "4",
	); err != nil {
		return err
	}

	if in.Port != 0 && (in.Port < minPort || in.Port > maxPort) {
		return fmt.Errorf(
			"components.%s.port %d must be between %d and %d", name, in.Port, minPort, maxPort,
		)
	}

	for _, p := range []struct{ kind, path string }{
		{"liveness", in.LivenessPath},
		{"readiness", in.ReadinessPath},
		{"startup", in.StartupPath},
	} {
		if p.path == "" {
			continue
		}

		if len(p.path) > maxProbePathLen || !probePathRegex.MatchString(p.path) {
			return fmt.Errorf(
				"components.%s.probes.%s.path %q must start with / and match ^/[A-Za-z0-9_./?=&%%~+-]*$ (at most %d characters)",
				name,
				p.kind,
				p.path,
				maxProbePathLen,
			)
		}
	}

	if err := validateEnv("components."+name+".env", in.Env, known); err != nil {
		return err
	}

	if len(in.EnvFromSecrets) > maxEnvFrom {
		return fmt.Errorf("components.%s.env_from: at most %d entries", name, maxEnvFrom)
	}

	seen := map[string]bool{}

	for _, s := range in.EnvFromSecrets {
		field := "components." + name + ".env_from"
		if !k8sNameRegex.MatchString(s) {
			return fmt.Errorf("%s: %q is not a valid Kubernetes name", field, s)
		}

		if seen[s] {
			return fmt.Errorf("%s: %q is listed twice", field, s)
		}

		seen[s] = true

		if !known[s] {
			return fmt.Errorf("%s: secret %q is not in shared_secrets", field, s)
		}
	}

	return nil
}

func validateEnv(field string, vars []EnvVar, known map[string]bool) error {
	if len(vars) > maxEnvVars {
		return fmt.Errorf("%s: at most %d variables, got %d", field, maxEnvVars, len(vars))
	}

	seen := map[string]bool{}

	for _, v := range vars {
		if err := validateEnvName(field, v.Name); err != nil {
			return err
		}

		if seen[v.Name] {
			return fmt.Errorf("%s: %s is set twice", field, v.Name)
		}

		seen[v.Name] = true

		if v.Secret == "" && v.Key == "" {
			if utf8.RuneCountInString(v.Value) > maxEnvValueLen {
				return fmt.Errorf(
					"%s.%s: value longer than %d characters",
					field,
					v.Name,
					maxEnvValueLen,
				)
			}

			continue
		}

		if !k8sNameRegex.MatchString(v.Secret) {
			return fmt.Errorf(
				"%s.%s: secret %q is not a valid Kubernetes name",
				field,
				v.Name,
				v.Secret,
			)
		}

		if !validSecretKey(v.Key) {
			return fmt.Errorf(
				"%s.%s: secret key %q must match [-._a-zA-Z0-9]+, not be . or start with .. (at most %d characters)",
				field,
				v.Name,
				v.Key,
				maxSecretKeyLen,
			)
		}

		if !known[v.Secret] {
			return fmt.Errorf(
				"%s.%s: secret %q is not in shared_secrets",
				field,
				v.Name,
				v.Secret,
			)
		}
	}

	return nil
}

func validateEnvName(field, name string) error {
	if len(name) > maxEnvNameLen || !envNameRegex.MatchString(name) {
		return fmt.Errorf(
			"%s: %q is not a valid variable name (^[A-Za-z_][A-Za-z0-9_]*$, at most %d characters)",
			field, name, maxEnvNameLen,
		)
	}

	return nil
}

// validSecretKey is the schema pattern ^(?!\.$)(?!\.\.)[-._a-zA-Z0-9]+$.
func validSecretKey(key string) bool {
	return len(key) <= maxSecretKeyLen &&
		secretKeyRegex.MatchString(key) &&
		key != "." &&
		!strings.HasPrefix(key, "..")
}

// validateImageTag is the tag schema pattern (tags/<slug>/<stage>.yaml)
// ^(?!latest$)[A-Za-z0-9_][A-Za-z0-9_.\-]{0,127}$.
func validateImageTag(field, tag string) error {
	if tag == "" {
		return fmt.Errorf("%s is required", field)
	}

	if tag == "latest" {
		return fmt.Errorf("%s may not be latest (Kyverno disallow-latest-tag blocks it)", field)
	}

	if !imageTagRegex.MatchString(tag) {
		return fmt.Errorf(
			"%s %q must match ^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$", field, tag,
		)
	}

	return nil
}

// validateLogin is the schema pattern
// ^[A-Za-z0-9](?:[A-Za-z0-9]|-(?=[A-Za-z0-9])){0,38}$: a GitHub login of
// 1 to 39 letters, digits and single inner hyphens.
func validateLogin(field, login string) error {
	ok := len(login) >= 1 && len(login) <= 39 &&
		login[0] != '-' && login[len(login)-1] != '-' &&
		!strings.Contains(login, "--")

	for _, r := range login {
		if !isASCIIAlnum(r) && r != '-' {
			ok = false
		}
	}

	if !ok {
		return fmt.Errorf("%s %q is not a valid GitHub login", field, login)
	}

	return nil
}

func isASCIIAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

func validateRepoURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || !strings.HasPrefix(raw, "https://") {
		return fmt.Errorf("repo.url must be an https:// URL, got %q", raw)
	}

	// url.Parse tolerates unescaped spaces and other characters that the
	// registry schema's `format: uri` (RFC 3986) rejects.
	for _, r := range raw {
		if !isURIChar(r) {
			return fmt.Errorf("repo.url %q contains %q, which must be percent-encoded", raw, r)
		}
	}

	return nil
}

// isURIChar reports whether r may appear literally in an RFC 3986 URI
// (unreserved, reserved or the '%' of a percent-encoding).
func isURIChar(r rune) bool {
	return isASCIIAlnum(r) || strings.ContainsRune("-._~:/?#[]@!$&'()*+,;=%", r)
}

// validateHost checks format hostname with lower-case labels.
func validateHost(field, host string) error {
	if host == "" {
		return fmt.Errorf("%s is required", field)
	}

	ok := len(host) >= 3 && len(host) <= 253

	for _, label := range strings.Split(host, ".") {
		if !hostLabelRegex.MatchString(label) {
			ok = false
		}
	}

	if !ok {
		return fmt.Errorf(
			"%s %q must be a lower-case host name (labels of a-z, 0-9 and inner hyphens, separated by dots)",
			field,
			host,
		)
	}

	return nil
}

// validateQuantity checks a Kubernetes quantity against the schema pattern
// and the validate-claims.sh bounds (> 0 and <= max).
func validateQuantity(field, q string, memory bool, maxValue float64, maxLabel string) error {
	pattern := cpuRegex
	if memory {
		pattern = memoryRegex
	}

	if q == "" {
		return fmt.Errorf("%s is required", field)
	}

	if !pattern.MatchString(q) {
		return fmt.Errorf("%s %q is not a valid quantity", field, q)
	}

	v := quantityValue(q)
	if v <= 0 {
		return fmt.Errorf("%s (%s) must be greater than zero", field, q)
	}

	if v > maxValue*(1+1e-12) {
		return fmt.Errorf("%s (%s) exceeds the maximum of %s", field, q, maxLabel)
	}

	return nil
}

// quantityValue converts a pattern-valid quantity to base units (bytes,
// cores), like qty_le in validate-claims.sh.
func quantityValue(q string) float64 {
	i := strings.IndexFunc(q, func(r rune) bool { return (r < '0' || r > '9') && r != '.' })
	if i < 0 {
		i = len(q)
	}

	n, err := strconv.ParseFloat(q[:i], 64)
	if err != nil {
		return 0
	}

	mult := map[string]float64{
		"":   1,
		"m":  1e-3,
		"K":  1e3,
		"M":  1e6,
		"G":  1e9,
		"T":  1e12,
		"Ki": 1 << 10,
		"Mi": 1 << 20,
		"Gi": 1 << 30,
		"Ti": 1 << 40,
	}[q[i:]]

	return n * mult
}
