// Package registry models the `apps/*.yaml` schema of the
// `ewws-apps-registry` repository.
//
// See schemas/app.schema.json in that repo for the source of truth.
package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v3"
)

// base64Std is the standard base64 encoder used by GitHub's Contents API.
var base64Std = base64.StdEncoding

// apiToken holds the server-side GitHub token used by getRaw to call the
// REST API. Set once at startup via SetAPIToken — without it, private
// registries return 404 on every fetch. atomic.Value lets handler
// goroutines read without a mutex.
var apiToken atomic.Pointer[string]

// SetAPIToken wires the server's GitHub token for subsequent registry
// fetches. Empty string is a no-op (and leaves any previous token
// in place — callers should call this at most once during boot).
func SetAPIToken(token string) {
	if token == "" {
		return
	}
	t := token
	apiToken.Store(&t)
}

// App is a parsed `apps/<slug>.yaml` document.
type App struct {
	APIVersion string `yaml:"apiVersion" json:"apiVersion"`
	Kind       string `yaml:"kind"       json:"kind"`
	Slug       string `yaml:"slug"       json:"slug"`

	Owners        Owners              `yaml:"owners"                   json:"owners"`
	Repo          Repo                `yaml:"repo"                     json:"repo"`
	Stages        Stages              `yaml:"stages"                   json:"stages"`
	Resources     Resources           `yaml:"resources"                json:"resources"`
	Features      Features            `yaml:"features"                 json:"features"`
	CreatedAt     string              `yaml:"created_at"               json:"created_at"`
	StatusHistory []StatusHistoryItem `yaml:"status_history,omitempty" json:"status_history,omitempty"`
}

// Owners — both required, GitHub logins.
type Owners struct {
	Technical      string `yaml:"technical"      json:"technical"`
	Organizational string `yaml:"organizational" json:"organizational"`
}

// Repo describes the source repository that builds this app.
type Repo struct {
	URL                string `yaml:"url"                           json:"url"`
	DefaultBranch      string `yaml:"default_branch,omitempty"      json:"default_branch,omitempty"`
	HasBackend         bool   `yaml:"has_backend"                   json:"has_backend"`
	HasFrontend        bool   `yaml:"has_frontend"                  json:"has_frontend"`
	DockerfileBackend  string `yaml:"dockerfile_backend,omitempty"  json:"dockerfile_backend,omitempty"`
	DockerfileFrontend string `yaml:"dockerfile_frontend,omitempty" json:"dockerfile_frontend,omitempty"`
}

// Stages holds the two stage definitions (staging, prod).
type Stages struct {
	Staging Stage `yaml:"staging" json:"staging"`
	Prod    Stage `yaml:"prod"    json:"prod"`
}

// Stage is one deployment environment.
type Stage struct {
	Host                   string `yaml:"host"                               json:"host"`
	AutoDeployBranch       string `yaml:"auto_deploy_branch"                 json:"auto_deploy_branch"`
	RequireManualPromotion bool   `yaml:"require_manual_promotion,omitempty" json:"require_manual_promotion,omitempty"`
}

// Resources blocks for backend + frontend pods.
type Resources struct {
	Backend  ResourceBlock `yaml:"backend"  json:"backend"`
	Frontend ResourceBlock `yaml:"frontend" json:"frontend"`
}

// ResourceBlock is a single workload's K8s resource block.
type ResourceBlock struct {
	MemoryLimit string `yaml:"memory_limit"       json:"memory_limit"`
	CPULimit    string `yaml:"cpu_limit"          json:"cpu_limit"`
	Replicas    int    `yaml:"replicas,omitempty" json:"replicas,omitempty"`
}

// Features is the optional opt-in block (websocket, jobs, auth, ...).
type Features struct {
	Websocket     bool           `yaml:"websocket,omitempty"      json:"websocket,omitempty"`
	ScheduledJobs []ScheduledJob `yaml:"scheduled_jobs,omitempty" json:"scheduled_jobs,omitempty"`
	IngressAuth   string         `yaml:"ingress_auth,omitempty"   json:"ingress_auth,omitempty"`
	CustomDomains []string       `yaml:"custom_domains,omitempty" json:"custom_domains,omitempty"`
}

// ScheduledJob is one cron-style job.
type ScheduledJob struct {
	Name     string `yaml:"name"              json:"name"`
	Schedule string `yaml:"schedule"          json:"schedule"`
	Command  string `yaml:"command,omitempty" json:"command,omitempty"`
}

// StatusHistoryItem is one entry in the audit trail.
type StatusHistoryItem struct {
	At     string `yaml:"at"           json:"at"`
	Action string `yaml:"action"       json:"action"`
	By     string `yaml:"by"           json:"by"`
	PR     int    `yaml:"pr,omitempty" json:"pr,omitempty"`
}

// SlugRegex matches valid claim slugs (see schema).
var SlugRegex = regexp.MustCompile(`^[a-z][a-z0-9-]{2,30}[a-z0-9]$`)

// ParseYAML decodes a single `apps/<slug>.yaml` document.
func ParseYAML(data []byte) (*App, error) {
	var app App

	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	dec.KnownFields(false)

	if err := dec.Decode(&app); err != nil {
		return nil, fmt.Errorf("registry: parse yaml: %w", err)
	}

	return &app, nil
}

// ReservedSlugs is the parsed `reserved-slugs.yaml` from the registry repo.
type ReservedSlugs struct {
	PlatformIntern      []string `yaml:"platform_intern"`
	NamespaceProtection []string `yaml:"namespace_protection"`
	PatternBlocks       []string `yaml:"pattern_blocks"`

	patterns []*regexp.Regexp
}

