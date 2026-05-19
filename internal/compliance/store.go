package compliance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/storage"
)

// Status enumerates the result of a single compliance check.
type Status string

const (
	StatusPass    Status = "pass"
	StatusFail    Status = "fail"
	StatusError   Status = "error" // transient failure (rate-limit, 5xx, etc.)
	StatusUnknown Status = "unknown"
)

// CheckResult is the typed row stored in compliance_check.
type CheckResult struct {
	Slug        string
	Repo        string // "owner/repo"
	CheckName   string
	Status      Status
	Details     string
	LastChecked time.Time
}

// Store wraps the SQLite handle with typed helpers.
type Store struct {
	db *storage.DB
}

// NewStore returns a Store backed by the given DB.
func NewStore(db *storage.DB) *Store {
	return &Store{db: db}
}

// Upsert writes (or replaces) a single check result.
func (s *Store) Upsert(ctx context.Context, r CheckResult) error {
	_, err := s.db.SQL().ExecContext(ctx, `
		INSERT INTO compliance_check (slug, repo, check_name, status, details, last_checked)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(slug, check_name) DO UPDATE SET
			repo = excluded.repo,
			status = excluded.status,
			details = excluded.details,
			last_checked = excluded.last_checked
	`,
		r.Slug, r.Repo, r.CheckName, string(r.Status), r.Details, r.LastChecked.Unix(),
	)
	if err != nil {
		return fmt.Errorf("upsert compliance_check: %w", err)
	}
	return nil
}

// ListBySlug returns every check stored for one slug.
func (s *Store) ListBySlug(ctx context.Context, slug string) ([]CheckResult, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `
		SELECT slug, repo, check_name, status, details, last_checked
		FROM compliance_check
		WHERE slug = ?
		ORDER BY check_name
	`, slug)
	if err != nil {
		return nil, fmt.Errorf("query compliance_check: %w", err)
	}
	defer rows.Close()

	var out []CheckResult
	for rows.Next() {
		var (
			r          CheckResult
			ts         int64
			statusStr  string
		)
		if err := rows.Scan(&r.Slug, &r.Repo, &r.CheckName, &statusStr, &r.Details, &ts); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		r.Status = Status(statusStr)
		r.LastChecked = time.Unix(ts, 0).UTC()
		out = append(out, r)
	}
	return out, rows.Err()
}

// Summary represents a per-slug rollup. Counts checks by status.
type Summary struct {
	Slug        string
	Pass        int
	Fail        int
	Error       int
	LastChecked time.Time // newest last_checked across the slug's checks
}

// Summarise rolls every slug up to pass/fail/error counts.
func (s *Store) Summarise(ctx context.Context) (map[string]Summary, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `
		SELECT slug, status, MAX(last_checked), COUNT(*)
		FROM compliance_check
		GROUP BY slug, status
	`)
	if err != nil {
		return nil, fmt.Errorf("summarise compliance_check: %w", err)
	}
	defer rows.Close()

	out := make(map[string]Summary)
	for rows.Next() {
		var (
			slug   string
			status string
			ts     sql.NullInt64
			count  int
		)
		if err := rows.Scan(&slug, &status, &ts, &count); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}

		summary := out[slug]
		summary.Slug = slug

		switch Status(status) {
		case StatusPass:
			summary.Pass += count
		case StatusFail:
			summary.Fail += count
		case StatusError:
			summary.Error += count
		}

		if ts.Valid {
			t := time.Unix(ts.Int64, 0).UTC()
			if t.After(summary.LastChecked) {
				summary.LastChecked = t
			}
		}

		out[slug] = summary
	}
	return out, rows.Err()
}

// ErrNotFound mirrors storage.ErrNotFound so callers don't need to
// import the storage package.
var ErrNotFound = errors.New("compliance: not found")
