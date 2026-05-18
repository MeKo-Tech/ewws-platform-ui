// Package argocd is a tiny HTTP wrapper around the Argo CD v1 REST API.
//
// We use only the read-only endpoints (`/api/v1/applications`,
// `/api/v1/applications/<name>/resource-tree`) — no SDK needed.
package argocd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a thin Argo CD HTTP client. Safe for concurrent use.
type Client struct {
	BaseURL string
	Token   string

	HTTP *http.Client
}

// New returns a Client backed by a 10s-timeout *http.Client.
func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Application is the subset of Argo CD's Application object we render.
type Application struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`

	Spec struct {
		Source struct {
			RepoURL        string `json:"repoURL"`
			TargetRevision string `json:"targetRevision"`
			Path           string `json:"path"`
		} `json:"source"`

		Destination struct {
			Namespace string `json:"namespace"`
			Server    string `json:"server"`
		} `json:"destination"`
	} `json:"spec"`

	Status struct {
		Sync struct {
			Status string `json:"status"` // Synced | OutOfSync | Unknown
		} `json:"sync"`

		Health struct {
			Status  string `json:"status"` // Healthy | Progressing | Degraded | Suspended | Missing | Unknown
			Message string `json:"message"`
		} `json:"health"`

		OperationState struct {
			FinishedAt time.Time `json:"finishedAt"`
			Phase      string    `json:"phase"`
		} `json:"operationState"`
	} `json:"status"`
}

// listResponse mirrors `{"items": [...]}` from `/api/v1/applications`.
type listResponse struct {
	Items []Application `json:"items"`
}

// ListApplications fetches all Argo CD Applications visible to the token.
func (c *Client) ListApplications(ctx context.Context) ([]Application, error) {
	var resp listResponse
	if err := c.getJSON(ctx, "/api/v1/applications", &resp); err != nil {
		return nil, err
	}

	return resp.Items, nil
}

// GetApplication fetches a single Application by name (e.g. "<slug>-staging").
func (c *Client) GetApplication(ctx context.Context, name string) (*Application, error) {
	var app Application
	if err := c.getJSON(ctx, "/api/v1/applications/"+url.PathEscape(name), &app); err != nil {
		return nil, err
	}

	return &app, nil
}

// ResourceNode is one entry from the resource-tree response.
type ResourceNode struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Group     string `json:"group"`
	Version   string `json:"version"`

	Health struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	} `json:"health"`
}

// ResourceTree mirrors `/api/v1/applications/<name>/resource-tree`.
type ResourceTree struct {
	Nodes []ResourceNode `json:"nodes"`
}

// GetResourceTree returns the (flattened) resource tree of an Application.
func (c *Client) GetResourceTree(ctx context.Context, name string) (*ResourceTree, error) {
	var tree ResourceTree

	path := "/api/v1/applications/" + url.PathEscape(name) + "/resource-tree"
	if err := c.getJSON(ctx, path, &tree); err != nil {
		return nil, err
	}

	return &tree, nil
}

func (c *Client) getJSON(ctx context.Context, path string, out any) error {
	if c.BaseURL == "" {
		return errors.New("argocd: BaseURL not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return fmt.Errorf("argocd: build request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("argocd: GET %s: %w", path, err)
	}

	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<13))
		return fmt.Errorf("argocd: GET %s: %s: %s", path, resp.Status, string(body))
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("argocd: decode %s: %w", path, err)
	}

	return nil
}
