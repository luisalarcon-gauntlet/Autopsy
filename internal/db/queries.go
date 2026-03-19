package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Bundle is a row from the bundles table, including file_data.
type Bundle struct {
	ID            string
	OrgID         string
	CustomerName  string
	Filename      string
	FileSizeBytes int64
	SHA256        string
	UploadedBy    string
	FileData      []byte
	UploadedAt    time.Time
}

// BundleListItem is a lightweight bundle row joined with analysis data,
// used for the bundle history page.
type BundleListItem struct {
	ID            string
	CustomerName  string
	Filename      string
	FileSizeBytes int64
	SeverityScore int
	ClusterHealth string
	UploadedAt    time.Time
}

// FormatSize returns a human-readable file size string.
func (b BundleListItem) FormatSize() string {
	if b.FileSizeBytes < 1024*1024 {
		return fmt.Sprintf("%d KB", b.FileSizeBytes/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b.FileSizeBytes)/(1024*1024))
}

// FormatDate returns a short human-readable upload date.
func (b BundleListItem) FormatDate() string {
	return b.UploadedAt.Format("Jan 2, 2006")
}

// Analysis is a row from the analyses table.
type Analysis struct {
	ID            string
	BundleID      string
	SeverityScore int
	ClusterHealth string
	TriageJSON    string
	TimelineJSON  string
	RCAMarkdown   string
	CreatedAt     time.Time
}

// ChatMessage is a row from the chat_messages table.
type ChatMessage struct {
	ID        string
	BundleID  string
	Role      string
	Content   string
	CreatedAt time.Time
}

// InboxItem is a bundle row for the platform inbox, joined with analysis data.
type InboxItem struct {
	ID           string
	OrgID        string // ISV identifier, e.g. "airbyte"
	CustomerName string
	SeverityScore int
	HasAnalysis  bool
	TopIssue     string
	Status       string // "new" | "in_review" | "resolved"
	UploadedAt   time.Time
}

// ISVDisplay returns a display-friendly version of the org ID ("airbyte" → "Airbyte").
func (i InboxItem) ISVDisplay() string {
	if i.OrgID == "" {
		return "Unknown"
	}
	return strings.ToUpper(i.OrgID[:1]) + i.OrgID[1:]
}

// IsNew reports whether the bundle was uploaded in the last 2 hours.
func (i InboxItem) IsNew() bool {
	return time.Since(i.UploadedAt) < 2*time.Hour
}

// Age returns a human-readable time-since string, e.g. "5m ago", "3h ago", "Jan 2, 2024".
func (i InboxItem) Age() string {
	d := time.Since(i.UploadedAt)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return i.UploadedAt.Format("Jan 2, 2006")
	}
}

// ISVTab is one entry in the platform inbox ISV filter bar.
type ISVTab struct {
	OrgID       string
	DisplayName string // title-cased OrgID
	OpenCount   int    // non-resolved bundle count
}

// InsertBundle inserts a new bundle record including its raw file bytes.
func (db *DB) InsertBundle(ctx context.Context, b Bundle) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO bundles (id, org_id, customer_name, filename, file_size_bytes, sha256, uploaded_by, file_data)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		b.ID, b.OrgID, nullStr(b.CustomerName), b.Filename,
		b.FileSizeBytes, b.SHA256, b.UploadedBy, b.FileData,
	)
	return err
}

// GetBundleBySHA256 returns the most recent bundle with the given SHA256 for an org,
// including file_data (used for session reconstruction on dedup).
func (db *DB) GetBundleBySHA256(ctx context.Context, sha256, orgID string) (*Bundle, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, org_id, customer_name, filename, file_size_bytes, sha256, uploaded_by, file_data, uploaded_at
		 FROM bundles WHERE sha256 = $1 AND org_id = $2 ORDER BY uploaded_at DESC LIMIT 1`,
		sha256, orgID,
	)
	return scanBundle(row)
}

// GetBundleByID returns a bundle by ID, verifying org ownership. Returns nil, nil if not found.
func (db *DB) GetBundleByID(ctx context.Context, id, orgID string) (*Bundle, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, org_id, customer_name, filename, file_size_bytes, sha256, uploaded_by, file_data, uploaded_at
		 FROM bundles WHERE id = $1 AND org_id = $2`,
		id, orgID,
	)
	return scanBundle(row)
}

