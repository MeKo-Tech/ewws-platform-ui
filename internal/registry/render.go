package registry

import (
	"bytes"
	"errors"
	"fmt"
	"time"
)

// ClaimInput is the claim form payload from POST /claim.
//
// Validate checks it against everything the registry's validate-apps job
// (schemas/app.schema.json plus scripts/validate-claims.sh) would reject;
// ToApp turns it into the Claim that is rendered as apps/<slug>.yaml, and
// TagFiles into the initial tags/<slug>/<stage>.yaml files.
//
// Settings of a component that is not enabled (HasBackend / HasFrontend)
// are kept so the form can show them again, but are neither validated nor
// rendered.
type ClaimInput struct {
	Slug                string
	OwnerTechnical      string
	OwnerOrganizational string
	RepoURL             string

	HasBackend  bool
	HasFrontend bool

	Backend  ComponentInput
	Frontend ComponentInput

	Staging StageInput
	Prod    StageInput

	AuthScope     string
	Websocket     bool
	IngressAuth   string // "", none, github-org, ewws-auth
	SharedSecrets []string
	Secrets       []SecretInput

	// CreatedAt (YYYY-MM-DD) is optional; empty means today (UTC).
	CreatedAt string
}

// ComponentInput holds the form settings of one component.
type ComponentInput struct {
	ImageRepository string
	ImageTag        string

	MemoryLimit string
	CPULimit    string

	Port           int
	Env            []EnvVar
	EnvFromSecrets []string

	LivenessPath  string
	ReadinessPath string
	StartupPath   string
}

// StageInput holds the form settings of one stage.
type StageInput struct {
	Host        string
	Enabled     bool
	BackendEnv  []EnvVar
	FrontendEnv []EnvVar
}

// EnvVar is one environment variable. Secret and Key set make it a
// reference to one key of a Secret; otherwise Value is used.
type EnvVar struct {
	Name   string
	Value  string
	Secret string
	Key    string
}

// SecretInput is one app-specific secret (ExternalSecret) with its
// ENV_VAR -> vault suffix mapping.
type SecretInput struct {
	Name string
	Data []SecretKey
}

// SecretKey maps one environment variable to a vault secret suffix.
type SecretKey struct {
	EnvVar      string
	VaultSuffix string
}

// Component names as used in the schema.
const (
	componentBackend  = "backend"
	componentFrontend = "frontend"
	stageStaging      = "staging"
	stageProd         = "prod"
)

// ToApp turns a validated ClaimInput into the Claim to render.
func (c *ClaimInput) ToApp() *Claim {
	claim := &Claim{
		APIVersion: "platform.meko.dev/v1",
		Kind:       "App",
		Slug:       c.Slug,
		Owners: ClaimOwners{
			Technical:      c.OwnerTechnical,
			Organizational: c.OwnerOrganizational,
		},
		Repo: ClaimRepo{
			URL:         c.RepoURL,
			HasBackend:  c.HasBackend,
			HasFrontend: c.HasFrontend,
		},
		Stages: ClaimStages{
			Staging: c.stage(&c.Staging, false),
			Prod:    c.stage(&c.Prod, true),
		},
		SharedSecrets: c.SharedSecrets,
		Features: ClaimFeatures{
			Websocket: c.Websocket,
		},
		CreatedAt: quotedString(c.CreatedAt),
	}

	if c.CreatedAt == "" {
		claim.CreatedAt = quotedString(time.Now().UTC().Format(time.DateOnly))
	}

	if c.IngressAuth != "" && c.IngressAuth != "none" {
		claim.Features.IngressAuth = c.IngressAuth
	}

	if c.AuthScope != "" {
		claim.Auth = &ClaimAuth{Scope: c.AuthScope}
	}

	comps := &ClaimComponents{}

	if c.HasBackend {
		claim.Images.Backend = image(&c.Backend)
		claim.Resources.Backend = resource(&c.Backend)
		comps.Backend = component(&c.Backend)
	}

	if c.HasFrontend {
		claim.Images.Frontend = image(&c.Frontend)
		claim.Resources.Frontend = resource(&c.Frontend)
		comps.Frontend = component(&c.Frontend)
	}

	if comps.Backend != nil || comps.Frontend != nil {
		claim.Components = comps
	}

	for _, s := range c.Secrets {
		data := make(map[string]string, len(s.Data))
		for _, d := range s.Data {
			data[d.EnvVar] = d.VaultSuffix
		}

		claim.Secrets = append(claim.Secrets, ClaimSecret{Name: s.Name, Data: data})
	}

	return claim
}

