package drift

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/MeKo-Tech/ewws-platform-ui/internal/storage"
)

// Component is one workload of an app. The values mirror the
// `images.<component>` keys in the registry schema.
type Component string

const (
	ComponentBackend  Component = "backend"
	ComponentFrontend Component = "frontend"
)

// Snapshot is one row of `drift_snapshot`. One per (slug, component).
type Snapshot struct {
	Slug         string
	Component    Component
	StagingTag   string
	ProdTag      string
	CommitsAhead int
	CollectedAt  time.Time
}

// Store wraps the SQLite handle with typed helpers.
type Store struct {
	db *storage.DB
}

// NewStore returns a Store backed by the given DB.
func NewStore(db *storage.DB) *Store {
	return &Store{db: db}
}

// Upsert writes (or replaces) one (slug, component) row.
func (s *Store) Upsert(ctx context.Context, snap Snapshot) error {
	_, err := s.db.SQL().ExecContext(ctx, `
		INSERT INTO drift_snapshot (slug, component, staging_tag, prod_tag, commits_ahead, collected_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(slug, component) DO UPDATE SET
			staging_tag   = excluded.staging_tag,
			prod_tag      = excluded.prod_tag,
			commits_ahead = excluded.commits_ahead,
			collected_at  = excluded.collected_at
	`,
		snap.Slug, string(snap.Component),
		snap.StagingTag, snap.ProdTag,
		snap.CommitsAhead, snap.CollectedAt.Unix(),
	)
	if err != nil {
		return fmt.Errorf("upsert drift_snapshot: %w", err)
	}

	return nil
}

// Get returns the snapshot for one (slug, component), or ErrNotFound.
func (s *Store) Get(ctx context.Context, slug string, component Component) (*Snapshot, error) {
	row := s.db.SQL().QueryRowContext(ctx, `
		SELECT slug, component, staging_tag, prod_tag, commits_ahead, collected_at
		FROM drift_snapshot
		WHERE slug = ? AND component = ?
	`, slug, string(component))

	snap, err := scanSnapshot(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}

	if err != nil {
		return nil, fmt.Errorf("get drift_snapshot: %w", err)
	}

	return snap, nil
}

// ListBySlug returns every drift row stored for one slug.
func (s *Store) ListBySlug(ctx context.Context, slug string) ([]Snapshot, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `
		SELECT slug, component, staging_tag, prod_tag, commits_ahead, collected_at
		FROM drift_snapshot
		WHERE slug = ?
		ORDER BY component
	`, slug)
	if err != nil {
		return nil, fmt.Errorf("query drift_snapshot: %w", err)
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

// List returns every drift snapshot across every slug.
func (s *Store) List(ctx context.Context) ([]Snapshot, error) {
	rows, err := s.db.SQL().QueryContext(ctx, `
		SELECT slug, component, staging_tag, prod_tag, commits_ahead, collected_at
		FROM drift_snapshot
		ORDER BY slug, component
	`)
	if err != nil {
		return nil, fmt.Errorf("list drift_snapshot: %w", err)
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
		component   string
		collectedAt int64
	)

	err := s.Scan(
		&snap.Slug, &component,
		&snap.StagingTag, &snap.ProdTag,
		&snap.CommitsAhead, &collectedAt,
	)
	if err != nil {
		return nil, err
	}

	snap.Component = Component(component)
	snap.CollectedAt = time.Unix(collectedAt, 0).UTC()

	return &snap, nil
}

// ErrNotFound is returned when a (slug, component) snapshot is missing.
var ErrNotFound = errors.New("drift: not found")
