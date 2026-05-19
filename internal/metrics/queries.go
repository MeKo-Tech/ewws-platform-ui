// Package metrics holds the per-(slug,stage) Prometheus queries we run
// from the metrics scanner and the typed Store the HTTP handlers read.
//
// Prometheus is treated as best-effort: every query function returns
// zero values (not an error) when the client is unconfigured or the
// upstream is unreachable, so the UI keeps working in degraded mode.
package metrics

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/prometheus"
)

// serviceRegex returns the regex placed inside `service=~"…"` selectors.
// chungus's Traefik IngressRoutes generate service labels of the form
// `tenant-<slug>-<stage>-…@kubernetescrd` or `…@kubernetes` — we anchor
// loosely with `.*` on both ends so any of the suffix variants match.
func serviceRegex(slug, stage string) string {
	return fmt.Sprintf(".*tenant-%s-%s.*", slug, stage)
}

// namespace returns the K8s namespace the slug+stage live in.
func namespace(slug, stage string) string {
	return fmt.Sprintf("tenant-%s-%s", slug, stage)
}

// requestsWindow runs the standard "requests over a window" instant
// query. Returns 0 when Prometheus reports an empty vector (no series
// matched — likely a brand-new tenant with no traffic yet).
func requestsWindow(
	ctx context.Context,
	c *prometheus.Client,
	slug, stage, window string,
) (int64, error) {
	if c == nil {
		return 0, nil
	}

	q := fmt.Sprintf(
		`sum(increase(traefik_service_requests_total{service=~"%s"}[%s]))`,
		serviceRegex(slug, stage), window,
	)

	v, _, err := c.Instant(ctx, q)
	if err != nil {
		return 0, err
	}

	return int64(math.Round(v)), nil
}

// Requests24h returns the total request count over the last 24 hours.
func Requests24h(ctx context.Context, c *prometheus.Client, slug, stage string) (int64, error) {
	return requestsWindow(ctx, c, slug, stage, "24h")
}

// Requests7d returns the total request count over the last 7 days.
func Requests7d(ctx context.Context, c *prometheus.Client, slug, stage string) (int64, error) {
	return requestsWindow(ctx, c, slug, stage, "7d")
}

// LastRequestAt looks back `lookback` (default 30d when zero) at 1h step
// for the most recent non-zero bucket. Returns nil if no traffic ever.
func LastRequestAt(
	ctx context.Context, c *prometheus.Client, slug, stage string, lookback time.Duration,
) (*time.Time, error) {
	if c == nil {
		return nil, nil
	}

	if lookback == 0 {
		lookback = 30 * 24 * time.Hour
	}

	end := time.Now().UTC()
	start := end.Add(-lookback)
	step := time.Hour

	q := fmt.Sprintf(
		`sum(increase(traefik_service_requests_total{service=~"%s"}[1h]))`,
		serviceRegex(slug, stage),
	)

	samples, err := c.Range(ctx, q, start, end, step)
	if err != nil {
		return nil, err
	}

	for i := len(samples) - 1; i >= 0; i-- {
		if samples[i].V > 0 {
			t := samples[i].T
			return &t, nil
		}
	}

	return nil, nil
}

// ErrorRate5xx returns the 5-minute rate of 5xx responses divided by all
// responses. Always in [0,1]; returns 0 when the denominator is 0 (no
// traffic — NaN-safe).
func ErrorRate5xx(ctx context.Context, c *prometheus.Client, slug, stage string) (float64, error) {
	if c == nil {
		return 0, nil
	}

	q := fmt.Sprintf(
		`sum(rate(traefik_service_requests_total{service=~"%s",code=~"5.."}[5m])) / `+
			`sum(rate(traefik_service_requests_total{service=~"%s"}[5m]))`,
		serviceRegex(slug, stage), serviceRegex(slug, stage),
	)

	v, _, err := c.Instant(ctx, q)
	if err != nil {
		return 0, err
	}

	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, nil
	}

	return v, nil
}

