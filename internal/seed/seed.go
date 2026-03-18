// Package seed inserts demo bundles and analyses for the Airbyte org
// so the customer detail trend graphs have data on first boot.
package seed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/yourusername/autopsy/internal/db"
)

type seedEntry struct {
	id            string
	customerName  string
	filename      string
	uploadedAt    time.Time
	severity      int
	clusterHealth string
	topIssue      string
}

var entries = []seedEntry{
	// Toyota — worsening trend
	{"seed-toyota-001", "Toyota", "toyota-cluster-2024-01-01.tar.gz", date("2024-01-01"), 34, "healthy", "Minor memory pressure on worker pods"},
	{"seed-toyota-002", "Toyota", "toyota-cluster-2024-01-08.tar.gz", date("2024-01-08"), 61, "warning", "CrashLoopBackOff on airbyte-worker, 3 restarts"},
	{"seed-toyota-003", "Toyota", "toyota-cluster-2024-01-15.tar.gz", date("2024-01-15"), 85, "critical", "OOMKilled memory-hog, 14 restarts — critical"},
	// Nike — stable and healthy
	{"seed-nike-001", "Nike", "nike-cluster-2024-01-05.tar.gz", date("2024-01-05"), 15, "healthy", ""},
	{"seed-nike-002", "Nike", "nike-cluster-2024-01-10.tar.gz", date("2024-01-10"), 12, "healthy", ""},
	{"seed-nike-003", "Nike", "nike-cluster-2024-01-15.tar.gz", date("2024-01-15"), 12, "healthy", ""},
	// Goldman Sachs — improving trend
	{"seed-goldman-001", "Goldman Sachs", "goldman-cluster-2024-01-03.tar.gz", date("2024-01-03"), 71, "critical", "PostgreSQL connection pool exhausted"},
	{"seed-goldman-002", "Goldman Sachs", "goldman-cluster-2024-01-10.tar.gz", date("2024-01-10"), 52, "warning", "Elevated error rate on api-server"},
	{"seed-goldman-003", "Goldman Sachs", "goldman-cluster-2024-01-15.tar.gz", date("2024-01-15"), 44, "warning", "Memory pressure on 2 nodes — monitoring"},
}

// Run seeds demo bundles for the airbyte org. Already-seeded entries are skipped.
func Run(ctx context.Context, d *db.DB) {
	inserted := 0
	for _, e := range entries {
		if err := seedOne(ctx, d, e); err != nil {
			slog.Warn("seed: failed", "id", e.id, "err", err)
		} else {
			inserted++
		}
	}
	slog.Info("seed: demo data ready", "entries", len(entries), "inserted", inserted)
}

func seedOne(ctx context.Context, d *db.DB, e seedEntry) error {
	// Skip if already present.
	existing, err := d.GetBundleByID(ctx, e.id, "airbyte")
	if err != nil {
		return fmt.Errorf("check: %w", err)
	}
	if existing != nil {
		return nil
	}

	// Build minimal triage JSON matching analysis.TriageResult structure.
	topIssues := []map[string]any{}
	if e.topIssue != "" {
		topIssues = append(topIssues, map[string]any{
			"title":       e.topIssue,
			"severity":    healthToIssueSeverity(e.clusterHealth),
			"affectedPod": "airbyte-worker",
			"category":    "unknown",
		})
	}
	triageJSON, err := json.Marshal(map[string]any{
		"severityScore":      e.severity,
		"clusterHealth":      e.clusterHealth,
		"summary":            fmt.Sprintf("Cluster health: %s. Severity score: %d.", e.clusterHealth, e.severity),
		"topIssues":          topIssues,
		"affectedNamespaces": []string{"airbyte"},
	})
	if err != nil {
		return fmt.Errorf("marshal triage: %w", err)
	}

	// Use deterministic SHA256 derived from the seed ID so dedup never fires.
	sum := sha256.Sum256([]byte(e.id))
	sha256hex := hex.EncodeToString(sum[:])

	if err := d.InsertBundleWithTime(ctx, db.Bundle{
		ID:            e.id,
		OrgID:         "airbyte",
		CustomerName:  e.customerName,
		Filename:      e.filename,
		FileSizeBytes: 0,
		SHA256:        sha256hex,
		UploadedBy:    "seed",
		FileData:      []byte{},
		UploadedAt:    e.uploadedAt,
	}); err != nil {
		return fmt.Errorf("insert bundle: %w", err)
	}

	if err := d.SaveTriage(ctx, e.id, e.severity, e.clusterHealth, string(triageJSON)); err != nil {
		return fmt.Errorf("insert analysis: %w", err)
	}

	return nil
}

func date(s string) time.Time {
	t, _ := time.Parse("2006-01-02", s)
	return t
}

func healthToIssueSeverity(health string) string {
	switch health {
	case "critical":
		return "critical"
	case "warning":
		return "high"
	default:
		return "low"
	}
}