func (c *ClaimInput) stage(in *StageInput, prod bool) ClaimStage {
	out := ClaimStage{Host: in.Host, RequireManualPromotion: prod}

	if !in.Enabled {
		disabled := false
		out.Enabled = &disabled
	}

	comps := &ClaimStageComps{}

	if c.HasBackend && len(in.BackendEnv) > 0 {
		comps.Backend = &ClaimStageComponent{Env: envMap(in.BackendEnv)}
	}

	if c.HasFrontend && len(in.FrontendEnv) > 0 {
		comps.Frontend = &ClaimStageComponent{Env: envMap(in.FrontendEnv)}
	}

	if comps.Backend != nil || comps.Frontend != nil {
		out.Components = comps
	}

	return out
}

func image(in *ComponentInput) *ClaimImage {
	return &ClaimImage{
		Repository: in.ImageRepository,
		Tag: ClaimImageTag{
			Staging: quotedString(in.ImageTag),
			Prod:    quotedString(in.ImageTag),
		},
	}
}

func resource(in *ComponentInput) *ClaimResource {
	return &ClaimResource{MemoryLimit: in.MemoryLimit, CPULimit: quotedString(in.CPULimit)}
}

// component returns components.<c>, or nil when nothing is set.
func component(in *ComponentInput) *ClaimComponent {
	out := &ClaimComponent{Port: in.Port}

	if len(in.Env) > 0 {
		out.Env = envMap(in.Env)
	}

	for _, name := range in.EnvFromSecrets {
		out.EnvFrom = append(out.EnvFrom, ClaimEnvFrom{Secret: name})
	}

	probes := &ClaimProbes{
		Liveness:  probe(in.LivenessPath),
		Readiness: probe(in.ReadinessPath),
		Startup:   probe(in.StartupPath),
	}
	if probes.Liveness != nil || probes.Readiness != nil || probes.Startup != nil {
		out.Probes = probes
	}

	if out.Port == 0 && out.Env == nil && out.EnvFrom == nil && out.Probes == nil {
		return nil
	}

	return out
}

func probe(path string) *ClaimProbe {
	if path == "" {
		return nil
	}

	return &ClaimProbe{Path: path}
}

func envMap(vars []EnvVar) map[string]EnvValue {
	out := make(map[string]EnvValue, len(vars))
	for _, v := range vars {
		out[v.Name] = EnvValue{Value: v.Value, Secret: v.Secret, Key: v.Key}
	}

	return out
}

// tagFileHeader is the comment on top of every seeded tag file.
const tagFileHeader = "# Seeded by ewws-platform-ui with the claim's initial tag. The app CI\n" +
	"# (ewws-workflows write-registry-tag) overwrites it on the next build.\n"

// TagFiles returns the initial tags/<slug>/<stage>.yaml files, keyed by
// their path in the registry: one per enabled stage, listing the initial
// image tag of every enabled component.
func (c *ClaimInput) TagFiles() (map[string][]byte, error) {
	if !c.HasBackend && !c.HasFrontend {
		return nil, errors.New("tag files: no component enabled")
	}

	var buf bytes.Buffer

	buf.WriteString(tagFileHeader)
	buf.WriteString("imageTags:\n")

	if c.HasBackend {
		fmt.Fprintf(&buf, "  backend: %q\n", c.Backend.ImageTag)
	}

	if c.HasFrontend {
		fmt.Fprintf(&buf, "  frontend: %q\n", c.Frontend.ImageTag)
	}

	files := map[string][]byte{}

	for _, s := range []struct {
		name    string
		enabled bool
	}{{stageStaging, c.Staging.Enabled}, {stageProd, c.Prod.Enabled}} {
		if s.enabled {
			files[fmt.Sprintf("tags/%s/%s.yaml", c.Slug, s.name)] = bytes.Clone(buf.Bytes())
		}
	}

	if len(files) == 0 {
		return nil, errors.New("tag files: no stage enabled")
	}

	return files, nil
}

// MemoryOptions / CPUOptions are the dropdown choices the claim form
// offers. Keep them in code so the form and the validator stay in
// lockstep.
var (
	MemoryOptions = []string{"128Mi", "256Mi", "512Mi", "1Gi", "2Gi"}
	CPUOptions    = []string{"100m", "250m", "500m", "1", "2"}
)
