// Package storage provides a tiny SQLite-backed key/value-ish store for
// compliance scan results. Single-writer (the scanner) + many readers
// (HTTP handlers); SQLite's WAL mode handles the concurrency without
// us needing a process-level mutex.
package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go driver, no CGo (distroless-friendly)
)

// DB wraps a SQL handle plus the schema migration state. Safe for use
// from multiple goroutines (SQLite locks at the file level).
type DB struct {
	sql *sql.DB
}

// Open returns a DB at the given path, creating + migrating it as needed.
// `path` may be a file path or an in-memory DSN (`:memory:` for tests).
func Open(path string) (*DB, error) {
	dsn := path
	if path != ":memory:" {
		// _journal_mode=WAL: concurrent readers don't block the writer.
		// _busy_timeout=5000: wait up to 5s if the file is locked.
		dsn = fmt.Sprintf(
			"file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
			filepath.Clean(path),
		)
	}

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	sqlDB.SetMaxOpenConns(4)

	db := &DB{sql: sqlDB}
	if err := db.migrate(); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

// Close flushes pending writes and closes the underlying handle.
func (db *DB) Close() error {
	if db == nil || db.sql == nil {
		return nil
	}

	return db.sql.Close()
}

// SQL returns the raw handle. Useful for tests or one-off queries —
// production code should go through the typed helpers in the
// surrounding packages.
func (db *DB) SQL() *sql.DB {
	return db.sql
}

// migrate applies the schema. SQLite has no proper migration story; we
// rely on `CREATE TABLE IF NOT EXISTS` and additive `ALTER TABLE ADD
// COLUMN` for forward changes. Destructive changes need a new migration
// runner — we don't have any yet.
func (db *DB) migrate() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := db.sql.ExecContext(ctx, schema)

	return err
}

// schema is applied at every open. Keep statements idempotent.
const schema = `
CREATE TABLE IF NOT EXISTS compliance_check (
    slug          TEXT NOT NULL,
    repo          TEXT NOT NULL,          -- "owner/repo"
    check_name    TEXT NOT NULL,          -- e.g. "pr_title_workflow"
    status        TEXT NOT NULL,          -- "pass" | "fail" | "error"
    details       TEXT NOT NULL DEFAULT '', -- human-readable hint when failing
    last_checked  INTEGER NOT NULL,       -- unix seconds
    PRIMARY KEY (slug, check_name)
);

CREATE INDEX IF NOT EXISTS idx_compliance_check_repo ON compliance_check(repo);

CREATE TABLE IF NOT EXISTS metrics_snapshot (
    slug                 TEXT NOT NULL,
    stage                TEXT NOT NULL,
    requests_24h         INTEGER NOT NULL DEFAULT 0,
    requests_7d          INTEGER NOT NULL DEFAULT 0,
    last_request_at      INTEGER,                       -- unix seconds, NULL if no traffic
    error_rate_5xx       REAL NOT NULL DEFAULT 0,
    restarts_24h         INTEGER NOT NULL DEFAULT 0,
    memory_used_bytes    INTEGER NOT NULL DEFAULT 0,
    memory_limit_bytes   INTEGER NOT NULL DEFAULT 0,
    cpu_used_millicores  INTEGER NOT NULL DEFAULT 0,
    cpu_limit_millicores INTEGER NOT NULL DEFAULT 0,
    sparkline_hourly     TEXT NOT NULL DEFAULT '',      -- comma-separated int64
    collected_at         INTEGER NOT NULL,
    PRIMARY KEY (slug, stage)
);

CREATE TABLE IF NOT EXISTS drift_snapshot (
    slug             TEXT NOT NULL,
    component        TEXT NOT NULL,
    staging_tag      TEXT NOT NULL DEFAULT '',
    prod_tag         TEXT NOT NULL DEFAULT '',
    commits_ahead    INTEGER NOT NULL DEFAULT 0,
    collected_at     INTEGER NOT NULL,
    PRIMARY KEY (slug, component)
);
`

// ErrNotFound is returned when a row is expected but missing.
var ErrNotFound = errors.New("storage: not found")
