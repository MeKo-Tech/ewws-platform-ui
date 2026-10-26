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
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

const (
	activityTimeout   = 10 * time.Second
	activityBodyLimit = 1 << 20  // 1 MiB; a client's accounts list is a few hundred rows
	errorBodyLimit    = 64 << 10 // error replies are a short GraphQL-shaped JSON
)

var (
	// ErrNotConfigured is returned by a nil or disabled ActivityClient.
	ErrNotConfigured = errors.New("ewwsauth: activity client not configured")
	// ErrForbidden matches an *APIError with status 403: the machine
	// account lacks the auth:activity:r grant.
	ErrForbidden = errors.New("ewwsauth: forbidden")
	// ErrUnauthenticated matches an *APIError with status 401: ewws-auth
	// rejected the machine token.
	ErrUnauthenticated = errors.New("ewwsauth: unauthenticated")
)

// APIError is a non-200 reply of the activity API. Code and Message come
// from the GraphQL-shaped error body ("" when it had none). It matches
// ErrForbidden (403) and ErrUnauthenticated (401) via errors.Is.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	s := fmt.Sprintf("ewwsauth: activity api: status %d", e.Status)
	if e.Code != "" {
		s += " " + e.Code
	}

	if e.Message != "" {
		s += ": " + e.Message
	}

	return s
}

// Is lets errors.Is(err, ErrForbidden) / ErrUnauthenticated match.
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrForbidden:
		return e.Status == http.StatusForbidden
	case ErrUnauthenticated:
		return e.Status == http.StatusUnauthorized
	default:
		return false
	}
}

// ActivityClient reads login activity from ewws-auth
// (`/api/activity/clients[/{client_id}]`) with a machine token obtained
// via client_credentials at `/login/azure`. The token is cached and
// refreshed before expiry; it never leaves this type — not in errors,
// not in logs. Safe for concurrent use. A nil client is disabled.
type ActivityClient struct {
	baseURL  string
	tokenURL string
	enabled  bool
	http     *http.Client
	tokens   oauth2.TokenSource
}

// NewActivityClient builds a client for baseURL (the ewws-auth origin,
// e.g. https://login.ewws.meko.dev) with the Entra app credential the
// machine account signs in with. Missing values yield a disabled client.
func NewActivityClient(baseURL, clientID, clientSecret string) *ActivityClient {
	base := strings.TrimRight(baseURL, "/")
	hc := &http.Client{Timeout: activityTimeout}

	cc := clientcredentials.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		TokenURL:     base + "/login/azure",
		AuthStyle:    oauth2.AuthStyleInParams,
	}

	// The token source keeps this context for every later refresh, so it
	// must not be a request context; the HTTP client's timeout bounds it.
	tokenCtx := context.WithValue(context.Background(), oauth2.HTTPClient, hc)

	return &ActivityClient{
		baseURL:  base,
		tokenURL: cc.TokenURL,
		enabled:  base != "" && clientID != "" && clientSecret != "",
		http:     hc,
		tokens:   cc.TokenSource(tokenCtx), // a ReuseTokenSource
	}
}

// Enabled reports whether base URL and both credentials are set.
func (c *ActivityClient) Enabled() bool {
	return c != nil && c.enabled
}

// ActivityWindow is the activity of one client in one time window.
type ActivityWindow struct {
	Users     int `json:"users"`
	Logins    int `json:"logins"`
	Refreshes int `json:"refreshes"`
	Uses      int `json:"uses"`
}

// ClientActivity is one (issuer, client_id) row of the overview.
type ClientActivity struct {
	Issuer     string         `json:"issuer"`    // "ewws-auth" or "keycloak"
	ClientID   string         `json:"client_id"` // "" = logins that named no client
	Registered bool           `json:"registered"`
	Name       string         `json:"name"`
	Last1d     ActivityWindow `json:"last_1d"`
	Last7d     ActivityWindow `json:"last_7d"`
	Last30d    ActivityWindow `json:"last_30d"`
}

// ClientsResult is the reply of GET /api/activity/clients.
type ClientsResult struct {
	Today   string           `json:"today"` // YYYY-MM-DD, UTC
	Clients []ClientActivity `json:"clients"`
}

