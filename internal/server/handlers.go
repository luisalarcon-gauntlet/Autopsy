// Package server provides the HTTP handlers and middleware for Autopsy.
package server

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"

	"github.com/yourusername/autopsy/internal/bundle"
	"github.com/yourusername/autopsy/internal/config"
	"github.com/yourusername/autopsy/internal/session"
)

const (
	maxMemoryBytes = 32 << 20 // 32MB for multipart parsing in memory
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	cfg   config.Config
	tmpl  *template.Template
	store *session.Store
}

// NewHandler creates a Handler with the given config, parsed templates, and session store.
func NewHandler(cfg config.Config, tmpl *template.Template, store *session.Store) *Handler {
	return &Handler{cfg: cfg, tmpl: tmpl, store: store}
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

// HandleUpload accepts a multipart upload of a .tar.gz bundle, extracts it
// to a temp directory, creates a session, and redirects via HTMX to the report page.
func (h *Handler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	maxBytes := h.cfg.MaxBundleMB * 1024 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes)

	if err := r.ParseMultipartForm(maxMemoryBytes); err != nil {
		if strings.Contains(err.Error(), "request body too large") ||
			strings.Contains(err.Error(), "http: request body too large") {
			jsonError(w, fmt.Sprintf("Bundle exceeds maximum allowed size (%dMB)", h.cfg.MaxBundleMB), http.StatusRequestEntityTooLarge)
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

	tmpDir, err := bundle.Extract(r.Context(), file, bundle.MaxTotalSizeBytes)
	if err != nil {
		slog.Error("bundle extraction failed", "filename", name, "err", err)
		jsonError(w, "Failed to extract bundle: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	sess := h.store.New(tmpDir)
	slog.Info("session created", "sessionID", sess.ID, "bundleDir", tmpDir)

	w.Header().Set("HX-Redirect", "/report/"+sess.ID)
	w.WriteHeader(http.StatusOK)
}

// HandleReport serves the analysis report page for a given session.
func (h *Handler) HandleReport(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	sess, ok := h.store.Get(sessionID)
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	data := map[string]any{
		"StubMode":  h.cfg.StubMode,
		"SessionID": sess.ID,
	}
	if err := h.tmpl.ExecuteTemplate(w, "report.html", data); err != nil {
		slog.Error("template execution failed", "template", "report.html", "err", err)
	}
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
