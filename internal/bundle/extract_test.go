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

// makeTarGz creates an in-memory tar.gz archive from the provided files map
// (name → content).
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	for name, content := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o640,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader %s: %v", name, err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatalf("Write %s: %v", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		t.Fatalf("tw.Close: %v", err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gw.Close: %v", err)
	}
	return buf.Bytes()
}

func TestExtract_ValidBundle(t *testing.T) {
	files := map[string]string{
		"cluster-resources/pods.json":       `{"items":[]}`,
		"cluster-resources/nodes.json":      `{"items":[]}`,
		"logs/default/nginx/nginx.log":      "2024-01-01 INFO started\n",
	}
	data := makeTarGz(t, files)

	dir, err := Extract(context.Background(), bytes.NewReader(data), MaxTotalSizeBytes)
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	defer os.RemoveAll(dir)

	for name := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected file %s to exist: %v", p, err)
		}
	}
}

func TestExtract_PathTraversal(t *testing.T) {
	tests := []struct {
		name    string
		tarPath string
	}{
		{"dotdot prefix", "../../../../etc/passwd"},
		{"dotdot in middle", "cluster-resources/../../../etc/shadow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			gw := gzip.NewWriter(&buf)
			tw := tar.NewWriter(gw)

			content := "evil content"
			hdr := &tar.Header{
				Name:     tt.tarPath,
				Mode:     0o640,
				Size:     int64(len(content)),
				Typeflag: tar.TypeReg,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatalf("WriteHeader: %v", err)
			}
			tw.Write([]byte(content))
			tw.Close()
			gw.Close()

			dir, err := Extract(context.Background(), bytes.NewReader(buf.Bytes()), MaxTotalSizeBytes)
			// Extract may succeed (the malicious entry is skipped) or fail.
			// The important thing is that /etc/passwd (or equiv) was NOT written.
			if dir != "" {
				defer os.RemoveAll(dir)
				// Verify the evil path was not written under the temp dir.
				etcPath := filepath.Join(dir, "etc", "passwd")
				if _, serr := os.Stat(etcPath); serr == nil {
					t.Errorf("path traversal succeeded: %s was created", etcPath)
				}
				_ = err
			}
		})
	}
}

func TestExtract_FileSizeLimit(t *testing.T) {
	// Create a single file that exceeds MaxFileSizeBytes.
	bigContent := strings.Repeat("a", int(MaxFileSizeBytes)+10)

	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	hdr := &tar.Header{
		Name:     "big-file.txt",
		Mode:     0o640,
		Size:     int64(len(bigContent)),
		Typeflag: tar.TypeReg,
	}
	tw.WriteHeader(hdr)
	tw.Write([]byte(bigContent))
	tw.Close()
	gw.Close()

	dir, err := Extract(context.Background(), bytes.NewReader(buf.Bytes()), MaxTotalSizeBytes)
	if dir != "" {
		os.RemoveAll(dir)
	}
	if err == nil {
		t.Error("expected error for file exceeding size limit, got nil")
	}
}

func TestExtract_TotalSizeLimit(t *testing.T) {
	// Max is set to 100 bytes; create two 60-byte files.
	files := map[string]string{
		"file1.txt": strings.Repeat("a", 60),
		"file2.txt": strings.Repeat("b", 60),
	}
	data := makeTarGz(t, files)

	dir, err := Extract(context.Background(), bytes.NewReader(data), 100)
	if dir != "" {
		os.RemoveAll(dir)
	}
	if err == nil {
		t.Error("expected error for total size exceeding limit, got nil")
	}
}

func TestExtract_ContextCancellation(t *testing.T) {
	files := map[string]string{
		"a.txt": strings.Repeat("x", 1000),
		"b.txt": strings.Repeat("y", 1000),
	}
	data := makeTarGz(t, files)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	dir, err := Extract(ctx, bytes.NewReader(data), MaxTotalSizeBytes)
	if dir != "" {
		os.RemoveAll(dir)
	}
	if err == nil {
		t.Error("expected error when context is already cancelled, got nil")
	}
}

func TestExtract_EmptyArchive(t *testing.T) {
	data := makeTarGz(t, map[string]string{})

	dir, err := Extract(context.Background(), bytes.NewReader(data), MaxTotalSizeBytes)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil for empty archive", err)
	}
	defer os.RemoveAll(dir)

	if dir == "" {
		t.Error("expected non-empty dir path")
	}
}

func TestSanitizePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantOut  string
	}{
		{"normal path", "cluster-resources/pods.json", false, ""},
		{"leading slash", "/etc/passwd", true, ""},
		{"dotdot start", "../../etc/passwd", true, ""},
		{"dotdot middle", "a/../../../etc/passwd", true, ""},
		{"clean path", "a/b/c.json", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := sanitizePath(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("sanitizePath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && out == "" {
				t.Errorf("sanitizePath(%q) returned empty string without error", tt.input)
			}
		})
	}
}
