package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/storage"
)

// Snapshot is one row of `metrics_snapshot`. One per (slug, stage).
type Snapshot struct {
	Slug               string
	Stage              string
	Requests24h        int64
	Requests7d         int64
	LastRequestAt      *time.Time // nil → no traffic ever in the lookback window
	ErrorRate5xx       float64    // 0..1
	Restarts24h        int64
	MemoryUsedBytes    int64
	MemoryLimitBytes   int64
	CPUUsedMillicores  int64
	CPULimitMillicores int64
	SparklineHourly    []int64 // oldest first
	CollectedAt        time.Time
}

// Store wraps the SQLite handle with typed helpers.
type Store struct {
	db *storage.DB
}

// NewStore returns a Store backed by the given DB.
func NewStore(db *storage.DB) *Store {
	return &Store{db: db}
}

// Upsert writes (or replaces) one (slug, stage) row.
func (s *Store) Upsert(ctx context.Context, snap Snapshot) error {
	var lastRequest sql.NullInt64
	if snap.LastRequestAt != nil {
		lastRequest = sql.NullInt64{Int64: snap.LastRequestAt.Unix(), Valid: true}
	}

	_, err := s.db.SQL().ExecContext(ctx, `
		INSERT INTO metrics_snapshot (
			slug, stage,
			requests_24h, requests_7d, last_request_at, error_rate_5xx,
			restarts_24h,
			memory_used_bytes, memory_limit_bytes,
			cpu_used_millicores, cpu_limit_millicores,
			sparkline_hourly, collected_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(slug, stage) DO UPDATE SET
			requests_24h         = excluded.requests_24h,
			requests_7d          = excluded.requests_7d,
			last_request_at      = excluded.last_request_at,
			error_rate_5xx       = excluded.error_rate_5xx,
			restarts_24h         = excluded.restarts_24h,
			memory_used_bytes    = excluded.memory_used_bytes,
			memory_limit_bytes   = excluded.memory_limit_bytes,
			cpu_used_millicores  = excluded.cpu_used_millicores,
			cpu_limit_millicores = excluded.cpu_limit_millicores,
			sparkline_hourly     = excluded.sparkline_hourly,
			collected_at         = excluded.collected_at
	`,
		snap.Slug, snap.Stage,
		snap.Requests24h, snap.Requests7d, lastRequest, snap.ErrorRate5xx,
		snap.Restarts24h,
		snap.MemoryUsedBytes, snap.MemoryLimitBytes,
		snap.CPUUsedMillicores, snap.CPULimitMillicores,
		encodeSparkline(snap.SparklineHourly), snap.CollectedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("upsert metrics_snapshot: %w", err)
	}

	return nil
}

// Get returns the snapshot for one (slug, stage), or ErrNotFound.
func (s *Store) Get(ctx context.Context, slug, stage string) (*Snapshot, error) {
	row := s.db.SQL().QueryRowContext(ctx, `
		SELECT slug, stage,
		       requests_24h, requests_7d, last_request_at, error_rate_5xx,
		       restarts_24h,
		       memory_used_bytes, memory_limit_bytes,
		       cpu_used_millicores, cpu_limit_millicores,
		       sparkline_hourly, collected_at
		FROM metrics_snapshot
		WHERE slug = ? AND stage = ?
	`, slug, stage)

	snap, err := scanSnapshot(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get metrics_snapshot: %w", err)
	}

	return snap, nil
}

// ListBySlug returns every stored snapshot for one slug.
func (s *Store) ListBySlug(ctx context.Context, slug string) ([]Snapshot, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `
		SELECT slug, stage,
		       requests_24h, requests_7d, last_request_at, error_rate_5xx,
		       restarts_24h,
		       memory_used_bytes, memory_limit_bytes,
		       cpu_used_millicores, cpu_limit_millicores,
		       sparkline_hourly, collected_at
		FROM metrics_snapshot
		WHERE slug = ?
		ORDER BY stage
	`, slug)
	if err != nil {
		return nil, fmt.Errorf("query metrics_snapshot: %w", err)
	}
	defer rows.Close()

	var out []Snapshot

	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		out = append(out, *snap)
	}

	return out, rows.Err()
}

// List returns every snapshot — useful for dashboard rollups.
func (s *Store) List(ctx context.Context) ([]Snapshot, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `
		SELECT slug, stage,
		       requests_24h, requests_7d, last_request_at, error_rate_5xx,
		       restarts_24h,
		       memory_used_bytes, memory_limit_bytes,
		       cpu_used_millicores, cpu_limit_millicores,
		       sparkline_hourly, collected_at
		FROM metrics_snapshot
		ORDER BY slug, stage
	`)
	if err != nil {
		return nil, fmt.Errorf("list metrics_snapshot: %w", err)
	}
	defer rows.Close()

	var out []Snapshot

	for rows.Next() {
		snap, err := scanSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		out = append(out, *snap)
	}

	return out, rows.Err()
}

// scanner is the subset of sql.Row + sql.Rows we depend on.
type scanner interface {
	Scan(dest ...any) error
}

func scanSnapshot(s scanner) (*Snapshot, error) {
	var (
		snap        Snapshot
		lastRequest sql.NullInt64
		sparkline   string
		collectedAt int64
	)

	err := s.Scan(
		&snap.Slug, &snap.Stage,
		&snap.Requests24h, &snap.Requests7d, &lastRequest, &snap.ErrorRate5xx,
		&snap.Restarts24h,
		&snap.MemoryUsedBytes, &snap.MemoryLimitBytes,
		&snap.CPUUsedMillicores, &snap.CPULimitMillicores,
		&sparkline, &collectedAt,
	)
	if err != nil {
		return nil, err
	}

	if lastRequest.Valid {
		t := time.Unix(lastRequest.Int64, 0).UTC()
		snap.LastRequestAt = &t
	}

	snap.SparklineHourly = decodeSparkline(sparkline)
	snap.CollectedAt = time.Unix(collectedAt, 0).UTC()

	return &snap, nil
}

// encodeSparkline serialises [1, 2, 3] → "1,2,3" for cheap text storage.
func encodeSparkline(values []int64) string {
	if len(values) == 0 {
		return ""
	}

	parts := make([]string, len(values))
	for i, v := range values {
		parts[i] = strconv.FormatInt(v, 10)
	}

	return strings.Join(parts, ",")
}

func decodeSparkline(s string) []int64 {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")

	out := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			// Skip malformed entries; partial data is better than no data.
			continue
		}

		out = append(out, v)
	}

	return out
}

// ErrNotFound is returned when a (slug, stage) snapshot is missing.
var ErrNotFound = errors.New("metrics: not found")