// GetBundlesByOrg returns bundle list items for an org, most recent first,
// joined with analysis data for display.
func (db *DB) GetBundlesByOrg(ctx context.Context, orgID string) ([]BundleListItem, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT b.id, b.customer_name, b.filename, b.file_size_bytes,
		        COALESCE(a.severity_score, 0), COALESCE(a.cluster_health, ''),
		        b.uploaded_at
		 FROM bundles b
		 LEFT JOIN analyses a ON b.id = a.bundle_id
		 WHERE b.org_id = $1
		 ORDER BY b.uploaded_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []BundleListItem
	for rows.Next() {
		var item BundleListItem
		var customerName, clusterHealth sql.NullString
		if err := rows.Scan(
			&item.ID, &customerName, &item.Filename, &item.FileSizeBytes,
			&item.SeverityScore, &clusterHealth, &item.UploadedAt,
		); err != nil {
			return nil, err
		}
		item.CustomerName = customerName.String
		item.ClusterHealth = clusterHealth.String
		items = append(items, item)
	}
	return items, rows.Err()
}

// SaveTriage upserts triage analysis data for a bundle (analyses.id = bundle_id).
func (db *DB) SaveTriage(ctx context.Context, bundleID string, severityScore int, clusterHealth, triageJSON string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO analyses (id, bundle_id, severity_score, cluster_health, triage_json)
		 VALUES ($1, $1, $2, $3, $4)
		 ON CONFLICT (id) DO UPDATE SET
		     severity_score = $2,
		     cluster_health = $3,
		     triage_json    = $4`,
		bundleID, severityScore, clusterHealth, triageJSON,
	)
	return err
}

// SaveTimeline upserts timeline analysis data for a bundle.
func (db *DB) SaveTimeline(ctx context.Context, bundleID, timelineJSON string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO analyses (id, bundle_id, timeline_json)
		 VALUES ($1, $1, $2)
		 ON CONFLICT (id) DO UPDATE SET timeline_json = $2`,
		bundleID, timelineJSON,
	)
	return err
}

// SaveRCA upserts the RCA markdown for a bundle.
func (db *DB) SaveRCA(ctx context.Context, bundleID, rcaMarkdown string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO analyses (id, bundle_id, rca_markdown)
		 VALUES ($1, $1, $2)
		 ON CONFLICT (id) DO UPDATE SET rca_markdown = $2`,
		bundleID, rcaMarkdown,
	)
	return err
}

// GetAnalysisByBundleID returns the stored analysis for a bundle, or nil if none.
func (db *DB) GetAnalysisByBundleID(ctx context.Context, bundleID string) (*Analysis, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, bundle_id, severity_score, cluster_health,
		        triage_json, timeline_json, rca_markdown, created_at
		 FROM analyses WHERE bundle_id = $1`,
		bundleID,
	)
	var a Analysis
	var clusterHealth, triageJSON, timelineJSON, rcaMarkdown sql.NullString
	err := row.Scan(
		&a.ID, &a.BundleID, &a.SeverityScore, &clusterHealth,
		&triageJSON, &timelineJSON, &rcaMarkdown, &a.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.ClusterHealth = clusterHealth.String
	a.TriageJSON = triageJSON.String
	a.TimelineJSON = timelineJSON.String
	a.RCAMarkdown = rcaMarkdown.String
	return &a, nil
}