// IsReserved reports whether `slug` collides with a reserved entry —
// either by literal match against `platform_intern` / `namespace_protection`
// or by regex match against `pattern_blocks`.
func (r *ReservedSlugs) IsReserved(slug string) bool {
	if r == nil {
		return false
	}

	for _, s := range r.PlatformIntern {
		if s == slug {
			return true
		}
	}

	for _, s := range r.NamespaceProtection {
		if s == slug {
			return true
		}
	}

	for _, re := range r.patterns {
		if re.MatchString(slug) {
			return true
		}
	}

	return false
}

// LoadReservedSlugs fetches and parses reserved-slugs.yaml via the GitHub
// REST API (private-repo friendly when SetAPIToken has been called).
//
// Returns an empty (non-nil) struct on transient errors so the server
// boots even if GitHub is briefly unreachable.
func LoadReservedSlugs(ctx context.Context, repo string) (*ReservedSlugs, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/reserved-slugs.yaml?ref=main", repo)

	body, err := getRaw(ctx, url)
	if err != nil {
		return &ReservedSlugs{}, fmt.Errorf("fetch reserved-slugs: %w", err)
	}

	// Contents API returns a JSON envelope with `content` base64-encoded.
	var env struct {
		Encoding string `json:"encoding"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("decode reserved-slugs envelope: %w", err)
	}

	if env.Encoding != "base64" {
		return nil, fmt.Errorf("reserved-slugs unexpected encoding %q", env.Encoding)
	}

	decoded, err := base64Decode(env.Content)
	if err != nil {
		return nil, fmt.Errorf("decode reserved-slugs body: %w", err)
	}

	var rs ReservedSlugs
	if err := yaml.Unmarshal(decoded, &rs); err != nil {
		return nil, fmt.Errorf("decode reserved-slugs: %w", err)
	}

	for _, p := range rs.PatternBlocks {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("compile pattern %q: %w", p, err)
		}

		rs.patterns = append(rs.patterns, re)
	}

	return &rs, nil
}

// Sort returns apps sorted by slug — handy for deterministic UI.
func Sort(apps []App) []App {
	sort.Slice(apps, func(i, j int) bool { return apps[i].Slug < apps[j].Slug })
	return apps
}

// AppsListResponse is the raw response from the GitHub Contents API for
// `apps/`. Private repos return `download_url: null`, so we fetch the
// file body via the Contents API instead.
type AppsListResponse []struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Type string `json:"type"`
}

// contentsFile is the per-file Contents API response (single-file GET).
type contentsFile struct {
	Encoding string `json:"encoding"`
	Content  string `json:"content"`
}

// FetchAppsFromGitHub returns every `apps/*.yaml` from the registry repo
// (except entries whose filename starts with `_`). Authenticates via
// SetAPIToken; private repos return 404 unauthenticated.
func FetchAppsFromGitHub(ctx context.Context, repo string) ([]App, error) {
	contentsURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/apps?ref=main", repo)

	body, err := getRaw(ctx, contentsURL)
	if err != nil {
		return nil, err
	}

	var listing AppsListResponse
	if err := json.Unmarshal(body, &listing); err != nil {
		return nil, fmt.Errorf("decode listing: %w", err)
	}

	apps := make([]App, 0, len(listing))

	for _, item := range listing {
		if item.Type != "file" {
			continue
		}

		if !strings.HasSuffix(item.Name, ".yaml") {
			continue
		}

		if strings.HasPrefix(item.Name, "_") {
			continue
		}

		fileURL := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s?ref=main", repo, item.Path)

		fileBody, err := getRaw(ctx, fileURL)
		if err != nil {
			continue
		}

		raw, err := decodeContentsEnvelope(fileBody)
		if err != nil {
			continue
		}

		app, err := ParseYAML(raw)
		if err != nil {
			continue
		}

		apps = append(apps, *app)
	}

	return Sort(apps), nil
}

// FetchSingleApp returns one `apps/<slug>.yaml` parsed. Uses the Contents
// API so private registries work as long as SetAPIToken was called.
func FetchSingleApp(ctx context.Context, repo, slug string) (*App, []byte, error) {
	if !SlugRegex.MatchString(slug) {
		return nil, nil, errors.New("invalid slug")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/apps/%s.yaml?ref=main", repo, slug)

	envelope, err := getRaw(ctx, url)
	if err != nil {
		return nil, nil, err
	}

	body, err := decodeContentsEnvelope(envelope)
	if err != nil {
		return nil, nil, err
	}

	app, err := ParseYAML(body)
	if err != nil {
		return nil, body, err
	}

	return app, body, nil
}

// decodeContentsEnvelope unwraps a `{"encoding":"base64","content":"…"}`
// response from the GitHub Contents API into the raw file bytes.
func decodeContentsEnvelope(body []byte) ([]byte, error) {
	var f contentsFile
	if err := json.Unmarshal(body, &f); err != nil {
		return nil, fmt.Errorf("decode contents envelope: %w", err)
	}

	if f.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected contents encoding %q", f.Encoding)
	}

	return base64Decode(f.Content)
}

// base64Decode strips the embedded newlines GitHub injects every 60
// characters before decoding.
func base64Decode(s string) ([]byte, error) {
	clean := strings.ReplaceAll(s, "\n", "")
	return base64Std.DecodeString(clean)
}

func getRaw(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Accept", "application/vnd.github+json")

	if t := apiToken.Load(); t != nil && *t != "" {
		req.Header.Set("Authorization", "Bearer "+*t)
	}

	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}

	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}
