package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"testing"
	"time"

	"github.com/yourusername/autopsy/internal/config"
)

// testConfig returns a Config suitable for unit tests: stub mode enabled,
// conservative limits, short TTL.
func testConfig() config.Config {
	return config.Config{
		Port:        "8080",
		StubMode:    true,
		MaxBundleMB: 250,
		SessionTTL:  30 * time.Minute,
	}
}

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
