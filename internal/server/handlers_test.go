package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleHealthz(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()

	h.HandleHealthz(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	body := w.Body.String()
	if !strings.Contains(body, `"status":"ok"`) {
		t.Errorf("body = %q, want to contain status:ok", body)
	}
	if !strings.Contains(body, `"service":"autopsy"`) {
		t.Errorf("body = %q, want to contain service:autopsy", body)
	}
}

func TestHandleUpload_WrongMethod(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/upload", nil)
	w := httptest.NewRecorder()

	h.HandleUpload(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
	}
}

func TestHandleUpload_NoFile(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/upload", nil)
	req.Header.Set("Content-Type", "multipart/form-data")
	w := httptest.NewRecorder()

	h.HandleUpload(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpload_WrongFileType(t *testing.T) {
	h := newTestHandler(t)

	body, contentType := makeMultipartFile(t, "test.pdf", []byte("not a tar.gz"))
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	h.HandleUpload(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d (wrong file type should be rejected)", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpload_FileTooLarge(t *testing.T) {
	h := newTestHandler(t)
	h.cfg.MaxBundleMB = 1 // 1MB limit for test

	// Create a body larger than 1MB
	bigContent := bytes.Repeat([]byte("x"), 2*1024*1024)
	body, contentType := makeMultipartFile(t, "bundle.tar.gz", bigContent)

	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	h.HandleUpload(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d (oversized file should be rejected)", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandleUpload_InvalidGzip(t *testing.T) {
	h := newTestHandler(t)

	// Valid .tar.gz extension but invalid content
	body, contentType := makeMultipartFile(t, "bundle.tar.gz", []byte("this is not a gzip file"))
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	h.HandleUpload(w, req)

	// Should fail at extraction stage
	if w.Code == http.StatusOK {
		t.Error("upload of invalid gzip should not return 200")
	}
}

func TestHandleUpload_ValidBundle(t *testing.T) {
	h := newTestHandler(t)

	// Use our test fixture bundle
	bundleData := makeTestBundle(t)
	body, contentType := makeMultipartFile(t, "support-bundle.tar.gz", bundleData)
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)
	w := httptest.NewRecorder()

	h.HandleUpload(w, req)

	// Should redirect to /report/{sessionID}
	if w.Code != http.StatusSeeOther && w.Code != http.StatusFound {
		// Also accept HTMX redirect header pattern
		if w.Header().Get("HX-Redirect") == "" {
			t.Errorf("valid upload: status = %d, expected redirect or HX-Redirect header", w.Code)
		}
	}
}

func TestHandleReport_UnknownSession(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/report/does-not-exist", nil)
	w := httptest.NewRecorder()

	// Set path value (Go 1.22 stdlib routing)
	req.SetPathValue("sessionID", "does-not-exist")
	h.HandleReport(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d for unknown session", w.Code, http.StatusNotFound)
	}
}

func TestHandleTriageSSE_UnknownSession(t *testing.T) {
	h := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/stream/does-not-exist/triage", nil)
	req.SetPathValue("sessionID", "does-not-exist")
	w := httptest.NewRecorder()

	h.HandleTriageSSE(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// newTestHandler creates a Handler configured for testing (stub mode, no API key).
func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	// Import your actual config and handler types here
	// This is a template — adjust to match your actual constructor signature
	cfg := testConfig()
	cfg.StubMode = true
	return NewHandler(cfg, nil) // nil client = stub mode
}

// makeMultipartFile creates a multipart form body with a single file field named "bundle".
func makeMultipartFile(t *testing.T, filename string, content []byte) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	part, err := writer.CreateFormFile("bundle", filename)
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("Write part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	return &body, writer.FormDataContentType()
}

// makeTestBundle creates a minimal valid .tar.gz in memory for upload tests.
func makeTestBundle(t *testing.T) []byte {
	t.Helper()
	// Reuse the helper from extract_test.go — same package
	return makeTarGz(t, map[string]string{
		"cluster-resources/nodes.json":          `[{"metadata":{"name":"node1"},"status":{"conditions":[{"type":"Ready","status":"True"}],"capacity":{"cpu":"4","memory":"8192Mi"}}}]`,
		"cluster-resources/default/pods.json":   `[{"metadata":{"name":"nginx","namespace":"default"},"status":{"phase":"Running","containerStatuses":[{"name":"nginx","ready":true,"restartCount":0,"state":{"running":{"startedAt":"2024-01-15T10:00:00Z"}}}]}}]`,
		"cluster-resources/default/events.json": `[]`,
		"cluster-info/cluster_version.json":     `{"info":{"gitVersion":"v1.28.4"},"string":"v1.28.4"}`,
	})
}