// AccountActivity is one account's use of a client in the requested range.
type AccountActivity struct {
	AccountID  string    `json:"account_id"`
	Name       string    `json:"name"`
	PersonioID *int      `json:"personio_id"`
	Logins     int       `json:"logins"`
	Refreshes  int       `json:"refreshes"`
	Uses       int       `json:"uses"`
	ActiveDays int       `json:"active_days"`
	LastSeenAt time.Time `json:"last_seen_at"`
}

// ClientResult is the reply of GET /api/activity/clients/{client_id}.
type ClientResult struct {
	Issuer     string            `json:"issuer"`
	ClientID   string            `json:"client_id"`
	Registered bool              `json:"registered"`
	Name       string            `json:"name"`
	From       string            `json:"from"`
	To         string            `json:"to"`
	Accounts   []AccountActivity `json:"accounts"`
}

// Clients returns the per-client activity of the last 1/7/30 days.
func (c *ActivityClient) Clients(ctx context.Context) (*ClientsResult, error) {
	var out ClientsResult
	if err := c.get(ctx, "/api/activity/clients", nil, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

// Client returns the per-account activity of one client. clientID "" is
// the "no client named" bucket; issuer "" means both issuers; from/to
// (YYYY-MM-DD, inclusive) are optional and defaulted by ewws-auth.
func (c *ActivityClient) Client(
	ctx context.Context,
	issuer, clientID, from, to string,
) (*ClientResult, error) {
	pathID := clientID
	if pathID == "" {
		pathID = "-"
	}

	q := url.Values{}

	for k, v := range map[string]string{"issuer": issuer, "from": from, "to": to} {
		if v != "" {
			q.Set(k, v)
		}
	}

	var out ClientResult
	if err := c.get(ctx, "/api/activity/clients/"+url.PathEscape(pathID), q, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

func (c *ActivityClient) get(ctx context.Context, path string, q url.Values, out any) error {
	if !c.Enabled() {
		return ErrNotConfigured
	}

	tok, err := c.token()
	if err != nil {
		return err
	}

	endpoint := c.baseURL + path
	if len(q) > 0 {
		endpoint += "?" + q.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("ewwsauth: build activity request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		// *url.Error names method, URL and cause — no header values.
		return fmt.Errorf("ewwsauth: get %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return decodeAPIError(resp, tok)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, activityBodyLimit)).
		Decode(out); err != nil {
		return fmt.Errorf("ewwsauth: decode %s: %w", path, err)
	}

	return nil
}

// token returns a cached or freshly fetched machine access token. Its
// errors carry the token URL and HTTP status only: an
// oauth2.RetrieveError's text can quote the token endpoint's reply.
func (c *ActivityClient) token() (string, error) {
	tok, err := c.tokens.Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && re.Response != nil {
			return "", fmt.Errorf(
				"ewwsauth: fetch machine token from %s: status %d",
				c.tokenURL,
				re.Response.StatusCode,
			)
		}

		return "", fmt.Errorf("ewwsauth: fetch machine token from %s failed", c.tokenURL)
	}

	if tok.AccessToken == "" {
		return "", fmt.Errorf("ewwsauth: fetch machine token from %s: empty token", c.tokenURL)
	}

	return tok.AccessToken, nil
}

type wireErrors struct {
	Errors []struct {
		Message    string `json:"message"`
		Extensions struct {
			Code string `json:"code"`
		} `json:"extensions"`
	} `json:"errors"`
}

// decodeAPIError reads the GraphQL-shaped error body. A body that is not
// that shape is dropped, not quoted. The token is scrubbed from the
// message defensively, in case a server ever echoes it.
func decodeAPIError(resp *http.Response, token string) *APIError {
	apiErr := &APIError{Status: resp.StatusCode}

	var w wireErrors
	if err := json.NewDecoder(io.LimitReader(resp.Body, errorBodyLimit)).Decode(&w); err == nil &&
		len(w.Errors) > 0 {
		apiErr.Code = w.Errors[0].Extensions.Code
		apiErr.Message = w.Errors[0].Message
	}

	if token != "" {
		apiErr.Message = strings.ReplaceAll(apiErr.Message, token, "[redacted]")
	}

	return apiErr
}
