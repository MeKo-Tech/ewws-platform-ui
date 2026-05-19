// Package prometheus is a tiny HTTP wrapper around the Prometheus
// query API.
//
// We use only the two read endpoints we need (`/api/v1/query` and
// `/api/v1/query_range`) — no SDK, no metric-name discovery, no
// alerting. chungus's kube-prometheus-stack runs unauthenticated
// in-cluster, so we don't add auth headers.
package prometheus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client is a thin Prometheus HTTP client. Safe for concurrent use.
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// New returns a Client backed by a 10s-timeout *http.Client.
func New(baseURL string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		HTTP:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Sample is one (timestamp, value) point from a range query.
type Sample struct {
	T time.Time
	V float64
}

// queryResponse mirrors the JSON envelope returned by both /query and
// /query_range. `Result` is left as raw JSON so the caller can decode it
// into the right shape (vector for instant, matrix for range).
type queryResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
	ErrorType string `json:"errorType,omitempty"`
	Error     string `json:"error,omitempty"`
}

// vectorEntry is one row of a `resultType=vector` result. Each row's
// `Value` is a (`unix-seconds-as-float`, `value-as-string`) tuple.
type vectorEntry struct {
	Metric map[string]string `json:"metric"`
	Value  [2]any            `json:"value"`
}

// matrixEntry is one row of a `resultType=matrix` result.
type matrixEntry struct {
	Metric map[string]string `json:"metric"`
	Values [][2]any          `json:"values"`
}

// Instant runs an instant query and returns the first scalar value of the
// resulting vector. An empty vector (no series matched the label
// selector — e.g. the metric simply doesn't exist for this app yet) is
// NOT an error: we return (0, zero-time, nil) so the caller can treat
// "no traffic" the same as "zero traffic".
func (c *Client) Instant(ctx context.Context, query string) (float64, time.Time, error) {
	form := url.Values{}
	form.Set("query", query)

	var env queryResponse
	if err := c.post(ctx, "/api/v1/query", form, &env); err != nil {
		return 0, time.Time{}, err
	}

	if env.Data.ResultType != "vector" {
		return 0, time.Time{}, fmt.Errorf(
			"prometheus: unexpected resultType %q (want vector)",
			env.Data.ResultType,
		)
	}

	var rows []vectorEntry
	if err := json.Unmarshal(env.Data.Result, &rows); err != nil {
		return 0, time.Time{}, fmt.Errorf("prometheus: decode vector: %w", err)
	}

	if len(rows) == 0 {
		return 0, time.Time{}, nil
	}

	t, v, err := parsePoint(rows[0].Value)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("prometheus: decode point: %w", err)
	}

	return v, t, nil
}

// Range runs a range query and returns the (timestamp,value) samples of
// the first row. Our queries always aggregate down to a single row, so
// extra rows are ignored. Empty result → (nil, nil).
func (c *Client) Range(
	ctx context.Context, query string, start, end time.Time, step time.Duration,
) ([]Sample, error) {
	form := url.Values{}
	form.Set("query", query)
	form.Set("start", strconv.FormatFloat(timeToUnix(start), 'f', -1, 64))
	form.Set("end", strconv.FormatFloat(timeToUnix(end), 'f', -1, 64))
	form.Set("step", strconv.FormatFloat(step.Seconds(), 'f', -1, 64))

	var env queryResponse
	if err := c.post(ctx, "/api/v1/query_range", form, &env); err != nil {
		return nil, err
	}

	if env.Data.ResultType != "matrix" {
		return nil, fmt.Errorf(
			"prometheus: unexpected resultType %q (want matrix)",
			env.Data.ResultType,
		)
	}

	var rows []matrixEntry
	if err := json.Unmarshal(env.Data.Result, &rows); err != nil {
		return nil, fmt.Errorf("prometheus: decode matrix: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil
	}

	out := make([]Sample, 0, len(rows[0].Values))
	for _, p := range rows[0].Values {
		t, v, err := parsePoint(p)
		if err != nil {
			return nil, fmt.Errorf("prometheus: decode point: %w", err)
		}

		out = append(out, Sample{T: t, V: v})
	}

	return out, nil
}

func (c *Client) post(ctx context.Context, path string, form url.Values, out any) error {
	if c.BaseURL == "" {
		return errors.New("prometheus: BaseURL not configured")
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.BaseURL+path,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return fmt.Errorf("prometheus: build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("prometheus: POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<13))
		return fmt.Errorf("prometheus: POST %s: %s: %s", path, resp.Status, string(body))
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("prometheus: decode %s: %w", path, err)
	}

	env, _ := out.(*queryResponse)
	if env != nil && env.Status != "success" {
		return fmt.Errorf("prometheus: %s: %s: %s", path, env.ErrorType, env.Error)
	}

	return nil
}

// parsePoint decodes a Prometheus `[unix-seconds, "value"]` tuple. The
// timestamp arrives as JSON-number (float64) and the value as a string
// — Prometheus encodes NaN/Inf as text so floats round-trip exactly.
func parsePoint(raw [2]any) (time.Time, float64, error) {
	tsFloat, ok := raw[0].(float64)
	if !ok {
		return time.Time{}, 0, fmt.Errorf("ts not float64: %T", raw[0])
	}

	valStr, ok := raw[1].(string)
	if !ok {
		return time.Time{}, 0, fmt.Errorf("value not string: %T", raw[1])
	}

	v, err := strconv.ParseFloat(valStr, 64)
	if err != nil {
		return time.Time{}, 0, fmt.Errorf("parse value %q: %w", valStr, err)
	}

	t := time.Unix(int64(tsFloat), int64((tsFloat-float64(int64(tsFloat)))*1e9)).UTC()

	return t, v, nil
}

// timeToUnix returns t as seconds-since-epoch with sub-second precision.
func timeToUnix(t time.Time) float64 {
	return float64(t.UnixNano()) / 1e9
}
