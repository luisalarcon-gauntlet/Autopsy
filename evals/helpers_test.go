//go:build evals

package evals_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/yourusername/autopsy/internal/bundle"
)

// requireAPIKey skips the test if ANTHROPIC_API_KEY is not set.
func requireAPIKey(t *testing.T) {
	t.Helper()
	if os.Getenv("ANTHROPIC_API_KEY") == "" {
		t.Skip("ANTHROPIC_API_KEY not set — skipping eval")
	}
}

// newClient creates a real Anthropic client using the environment API key.
func newClient(t *testing.T) *anthropic.Client {
	t.Helper()
	client := anthropic.NewClient()
	return &client
}

// loadToyotaBundle parses the Toyota fixture bundle and returns BundleData.
func loadToyotaBundle(t *testing.T) *bundle.BundleData {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not determine test file path")
	}
	dir := filepath.Join(filepath.Dir(file), "testdata", "toyota")
	data, err := bundle.Parse(context.Background(), dir)
	if err != nil {
		t.Fatalf("loadToyotaBundle: %v", err)
	}
	return data
}

// logResult logs a labelled value for human review of eval output.
func logResult(t *testing.T, label, value string) {
	t.Helper()
	t.Logf("[eval] %s:\n%s", label, value)
}
