// Package status aggregates everything we know about a single tenant —
// the registry claim, the per-stage Argo CD application, the latest
// Prometheus metrics snapshot, drift between staging and prod, and the
// compliance check results — into one struct the landing page can render
// without doing any further joins.
//
// The actual activity classification (aktiv / ruhig / idle / verwaist /
// ungesund) is NOT computed here. It is applied client-side from the
// raw numbers so users can tweak thresholds without a server round-trip.
package status

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/argocd"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/compliance"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/drift"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/metrics"
	"github.com/MeKo-Tech/ewws-platform-ui/internal/registry"
)

// Tenant is the merged view per slug. Frontend renders one row / card
// per Tenant and one mini-block per Stage inside it.
type Tenant struct {
	Slug   string
	Owners registry.Owners
	Repo   registry.Repo

	// Stages always contains both "staging" and "prod" entries (even
	// when Argo CD has no Application yet) so the template can render
	// missing-state placeholders without nil checks.
	Stages map[string]Stage

	// Drift keyed by component ("backend" | "frontend"). Missing entry
	// means the drift scanner hasn't seen this component yet (e.g. an
	// app that only just claimed and hasn't released a prod tag).
	Drift map[string]drift.Snapshot

	// Compliance rows from the compliance_check store; usually three
	// entries (pr_title_workflow, release_please_workflow,
	// branch_protection_main). Empty if the scanner hasn't run yet.
	Compliance []compliance.CheckResult
}

// Stage holds the per-stage Argo CD state + the latest metrics row.
type Stage struct {
	Stage        string // "staging" | "prod"
	Host         string
	SyncStatus   string // "Synced" | "OutOfSync" | "Unknown" | ""
	HealthStatus string // "Healthy" | "Progressing" | "Degraded" | ""
	LastDeployAt *time.Time

	// Metrics may be nil if the metrics scanner hasn't run yet for
	// this (slug, stage) pair (or PrometheusURL is unset).
	Metrics *metrics.Snapshot
}

// Aggregator is the read-side of the dashboard. Construct it once at
// boot and call BuildAll() per page render.
type Aggregator struct {
	Argo            *argocd.Client
	MetricsStore    *metrics.Store
	DriftStore      *drift.Store
	ComplianceStore *compliance.Store
	Logger          *slog.Logger
}

// BuildAll merges every registry App with whatever data the various
// stores already hold. It never errors — missing data degrades to empty
// fields so the UI can still render. Argo CD / store failures are
// logged at warn level and produce empty per-stage status blocks.
func (a *Aggregator) BuildAll(ctx context.Context, apps []registry.App) []Tenant {
	argoBySlugStage := a.fetchArgoIndex(ctx)
	metricsBySlugStage := a.fetchMetricsIndex(ctx)
	driftBySlugComponent := a.fetchDriftIndex(ctx)
	complianceBySlug := a.fetchComplianceIndex(ctx)

	out := make([]Tenant, 0, len(apps))

	for _, app := range apps {
		t := Tenant{
			Slug:       app.Slug,
			Owners:     app.Owners,
			Repo:       app.Repo,
			Stages:     map[string]Stage{},
			Drift:      driftBySlugComponent[app.Slug],
			Compliance: complianceBySlug[app.Slug],
		}

		for _, stage := range []string{"staging", "prod"} {
			t.Stages[stage] = a.buildStage(
				app, stage,
				argoBySlugStage[stageKey{app.Slug, stage}],
				metricsBySlugStage[stageKey{app.Slug, stage}],
			)
		}

		out = append(out, t)
	}

	return out
}

func (a *Aggregator) buildStage(
	app registry.App, stage string,
	argoApp *argocd.Application, snap *metrics.Snapshot,
) Stage {
	s := Stage{Stage: stage, Metrics: snap}

	switch stage {
	case "staging":
		s.Host = app.Stages.Staging.Host
	case "prod":
		s.Host = app.Stages.Prod.Host
	}

	if argoApp != nil {
		s.SyncStatus = argoApp.Status.Sync.Status
		s.HealthStatus = argoApp.Status.Health.Status

		if !argoApp.Status.OperationState.FinishedAt.IsZero() {
			t := argoApp.Status.OperationState.FinishedAt
			s.LastDeployAt = &t
		}
	}

	return s
}

// stageKey is the composite map key for per-stage indexes.
type stageKey struct{ slug, stage string }

func (a *Aggregator) fetchArgoIndex(ctx context.Context) map[stageKey]*argocd.Application {
	out := map[stageKey]*argocd.Application{}

	if a.Argo == nil {
		return out
	}

	apps, err := a.Argo.ListApplications(ctx)
	if err != nil {
		a.warn("argo list applications failed", "err", err)

		return out
	}

	for i := range apps {
		slug, stage := splitSlugStage(apps[i].Metadata.Name)
		if slug == "" {
			continue
		}

		copy := apps[i]
		out[stageKey{slug, stage}] = &copy
	}

	return out
}

func (a *Aggregator) fetchMetricsIndex(ctx context.Context) map[stageKey]*metrics.Snapshot {
	out := map[stageKey]*metrics.Snapshot{}

	if a.MetricsStore == nil {
		return out
	}

	snaps, err := a.MetricsStore.List(ctx)
	if err != nil {
		a.warn("metrics store list failed", "err", err)

		return out
	}

	for i := range snaps {
		s := snaps[i]
		out[stageKey{s.Slug, s.Stage}] = &s
	}

	return out
}

func (a *Aggregator) fetchDriftIndex(ctx context.Context) map[string]map[string]drift.Snapshot {
	out := map[string]map[string]drift.Snapshot{}

	if a.DriftStore == nil {
		return out
	}

	snaps, err := a.DriftStore.List(ctx)
	if err != nil {
		a.warn("drift store list failed", "err", err)

		return out
	}

	for _, s := range snaps {
		if out[s.Slug] == nil {
			out[s.Slug] = map[string]drift.Snapshot{}
		}

		out[s.Slug][string(s.Component)] = s
	}

	return out
}

func (a *Aggregator) fetchComplianceIndex(ctx context.Context) map[string][]compliance.CheckResult {
	out := map[string][]compliance.CheckResult{}

	if a.ComplianceStore == nil {
		return out
	}

	rows, err := a.ComplianceStore.List(ctx)
	if err != nil {
		a.warn("compliance store list failed", "err", err)

		return out
	}

	for _, r := range rows {
		out[r.Slug] = append(out[r.Slug], r)
	}

	return out
}

// splitSlugStage parses an Argo CD application name of the form
// "<slug>-staging" or "<slug>-prod". Returns ("","") for anything else.
func splitSlugStage(name string) (string, string) {
	switch {
	case strings.HasSuffix(name, "-staging"):
		return strings.TrimSuffix(name, "-staging"), "staging"
	case strings.HasSuffix(name, "-prod"):
		return strings.TrimSuffix(name, "-prod"), "prod"
	default:
		return "", ""
	}
}

func (a *Aggregator) warn(msg string, kv ...any) {
	if a.Logger == nil {
		return
	}

	args := make([]any, 0, len(kv))
	for i := 0; i+1 < len(kv); i += 2 {
		if k, ok := kv[i].(string); ok {
			args = append(args, slog.Any(k, kv[i+1]))
		}
	}

	a.Logger.Warn(msg, args...)
}
