package registry

import (
	"bytes"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Claim is the render-only model of a new `apps/<slug>.yaml` claim.
//
// It is separate from App (the parse model for existing claims) so that the
// rendered YAML carries only what the claim form set, in the order of the
// registry's schemas/app.schema.json, and none of the deprecated or
// defaulted keys (auto_deploy_branch, default_branch, dockerfiles).
type Claim struct {
	APIVersion    string           `yaml:"apiVersion"`
	Kind          string           `yaml:"kind"`
	Slug          string           `yaml:"slug"`
	Owners        ClaimOwners      `yaml:"owners"`
	Repo          ClaimRepo        `yaml:"repo"`
	Stages        ClaimStages      `yaml:"stages"`
	Images        ClaimImages      `yaml:"images"`
	Resources     ClaimResources   `yaml:"resources"`
	Components    *ClaimComponents `yaml:"components,omitempty"`
	Auth          *ClaimAuth       `yaml:"auth,omitempty"`
	SharedSecrets []string         `yaml:"shared_secrets,omitempty"`
	Features      ClaimFeatures    `yaml:"features"`
	CreatedAt     quotedString     `yaml:"created_at"`
}

// ClaimOwners is owners.{technical,organizational}.
type ClaimOwners struct {
	Technical      string `yaml:"technical"`
	Organizational string `yaml:"organizational"`
}

// ClaimRepo is the repo block. has_backend / has_frontend are always
// written, false included.
type ClaimRepo struct {
	URL         string `yaml:"url"`
	HasBackend  bool   `yaml:"has_backend"`
	HasFrontend bool   `yaml:"has_frontend"`
}

// ClaimStages is stages.{staging,prod}; both are required by the schema.
type ClaimStages struct {
	Staging ClaimStage `yaml:"staging"`
	Prod    ClaimStage `yaml:"prod"`
}

// ClaimStage is one stage. Enabled is only set (to false) for a disabled
// stage; the schema default is true.
type ClaimStage struct {
	Host                   string           `yaml:"host"`
	Enabled                *bool            `yaml:"enabled,omitempty"`
	RequireManualPromotion bool             `yaml:"require_manual_promotion,omitempty"`
	Components             *ClaimStageComps `yaml:"components,omitempty"`
}

// ClaimStageComps is stages.<s>.components.
type ClaimStageComps struct {
	Backend  *ClaimStageComponent `yaml:"backend,omitempty"`
	Frontend *ClaimStageComponent `yaml:"frontend,omitempty"`
}

// ClaimStageComponent is stages.<s>.components.<c>.
type ClaimStageComponent struct {
	Env map[string]EnvValue `yaml:"env,omitempty"`
}

// ClaimImages is images.<c> for the enabled components only.
type ClaimImages struct {
	Backend  *ClaimImage `yaml:"backend,omitempty"`
	Frontend *ClaimImage `yaml:"frontend,omitempty"`
}

// ClaimImage is images.<c>. It carries the repository only: the image tag
// of every stage lives in tags/<slug>/<stage>.yaml (see TagFiles), and the
// registry rejects a claim with images.<c>.tag.
type ClaimImage struct {
	Repository string `yaml:"repository"`
}

// ClaimResources is resources.<c> for the enabled components only.
type ClaimResources struct {
	Backend  *ClaimResource `yaml:"backend,omitempty"`
	Frontend *ClaimResource `yaml:"frontend,omitempty"`
}

// ClaimResource is resources.<c>.
type ClaimResource struct {
	MemoryLimit string       `yaml:"memory_limit"`
	CPULimit    quotedString `yaml:"cpu_limit"`
}

// ClaimComponents is components.<c>.
type ClaimComponents struct {
	Backend  *ClaimComponent `yaml:"backend,omitempty"`
	Frontend *ClaimComponent `yaml:"frontend,omitempty"`
}

// ClaimComponent is components.<c>, only the keys the form offers.
type ClaimComponent struct {
	Port    int                 `yaml:"port,omitempty"`
	Env     map[string]EnvValue `yaml:"env,omitempty"`
	EnvFrom []ClaimEnvFrom      `yaml:"env_from,omitempty"`
	Probes  *ClaimProbes        `yaml:"probes,omitempty"`
}

// ClaimEnvFrom is one env_from entry; the form only offers secrets.
type ClaimEnvFrom struct {
	Secret string `yaml:"secret"`
}

// ClaimProbes is components.<c>.probes.
type ClaimProbes struct {
	Liveness  *ClaimProbe `yaml:"liveness,omitempty"`
	Readiness *ClaimProbe `yaml:"readiness,omitempty"`
	Startup   *ClaimProbe `yaml:"startup,omitempty"`
}

// ClaimProbe is an http probe override with only a path.
type ClaimProbe struct {
	Path string `yaml:"path"`
}

// ClaimAuth is the optional auth block.
type ClaimAuth struct {
	Scope string `yaml:"scope"`
}

// ClaimFeatures is the required features block; `{}` when nothing is set.
type ClaimFeatures struct {
	Websocket   bool   `yaml:"websocket,omitempty"`
	IngressAuth string `yaml:"ingress_auth,omitempty"`
}

// EnvValue is an env value: a plain string, or a reference to one key of a
// Secret when Secret is set.
type EnvValue struct {
	Value  string
	Secret string
	Key    string
}

// MarshalYAML renders a plain value as a double-quoted string (so "8080"
// and "true" stay strings) and a secret reference as {secret, key}.
func (v EnvValue) MarshalYAML() (any, error) {
	if v.Secret != "" {
		return struct {
			Secret string `yaml:"secret"`
			Key    string `yaml:"key"`
		}{Secret: v.Secret, Key: v.Key}, nil
	}

	return quotedString(v.Value).MarshalYAML()
}

// quotedString always renders as a double-quoted YAML string, so values
// such as dates, versions and numbers can never be read back as another
// type.
type quotedString string

// MarshalYAML implements yaml.Marshaler.
func (q quotedString) MarshalYAML() (any, error) {
	return &yaml.Node{
		Kind:  yaml.ScalarNode,
		Tag:   "!!str",
		Style: yaml.DoubleQuotedStyle,
		Value: string(q),
	}, nil
}

// RenderYAML marshals a Claim to indented YAML.
func (c *Claim) RenderYAML() ([]byte, error) {
	var buf bytes.Buffer

	buf.WriteString("# Generated by ewws-platform-ui\n")
	buf.WriteString("# Schema: ../schemas/app.schema.json\n\n")

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)

	if err := enc.Encode(c); err != nil {
		return nil, fmt.Errorf("encode claim: %w", err)
	}

	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("close encoder: %w", err)
	}

	return buf.Bytes(), nil
}
