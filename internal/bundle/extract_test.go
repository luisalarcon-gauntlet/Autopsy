package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTarGz builds an in-memory .tar.gz from a map of filename→content.
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Write %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

func TestExtract_ValidBundle(t *testing.T) {
	files := map[string]string{
		"cluster-resources/nodes.json":             `[{"metadata":{"name":"node1"}}]`,
		"cluster-resources/default/pods.json":      `[{"metadata":{"name":"pod1","namespace":"default"}}]`,
		"cluster-resources/default/events.json":    `[]`,
		"logs/default/pod1/container1.log":         "some log output\n",
		"cluster-info/cluster_version.json":        `{"info":{"gitVersion":"v1.28.4"},"string":"v1.28.4"}`,
	}

	data := makeTarGz(t, files)
	dir, err := Extract(context.Background(), bytes.NewReader(data), 100<<20)
	if err != nil {
		t.Fatalf("Extract() unexpected error: %v", err)
	}
	defer os.RemoveAll(dir)

	// Verify all files were extracted
	for name := range files {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %q not found in extracted dir", name)
		}
	}
}

func TestExtract_EmptyArchive(t *testing.T) {
	data := makeTarGz(t, map[string]string{})
	dir, err := Extract(context.Background(), bytes.NewReader(data), 100<<20)
	if err != nil {
		t.Fatalf("Extract() should succeed on empty archive, got: %v", err)
	}
	defer os.RemoveAll(dir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected empty dir, got %d entries", len(entries))
	}
}

func TestExtract_PathTraversal(t *testing.T) {
	tests := []struct {
		name     string
		filename string
	}{
		{"dotdot prefix", "../../etc/passwd"},
		{"dotdot in middle", "cluster-resources/../../etc/passwd"},
		{"absolute path", "/etc/passwd"},
		{"dotdot after valid", "logs/../../../etc/shadow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := map[string]string{
				tt.filename:                         "malicious content",
				"cluster-resources/nodes.json":      `[]`, // one legit file
			}
			data := makeTarGz(t, files)
			dir, err := Extract(context.Background(), bytes.NewReader(data), 100<<20)
			// Should NOT return an error — malicious files are skipped, not fatal
			if err != nil {
				t.Fatalf("Extract() unexpected error: %v", err)
			}
			defer os.RemoveAll(dir)

			// The traversal file must NOT exist outside the temp dir
			// Check that no file named "passwd" or "shadow" escaped
			escaped := filepath.Join(dir, "..", "etc", "passwd")
			if _, statErr := os.Stat(escaped); !os.IsNotExist(statErr) {
				t.Errorf("path traversal succeeded: file exists at %s", escaped)
			}

			// The legit file must still be there
			legit := filepath.Join(dir, "cluster-resources", "nodes.json")
			if _, statErr := os.Stat(legit); os.IsNotExist(statErr) {
				t.Errorf("legitimate file was not extracted")
			}
		})
	}
}

func TestExtract_FileSizeLimit(t *testing.T) {
	// Build a file that exceeds the per-file limit (50MB)
	bigContent := strings.Repeat("x", 51*1024*1024) // 51MB
	files := map[string]string{
		"logs/default/pod1/container1.log": bigContent,
	}
	data := makeTarGz(t, files)
	_, err := Extract(context.Background(), bytes.NewReader(data), 200<<20)
	if err == nil {
		t.Error("Extract() should return error for file exceeding 50MB limit")
	}
}

func TestExtract_ContextCancellation(t *testing.T) {
	// Build a valid bundle
	files := map[string]string{
		"cluster-resources/nodes.json": `[]`,
		"cluster-resources/default/pods.json": `[]`,
	}
	data := makeTarGz(t, files)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := Extract(ctx, bytes.NewReader(data), 100<<20)
	if err == nil {
		t.Error("Extract() should return error on cancelled context")
	}
}

func TestExtract_InvalidGzip(t *testing.T) {
	_, err := Extract(context.Background(), strings.NewReader("not a gzip file"), 100<<20)
	if err == nil {
		t.Error("Extract() should return error for invalid gzip data")
	}
}

func TestExtract_TempDirCreated(t *testing.T) {
	files := map[string]string{
		"cluster-resources/nodes.json": `[]`,
	}
	data := makeTarGz(t, files)

	dir, err := Extract(context.Background(), bytes.NewReader(data), 100<<20)
	if err != nil {
		t.Fatalf("Extract() unexpected error: %v", err)
	}

	// Dir must exist and be a directory
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("temp dir %q does not exist: %v", dir, err)
	}
	if !info.IsDir() {
		t.Errorf("expected %q to be a directory", dir)
	}

	// Cleanup
	if err := os.RemoveAll(dir); err != nil {
		t.Errorf("cleanup failed: %v", err)
	}

	// Dir must no longer exist after cleanup
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("temp dir still exists after RemoveAll")
	}
}
