// Package ewwsauth is a tiny read-only HTTP client for the ewws-auth
// scope catalog (`GET /scope-catalog/{name}`).
//
// The detail page uses it to show what a claimed scope grants. Results
// are cached in memory for a few minutes so a page load does not hit
// ewws-auth every time.
package ewwsauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// DefaultCacheTTL is how long a scope lookup (hit or 404) is reused.
const DefaultCacheTTL = 5 * time.Minute

// ErrNotFound is returned when ewws-auth does not know the scope (404).
var ErrNotFound = errors.New("ewwsauth: scope not found")

// Client talks to the ewws-auth scope catalog with HTTP Basic auth.
// Safe for concurrent use. The zero value is disabled.
type Client struct {
	BaseURL      string
	ClientID     string
	ClientSecret string

	HTTP *http.Client

	// CacheTTL overrides DefaultCacheTTL when > 0.
	CacheTTL time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
	now   func() time.Time // test hook
}

type cacheEntry struct {
	scope   *Scope
	err     error // nil or ErrNotFound
	expires time.Time
}

// New returns a Client backed by a 10s-timeout *http.Client.
func New(baseURL, clientID, clientSecret string) *Client {
	return &Client{
		BaseURL:      strings.TrimRight(baseURL, "/"),
		ClientID:     clientID,
		ClientSecret: clientSecret,
		HTTP:         &http.Client{Timeout: 10 * time.Second},
	}
}

// Scope is a catalog entry. Nullable JSON fields decode to "".
type Scope struct {
	Name        string
	Description string
	URLs        []ScopeURL
	Permissions []Permission
}

// ScopeURL is one environment URL of a scope.
type ScopeURL struct {
	Environment string
	URL         string
	Label       string
}

// Permission is one permission the scope grants.
type Permission struct {
	Key         string
	Name        string
	Description string
	Category    string
}

// Enabled reports whether base URL, client id and secret are all set.
func (c *Client) Enabled() bool {
	return c != nil && c.BaseURL != "" && c.ClientID != "" && c.ClientSecret != ""
}

// Scope fetches the catalog entry for name. Returns ErrNotFound for an
// unknown scope. Successes and ErrNotFound are cached; other errors are
// not.
func (c *Client) Scope(ctx context.Context, name string) (*Scope, error) {
	if !c.Enabled() {
		return nil, errors.New("ewwsauth: client not configured")
	}

	if s, err, ok := c.cached(name); ok {
		return s, err
	}

	s, err := c.fetch(ctx, name)
	if err == nil || errors.Is(err, ErrNotFound) {
		c.store(name, s, err)
	}

	return s, err
}

func (c *Client) clock() time.Time {
	if c.now != nil {
		return c.now()
	}

	return time.Now()
}

func (c *Client) cached(name string) (*Scope, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.cache[name]
	if !ok || !c.clock().Before(e.expires) {
		return nil, nil, false
	}

	return e.scope, e.err, true
}

func (c *Client) store(name string, s *Scope, err error) {
	ttl := c.CacheTTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil {
		c.cache = make(map[string]cacheEntry)
	}

	c.cache[name] = cacheEntry{scope: s, err: err, expires: c.clock().Add(ttl)}
}

type wireScope struct {
	Name        string  `json:"name"`
	Description *string `json:"description"`
	URLs        []struct {
		Environment string  `json:"environment"`
		URL         string  `json:"url"`
		Label       *string `json:"label"`
	} `json:"urls"`
	Permissions []struct {
		Key         string  `json:"key"`
		Name        string  `json:"name"`
		Description *string `json:"description"`
		Category    *string `json:"category"`
	} `json:"permissions"`
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}

func (c *Client) fetch(ctx context.Context, name string) (*Scope, error) {
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/scope-catalog/" + url.PathEscape(name)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("ewwsauth: build request: %w", err)
	}

	req.SetBasicAuth(c.ClientID, c.ClientSecret)
	req.Header.Set("Accept", "application/json")

	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ewwsauth: get scope %q: %w", name, err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

		return nil, fmt.Errorf(
			"ewwsauth: get scope %q: unexpected status %d",
			name,
			resp.StatusCode,
		)
	}

	var w wireScope
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&w); err != nil {
		return nil, fmt.Errorf("ewwsauth: decode scope %q: %w", name, err)
	}

	s := &Scope{Name: w.Name, Description: deref(w.Description)}

	for _, u := range w.URLs {
		s.URLs = append(
			s.URLs,
			ScopeURL{Environment: u.Environment, URL: u.URL, Label: deref(u.Label)},
		)
	}

	for _, p := range w.Permissions {
		s.Permissions = append(s.Permissions, Permission{
			Key:         p.Key,
			Name:        p.Name,
			Description: deref(p.Description),
			Category:    deref(p.Category),
		})
	}

	return s, nil
}
