package ewwsauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const sampleBody = `{
  "name": "ewlt",
  "description": null,
  "urls": [
    {"environment": "prod", "url": "https://ewlt.example", "label": "Live"},
    {"environment": "staging", "url": "https://ewlt-staging.example", "label": null}
  ],
  "permissions": [
    {"key": "ewlt:full", "name": "Vollzugriff", "description": null, "category": null},
    {"key": "ewlt:r", "name": "Lesen", "description": "nur lesen", "category": "ewlt"}
  ]
}`

func newServer(t *testing.T, h http.HandlerFunc) (*Client, *atomic.Int32) {
	t.Helper()

	var hits atomic.Int32

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h(w, r)
	}))
	t.Cleanup(srv.Close)

	return New(srv.URL+"/", "platform-ui", "s3cret"), &hits
}

func TestScopeDecodesIncludingNulls(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/scope-catalog/ewlt" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}

		user, pass, ok := r.BasicAuth()
		if !ok || user != "platform-ui" || pass != "s3cret" {
			t.Errorf("basic auth = %q/%q ok=%v", user, pass, ok)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleBody))
	})

	s, err := c.Scope(context.Background(), "ewlt")
	if err != nil {
		t.Fatalf("Scope: %v", err)
	}

	if s.Name != "ewlt" || s.Description != "" {
		t.Errorf("name/description = %q/%q", s.Name, s.Description)
	}

	if len(s.URLs) != 2 || s.URLs[0].Label != "Live" || s.URLs[1].Label != "" ||
		s.URLs[1].Environment != "staging" {
		t.Errorf("urls = %+v", s.URLs)
	}

	if len(s.Permissions) != 2 {
		t.Fatalf("permissions = %+v", s.Permissions)
	}

	if p := s.Permissions[0]; p.Key != "ewlt:full" || p.Description != "" || p.Category != "" {
		t.Errorf("perm[0] = %+v", p)
	}

	if p := s.Permissions[1]; p.Description != "nur lesen" || p.Category != "ewlt" {
		t.Errorf("perm[1] = %+v", p)
	}
}

func TestScopeNotFoundIsSentinelAndCached(t *testing.T) {
	c, hits := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"scope not found"}`))
	})

	for range 2 {
		if _, err := c.Scope(context.Background(), "nope"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	}

	if got := hits.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 (404 cached)", got)
	}
}

func TestScopeServerErrorNotCachedAndNoSecret(t *testing.T) {
	c, hits := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	for range 2 {
		_, err := c.Scope(context.Background(), "ewlt")
		if err == nil || errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want non-404 error", err)
		}

		if !strings.Contains(err.Error(), "500") {
			t.Errorf("error %q lacks status code", err)
		}

		if strings.Contains(err.Error(), "s3cret") {
			t.Errorf("error %q leaks the secret", err)
		}
	}

	if got := hits.Load(); got != 2 {
		t.Errorf("requests = %d, want 2 (errors not cached)", got)
	}
}

func TestScopeCacheHitAndExpiry(t *testing.T) {
	c, hits := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleBody))
	})

	now := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return now }

	for range 3 {
		if _, err := c.Scope(context.Background(), "ewlt"); err != nil {
			t.Fatalf("Scope: %v", err)
		}
	}

	if got := hits.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 (cache hit)", got)
	}

	now = now.Add(DefaultCacheTTL + time.Second)

	if _, err := c.Scope(context.Background(), "ewlt"); err != nil {
		t.Fatalf("Scope after expiry: %v", err)
	}

	if got := hits.Load(); got != 2 {
		t.Errorf("requests = %d, want 2 after expiry", got)
	}
}

func TestScopePathEscaping(t *testing.T) {
	c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/scope-catalog/a%2Fb%20c" {
			t.Errorf("escaped path = %q", r.URL.EscapedPath())
		}

		w.WriteHeader(http.StatusNotFound)
	})

	if _, err := c.Scope(context.Background(), "a/b c"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnabled(t *testing.T) {
	var nilClient *Client
	if nilClient.Enabled() {
		t.Error("nil client enabled")
	}

	if New("https://x", "id", "").Enabled() {
		t.Error("client without secret enabled")
	}

	if !New("https://x", "id", "secret").Enabled() {
		t.Error("full client disabled")
	}
}
