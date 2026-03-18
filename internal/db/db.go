// Package db provides PostgreSQL persistence for Autopsy bundles and analyses.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// DB wraps a *sql.DB connection pool opened with the pgx driver.
type DB struct {
	*sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS bundles (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    customer_name TEXT,
    filename TEXT NOT NULL,
    file_size_bytes INTEGER,
    sha256 TEXT,
    uploaded_by TEXT,
    file_data BYTEA,
    uploaded_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS analyses (
    id TEXT PRIMARY KEY,
    bundle_id TEXT REFERENCES bundles(id),
    severity_score INTEGER,
    cluster_health TEXT,
    triage_json TEXT,
    timeline_json TEXT,
    rca_markdown TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS chat_messages (
    id TEXT PRIMARY KEY,
    bundle_id TEXT REFERENCES bundles(id),
    role TEXT NOT NULL,
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
`

// Open opens a connection pool to the given PostgreSQL DATABASE_URL and pings it.
func Open(databaseURL string) (*DB, error) {
	sqlDB, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("db open: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return &DB{sqlDB}, nil
}

// Migrate creates tables if they don't already exist. Safe to call on every startup.
func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return fmt.Errorf("db migrate: %w", err)
	}
	slog.Info("db schema ready")
	return nil
}