// InsertChatMessage saves a single chat message.
func (db *DB) InsertChatMessage(ctx context.Context, m ChatMessage) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO chat_messages (id, bundle_id, role, content) VALUES ($1, $2, $3, $4)`,
		m.ID, m.BundleID, m.Role, m.Content,
	)
	return err
}

// GetChatMessagesByBundleID returns all chat messages for a bundle, oldest first.
func (db *DB) GetChatMessagesByBundleID(ctx context.Context, bundleID string) ([]ChatMessage, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, bundle_id, role, content, created_at
		 FROM chat_messages WHERE bundle_id = $1 ORDER BY created_at ASC`,
		bundleID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []ChatMessage
	for rows.Next() {
		var m ChatMessage
		if err := rows.Scan(&m.ID, &m.BundleID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// CustomerBundleItem is a bundle row joined with analysis data for the customer detail page.
type CustomerBundleItem struct {
	ID            string
	CustomerName  string
	Filename      string
	UploadedAt    time.Time
	SeverityScore int
	ClusterHealth string
	TopIssue      string
	HasAnalysis   bool
}

// FormatDate returns a short human-readable upload date.
func (b CustomerBundleItem) FormatDate() string { return b.UploadedAt.Format("Jan 2, 2006") }

// SeverityLabel returns "critical", "warning", or "healthy" based on score.
func (b CustomerBundleItem) SeverityLabel() string {
	if b.SeverityScore >= 70 {
		return "critical"
	} else if b.SeverityScore >= 40 {
		return "warning"
	}
	return "healthy"
}

// GetBundlesByCustomer returns bundles for a customer identified by URL slug
// (lowercase name with spaces as hyphens), most recent first.
func (db *DB) GetBundlesByCustomer(ctx context.Context, orgID, slug string) ([]CustomerBundleItem, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT b.id, b.customer_name, b.filename, b.uploaded_at,
		        COALESCE(a.severity_score, 0), COALESCE(a.cluster_health, ''),
		        COALESCE(a.triage_json, ''), a.id IS NOT NULL
		 FROM bundles b
		 LEFT JOIN analyses a ON b.id = a.bundle_id
		 WHERE b.org_id = $1 AND LOWER(REPLACE(b.customer_name, ' ', '-')) = $2
		 ORDER BY b.uploaded_at DESC`,
		orgID, slug,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CustomerBundleItem
	for rows.Next() {
		var item CustomerBundleItem
		var customerName, clusterHealth, triageJSON sql.NullString
		var hasAnalysis bool
		if err := rows.Scan(
			&item.ID, &customerName, &item.Filename, &item.UploadedAt,
			&item.SeverityScore, &clusterHealth, &triageJSON, &hasAnalysis,
		); err != nil {
			return nil, err
		}
		item.CustomerName = customerName.String
		item.ClusterHealth = clusterHealth.String
		item.HasAnalysis = hasAnalysis
		if triageJSON.Valid && triageJSON.String != "" {
			item.TopIssue = extractTopIssue(triageJSON.String)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetDistinctCustomers returns distinct customer names for an org, sorted alphabetically.
func (db *DB) GetDistinctCustomers(ctx context.Context, orgID string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT DISTINCT customer_name FROM bundles
		 WHERE org_id = $1 AND customer_name IS NOT NULL AND customer_name <> ''
		 ORDER BY customer_name`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

// InsertBundleWithTime inserts a bundle with an explicit uploaded_at timestamp.
// If a bundle with the same id already exists, it is skipped (used for seeding).
func (db *DB) InsertBundleWithTime(ctx context.Context, b Bundle) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO bundles (id, org_id, customer_name, filename, file_size_bytes, sha256, uploaded_by, file_data, uploaded_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		 ON CONFLICT (id) DO NOTHING`,
		b.ID, b.OrgID, nullStr(b.CustomerName), b.Filename,
		b.FileSizeBytes, b.SHA256, b.UploadedBy, b.FileData, b.UploadedAt,
	)
	return err
}

// GetPlatformInbox returns all bundles across all orgs joined with analysis data,
// ordered by uploaded_at DESC. Used for the Marcus/Replicated platform dashboard.
func (db *DB) GetPlatformInbox(ctx context.Context) ([]InboxItem, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT b.id, b.org_id, COALESCE(b.customer_name, ''),
		        COALESCE(a.severity_score, 0),
		        COALESCE(a.triage_json, ''),
		        a.id IS NOT NULL,
		        b.status, b.uploaded_at
		 FROM bundles b
		 LEFT JOIN analyses a ON b.id = a.bundle_id
		 ORDER BY b.uploaded_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []InboxItem
	for rows.Next() {
		var item InboxItem
		var triageJSON string
		if err := rows.Scan(
			&item.ID, &item.OrgID, &item.CustomerName,
			&item.SeverityScore, &triageJSON,
			&item.HasAnalysis, &item.Status, &item.UploadedAt,
		); err != nil {
			return nil, err
		}
		if triageJSON != "" {
			item.TopIssue = extractTopIssue(triageJSON)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetPlatformISVTabs returns one tab entry per distinct org in the bundles table,
// with the count of open (non-resolved) bundles for each.
func (db *DB) GetPlatformISVTabs(ctx context.Context) ([]ISVTab, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT org_id,
		        COUNT(*) FILTER (WHERE status != 'resolved') AS open_count
		 FROM bundles
		 GROUP BY org_id
		 ORDER BY org_id`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tabs []ISVTab
	for rows.Next() {
		var t ISVTab
		if err := rows.Scan(&t.OrgID, &t.OpenCount); err != nil {
			return nil, err
		}
		if len(t.OrgID) > 0 {
			t.DisplayName = strings.ToUpper(t.OrgID[:1]) + t.OrgID[1:]
		}
		tabs = append(tabs, t)
	}
	return tabs, rows.Err()
}

// GetBundleStatus returns the current status of a bundle.
func (db *DB) GetBundleStatus(ctx context.Context, bundleID string) (string, error) {
	var status string
	err := db.QueryRowContext(ctx,
		`SELECT status FROM bundles WHERE id = $1`, bundleID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return status, err
}

// SetBundleStatus updates the workflow status for a bundle.
// Valid values: "new", "in_review", "resolved".
func (db *DB) SetBundleStatus(ctx context.Context, bundleID, status string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE bundles SET status = $1 WHERE id = $2`, status, bundleID,
	)
	return err
}

// GetBundleByIDGlobal returns a bundle by ID without org ownership check.
// Used by platform users who can view bundles across all ISV orgs.
func (db *DB) GetBundleByIDGlobal(ctx context.Context, id string) (*Bundle, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, org_id, customer_name, filename, file_size_bytes, sha256, uploaded_by, file_data, uploaded_at
		 FROM bundles WHERE id = $1`, id,
	)
	return scanBundle(row)
}

// extractTopIssue parses triage JSON and returns the title of the first top issue.
func extractTopIssue(triageJSON string) string {
	var result struct {
		TopIssues []struct {
			Title string `json:"title"`
		} `json:"topIssues"`
	}
	if err := json.Unmarshal([]byte(triageJSON), &result); err != nil {
		return ""
	}
	if len(result.TopIssues) > 0 {
		return result.TopIssues[0].Title
	}
	return ""
}

// CustomerSummaryRow holds the latest analysis data per customer for an org.
type CustomerSummaryRow struct {
	CustomerName  string
	SeverityScore int
	ClusterHealth string
}

// GetCustomerSummaries returns one row per customer for an org using the most
// recent bundle's analysis data, ordered alphabetically by customer name.
func (db *DB) GetCustomerSummaries(ctx context.Context, orgID string) ([]CustomerSummaryRow, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT DISTINCT ON (b.customer_name)
		        b.customer_name,
		        COALESCE(a.severity_score, 0),
		        COALESCE(a.cluster_health, '')
		 FROM bundles b
		 LEFT JOIN analyses a ON b.id = a.bundle_id
		 WHERE b.org_id = $1 AND b.customer_name IS NOT NULL AND b.customer_name <> ''
		 ORDER BY b.customer_name, b.uploaded_at DESC`,
		orgID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []CustomerSummaryRow
	for rows.Next() {
		var item CustomerSummaryRow
		if err := rows.Scan(&item.CustomerName, &item.SeverityScore, &item.ClusterHealth); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ── helpers ──────────────────────────────────────────────────────────────────

func nullStr(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

func scanBundle(row *sql.Row) (*Bundle, error) {
	var b Bundle
	var customerName sql.NullString
	err := row.Scan(
		&b.ID, &b.OrgID, &customerName, &b.Filename,
		&b.FileSizeBytes, &b.SHA256, &b.UploadedBy, &b.FileData, &b.UploadedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	b.CustomerName = customerName.String
	return &b, nil
}
