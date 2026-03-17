// Package server provides the HTTP handlers and middleware for Autopsy.
package server

import (
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yourusername/autopsy/internal/config"
)

const (
	maxMemoryBytes = 32 << 20 // 32MB for multipart parsing in memory
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	cfg  config.Config
	tmpl *template.Template
}

// NewHandler creates a Handler with the given config and parsed templates.
func NewHandler(cfg config.Config, tmpl *template.Template) *Handler {
	return &Handler{cfg: cfg, tmpl: tmpl}
}

// HandleIndex serves the upload page.
func (h *Handler) HandleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data := map[string]any{
		"StubMode":    h.cfg.StubMode,
		"MaxBundleMB": h.cfg.MaxBundleMB,
	}
	if err := h.tmpl.ExecuteTemplate(w, "upload.html", data); err != nil {
		slog.Error("template execution failed", "template", "upload.html", "err", err)
	}
}

// HandleUpload accepts a multipart upload of a .tar.gz bundle.
// It enforces the MAX_BUNDLE_MB size limit and validates the file extension.
func (h *Handler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	maxBytes := h.cfg.MaxBundleMB * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxMemoryBytes); err != nil {
		if strings.Contains(err.Error(), "request body too large") || strings.Contains(err.Error(), "http: request body too large") {
			jsonError(w, "Bundle exceeds maximum allowed size", http.StatusRequestEntityTooLarge)
			return
		}
		jsonError(w, "Failed to parse upload: "+err.Error(), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("bundle")
	if err != nil {
		jsonError(w, "No file provided — field name must be 'bundle'", http.StatusBadRequest)
		return
	}
	defer file.Close()

	name := header.Filename
	if !strings.HasSuffix(name, ".tar.gz") && !strings.HasSuffix(name, ".tgz") {
		jsonError(w, "Invalid file type: only .tar.gz and .tgz bundles are accepted", http.StatusBadRequest)
		return
	}

	slog.Info("bundle upload received", "filename", name, "size_bytes", header.Size)

	// Placeholder response until S1.4 wires in extraction + session.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "received", "filename": name})
}

// HandleHealthz returns a simple health check response.
func (h *Handler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"autopsy"}`))
}

// jsonError writes a JSON error body with the given status code.
func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
