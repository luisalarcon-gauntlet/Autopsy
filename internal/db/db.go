// Package db provides PostgreSQL persistence for Autopsy bundles and analyses.
package db

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

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

ALTER TABLE bundles ADD COLUMN IF NOT EXISTS status TEXT NOT NULL DEFAULT 'new';

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

// DBHealthResult holds the result of a connectivity and schema check.
type DBHealthResult struct {
	DBStatus      string
	DBMessage     string
	DBLatencyMS   int64
	SchemaStatus  string
	SchemaMessage string
}

// HealthCheck runs SELECT 1 to verify connectivity and measures round-trip
// latency, then queries pg_tables to confirm all three expected tables exist.
// All queries run under a 5-second timeout.
func (db *DB) HealthCheck(ctx context.Context) DBHealthResult {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	start := time.Now()
	var dummy int
	err := db.QueryRowContext(ctx, "SELECT 1").Scan(&dummy)
	latencyMS := time.Since(start).Milliseconds()

	if err != nil {
		return DBHealthResult{
			DBStatus:      "error",
			DBMessage:     "connection failed: " + err.Error(),
			DBLatencyMS:   latencyMS,
			SchemaStatus:  "error",
			SchemaMessage: "skipped — DB unavailable",
		}
	}

	rows, err := db.QueryContext(ctx, `SELECT tablename FROM pg_tables WHERE schemaname='public'`)
	if err != nil {
		return DBHealthResult{
			DBStatus:      "ok",
			DBMessage:     fmt.Sprintf("connected, latency %dms", latencyMS),
			DBLatencyMS:   latencyMS,
			SchemaStatus:  "error",
			SchemaMessage: "pg_tables query failed: " + err.Error(),
		}
	}
	defer rows.Close()

	tableSet := make(map[string]bool)
	for rows.Next() {
		var name string
		if rows.Scan(&name) == nil {
			tableSet[name] = true
		}
	}

	required := []string{"bundles", "analyses", "chat_messages"}
	var missing []string
	for _, t := range required {
		if !tableSet[t] {
			missing = append(missing, t)
		}
	}

	schemaStatus, schemaMsg := "ok", fmt.Sprintf("all tables present (%d found)", len(tableSet))
	if len(missing) > 0 {
		schemaStatus = "error"
		schemaMsg = "missing tables: " + strings.Join(missing, ", ")
	}

	return DBHealthResult{
		DBStatus:      "ok",
		DBMessage:     fmt.Sprintf("connected, %d tables found", len(tableSet)),
		DBLatencyMS:   latencyMS,
		SchemaStatus:  schemaStatus,
		SchemaMessage: schemaMsg,
	}
}