// Restarts24h returns the total container-restart count for the
// namespace over the last 24h.
func Restarts24h(ctx context.Context, c *prometheus.Client, slug, stage string) (int64, error) {
	if c == nil {
		return 0, nil
	}

	q := fmt.Sprintf(
		`sum(increase(kube_pod_container_status_restarts_total{namespace="%s"}[24h]))`,
		namespace(slug, stage),
	)

	v, _, err := c.Instant(ctx, q)
	if err != nil {
		return 0, err
	}

	return int64(math.Round(v)), nil
}

// MemoryUsedBytes returns the working-set memory of all non-pause
// containers in the namespace.
func MemoryUsedBytes(ctx context.Context, c *prometheus.Client, slug, stage string) (int64, error) {
	if c == nil {
		return 0, nil
	}

	q := fmt.Sprintf(
		`sum(container_memory_working_set_bytes{namespace="%s",container!=""})`,
		namespace(slug, stage),
	)

	v, _, err := c.Instant(ctx, q)
	if err != nil {
		return 0, err
	}

	return int64(math.Round(v)), nil
}

// MemoryLimitBytes sums the memory limits set on every container in the
// namespace. Returns 0 when no limits are set.
func MemoryLimitBytes(
	ctx context.Context,
	c *prometheus.Client,
	slug, stage string,
) (int64, error) {
	if c == nil {
		return 0, nil
	}

	q := fmt.Sprintf(
		`sum(kube_pod_container_resource_limits{namespace="%s",resource="memory"})`,
		namespace(slug, stage),
	)

	v, _, err := c.Instant(ctx, q)
	if err != nil {
		return 0, err
	}

	return int64(math.Round(v)), nil
}

// CPUUsedMillicores returns the average CPU consumption (millicores)
// over the last 5 minutes across every container in the namespace.
func CPUUsedMillicores(
	ctx context.Context,
	c *prometheus.Client,
	slug, stage string,
) (int64, error) {
	if c == nil {
		return 0, nil
	}

	q := fmt.Sprintf(
		`1000 * sum(rate(container_cpu_usage_seconds_total{namespace="%s",container!=""}[5m]))`,
		namespace(slug, stage),
	)

	v, _, err := c.Instant(ctx, q)
	if err != nil {
		return 0, err
	}

	return int64(math.Round(v)), nil
}

// CPULimitMillicores sums the per-container CPU limits (in millicores).
func CPULimitMillicores(
	ctx context.Context,
	c *prometheus.Client,
	slug, stage string,
) (int64, error) {
	if c == nil {
		return 0, nil
	}

	q := fmt.Sprintf(
		`1000 * sum(kube_pod_container_resource_limits{namespace="%s",resource="cpu"})`,
		namespace(slug, stage),
	)

	v, _, err := c.Instant(ctx, q)
	if err != nil {
		return 0, err
	}

	return int64(math.Round(v)), nil
}

// SparklineHourly returns one count per hour-bucket over `days*24h` of
// history. Oldest sample first. The returned slice may be shorter than
// days*24 when Prometheus doesn't have that much retention.
func SparklineHourly(
	ctx context.Context, c *prometheus.Client, slug, stage string, days int,
) ([]int64, error) {
	if c == nil || days <= 0 {
		return nil, nil
	}

	end := time.Now().UTC()
	start := end.Add(-time.Duration(days) * 24 * time.Hour)
	step := time.Hour

	q := fmt.Sprintf(
		`sum(increase(traefik_service_requests_total{service=~"%s"}[1h]))`,
		serviceRegex(slug, stage),
	)

	samples, err := c.Range(ctx, q, start, end, step)
	if err != nil {
		return nil, err
	}

	out := make([]int64, 0, len(samples))
	for _, s := range samples {
		if math.IsNaN(s.V) || math.IsInf(s.V, 0) {
			out = append(out, 0)
			continue
		}

		out = append(out, int64(math.Round(s.V)))
	}

	return out, nil
}
