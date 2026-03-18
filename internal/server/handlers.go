// Package server provides the HTTP handlers and middleware for Autopsy.
package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/google/uuid"
	"github.com/yourusername/autopsy/internal/analysis"
	"github.com/yourusername/autopsy/internal/auth"
	"github.com/yourusername/autopsy/internal/bundle"
	"github.com/yourusername/autopsy/internal/config"
	"github.com/yourusername/autopsy/internal/db"
	"github.com/yourusername/autopsy/internal/session"
)

const (
	maxMemoryBytes = 32 << 20 // 32MB for multipart parsing in memory
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	cfg          config.Config
	tmpl         *template.Template // upload page + partials
	reportTmpl   *template.Template // report page + partials
	loginTmpl    *template.Template // login page (standalone)
	isvTmpl      *template.Template // ISV dashboard
	platformTmpl *template.Template // platform dashboard
	bundlesTmpl  *template.Template // bundle history page
	store        *session.Store
	client       *anthropic.Client
	cache        *analysis.Cache
	db           *db.DB // nil when DATABASE_URL is not set
}

// NewHandler creates a Handler with the given configuration and API client.
// A session store and analysis cache are created internally using the config's
// SessionTTL. Call SetTemplate to attach parsed HTML templates before serving.
func NewHandler(cfg config.Config, client *anthropic.Client) *Handler {
	return &Handler{
		cfg:    cfg,
		store:  session.NewStore(cfg.SessionTTL),
		client: client,
		cache:  analysis.NewCache(),
	}
}

// SetTemplate attaches the upload-page template set to the handler.
// It must be called before the handler serves any requests that render HTML.
func (h *Handler) SetTemplate(tmpl *template.Template) {
	h.tmpl = tmpl
}

// SetReportTemplate attaches the report-page template set to the handler.
// It must be called before the handler serves any report requests.
func (h *Handler) SetReportTemplate(tmpl *template.Template) {
	h.reportTmpl = tmpl
}

// SetLoginTemplate attaches the login-page template to the handler.
func (h *Handler) SetLoginTemplate(tmpl *template.Template) { h.loginTmpl = tmpl }

// SetISVTemplate attaches the ISV dashboard template to the handler.
func (h *Handler) SetISVTemplate(tmpl *template.Template) { h.isvTmpl = tmpl }

// SetPlatformTemplate attaches the platform dashboard template to the handler.
func (h *Handler) SetPlatformTemplate(tmpl *template.Template) { h.platformTmpl = tmpl }

// SetBundlesTemplate attaches the bundle history page template to the handler.
func (h *Handler) SetBundlesTemplate(tmpl *template.Template) { h.bundlesTmpl = tmpl }

// SetDB attaches a database connection to the handler. May be nil for in-memory-only mode.
func (h *Handler) SetDB(d *db.DB) { h.db = d }

// HandleHome serves the role-appropriate dashboard for the logged-in user.
func (h *Handler) HandleHome(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	base := map[string]any{
		"StubMode": h.cfg.StubMode,
		"User":     user,
	}
	switch user.Role {
	case auth.RoleISV:
		base["Customers"] = auth.ISVCustomers[user.Username]
		if err := h.isvTmpl.ExecuteTemplate(w, "dashboard_isv.html", base); err != nil {
			slog.Error("template execution failed", "template", "dashboard_isv.html", "err", err)
		}
	case auth.RolePlatform:
		base["Inbox"] = auth.PlatformInbox
		base["Partners"] = auth.PlatformPartners
		if err := h.platformTmpl.ExecuteTemplate(w, "dashboard_platform.html", base); err != nil {
			slog.Error("template execution failed", "template", "dashboard_platform.html", "err", err)
		}
	default:
		http.Redirect(w, r, "/upload", http.StatusSeeOther)
	}
}

// HandleIndex serves the upload page.
func (h *Handler) HandleIndex(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{
		"StubMode":    h.cfg.StubMode,
		"MaxBundleMB": h.cfg.MaxBundleMB,
	}
	if user, ok := auth.FromContext(r.Context()); ok {
		data["User"] = user
		if user.Role == auth.RoleISV {
			data["Customers"] = auth.ISVCustomers[user.Username]
		}
	}
	if err := h.tmpl.ExecuteTemplate(w, "upload.html", data); err != nil {
		slog.Error("template execution failed", "template", "upload.html", "err", err)
	}
}

// HandleUpload accepts a multipart upload of a .tar.gz bundle, extracts and
// parses it, creates a session, and redirects via HTMX to the report page.
func (h *Handler) HandleUpload(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Upload request received")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

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

	// Buffer the file so we can hash it and extract it.
	fileBytes, err := io.ReadAll(file)
	if err != nil {
		slog.Error("failed to read upload", "filename", name, "err", err)
		jsonError(w, "Failed to read uploaded file: "+err.Error(), http.StatusInternalServerError)
		return
	}

	sum := sha256.Sum256(fileBytes)
	bundleSHA256 := hex.EncodeToString(sum[:])
	slog.Info("bundle SHA256 computed", "sha256_prefix", bundleSHA256[:8])

	// If a DB is connected, check whether this bundle was already analyzed.
	// If so, restore (or reuse) the existing session and redirect immediately.
	if h.db != nil {
		user, _ := auth.FromContext(r.Context())
		if existing, err := h.db.GetBundleBySHA256(r.Context(), bundleSHA256, user.Username); err == nil && existing != nil {
			slog.Info("duplicate SHA256 — reusing existing bundle", "bundleID", existing.ID)
			if _, ok := h.store.Get(existing.ID); !ok {
				h.restoreSessionFromDB(r.Context(), existing, user.Username)
			}
			w.Header().Set("HX-Redirect", "/report/"+existing.ID)
			w.WriteHeader(http.StatusOK)
			return
		}
	}

	tmpDir, err := bundle.Extract(r.Context(), bytes.NewReader(fileBytes), bundle.MaxTotalSizeBytes)
	if err != nil {
		slog.Error("bundle extraction failed", "filename", name, "err", err)
		jsonError(w, "Failed to extract bundle: "+err.Error(), http.StatusUnprocessableEntity)
		return
	}

	// Parse the bundle contents immediately so SSE handlers have data ready.
	bundleData, err := bundle.Parse(r.Context(), tmpDir)
	if err != nil {
		// Non-fatal: SSE handlers will use an empty BundleData in stub mode,
		// or show an error partial in live mode.
		slog.Warn("bundle parse returned error", "filename", name, "err", err)
	}

	sess := h.store.New(tmpDir)
	sess.BundleSHA256 = bundleSHA256
	sess.BundleData = bundleData
	h.store.Set(sess.ID, sess)
	slog.Info("session created", "sessionID", sess.ID, "sha256_prefix", bundleSHA256[:8])

	// Persist bundle record to DB (non-fatal on error).
	if h.db != nil {
		user, _ := auth.FromContext(r.Context())
		if err := h.db.InsertBundle(r.Context(), db.Bundle{
			ID:            sess.ID,
			OrgID:         user.Username,
			CustomerName:  r.FormValue("customer"),
			Filename:      name,
			FileSizeBytes: int64(len(fileBytes)),
			SHA256:        bundleSHA256,
			UploadedBy:    user.Name,
			FileData:      fileBytes,
		}); err != nil {
			slog.Error("failed to persist bundle to DB", "sessionID", sess.ID, "err", err)
		}
	}

	w.Header().Set("HX-Redirect", "/report/"+sess.ID)
	w.WriteHeader(http.StatusOK)
}

// HandleReport serves the analysis report page for a given session.
// If the session is not in memory but a DB is connected, it reconstructs
// the session from stored file_data so past reports survive server restarts.
func (h *Handler) HandleReport(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	sess, ok := h.store.Get(sessionID)
	if !ok && h.db != nil {
		user, _ := auth.FromContext(r.Context())
		if dbBundle, err := h.db.GetBundleByID(r.Context(), sessionID, user.Username); err == nil && dbBundle != nil {
			sess = h.restoreSessionFromDB(r.Context(), dbBundle, user.Username)
			ok = sess != nil
		}
	}
	if !ok {
		http.Error(w, "Session not found", http.StatusNotFound)
		return
	}

	// Pre-populate chat history from DB if not already in memory.
	if h.db != nil && len(sess.ChatHistory) == 0 {
		if msgs, err := h.db.GetChatMessagesByBundleID(r.Context(), sessionID); err == nil && len(msgs) > 0 {
			for _, m := range msgs {
				sess.ChatHistory = append(sess.ChatHistory, session.ChatMessage{Role: m.Role, Content: m.Content})
			}
			h.store.Set(sessionID, sess)
		}
	}

	data := map[string]any{
		"StubMode":    h.cfg.StubMode,
		"SessionID":   sess.ID,
		"ChatHistory": sess.ChatHistory,
	}
	if user, ok := auth.FromContext(r.Context()); ok {
		data["User"] = user
	}
	if err := h.reportTmpl.ExecuteTemplate(w, "report.html", data); err != nil {
		slog.Error("template execution failed", "template", "report.html", "err", err)
	}
}

// HandleHealthz returns a simple health check response.
func (h *Handler) HandleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok","service":"autopsy"}`))
}

// HandleDebugCache returns a JSON snapshot of the analysis cache.
// Only available when not in production (it exposes internal state).
func (h *Handler) HandleDebugCache(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	snapshot := h.cache.Snapshot()
	json.NewEncoder(w).Encode(map[string]any{
		"count":   len(snapshot),
		"entries": snapshot,
	})
}

// HandleTriageSSE streams Phase 1 (triage) analysis results via SSE.
// It checks the cache first and only calls Claude if necessary.
// The rendered risk_card HTML partial is sent as the "triage-update" event.
func (h *Handler) HandleTriageSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	sess, ok := h.store.Get(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	sse, err := NewSSEWriter(w, r)
	if err != nil {
		slog.Error("SSE triage: failed to create SSE writer", "sessionID", sessionID, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if sess.BundleData == nil {
		slog.Warn("SSE triage: BundleData is nil, proceeding with empty data", "sessionID", sessionID)
	}

	ctx := r.Context()

	// Check analysis cache (keyed by bundle SHA256).
	var triageResult *analysis.TriageResult
	fromCache := false
	if cached, hit := h.cache.Get(sess.BundleSHA256); hit && cached.Triage != nil {
		slog.Info("triage cache hit", "sha256_prefix", sess.BundleSHA256[:8])
		triageResult = cached.Triage
		fromCache = true
	} else {
		slog.Info("triage cache miss, running analysis", "sha256_prefix", sess.BundleSHA256[:8])
		data := bundleDataOrEmpty(sess)
		result, runErr := analysis.RunTriage(ctx, h.client, data, h.cfg.StubMode)
		if runErr != nil {
			select {
			case <-ctx.Done():
				return // client disconnected — goroutine exits
			default:
			}
			slog.Error("triage analysis failed", "err", runErr)
			sendErrorPartial(sse, "triage-update", "Analysis unavailable — "+runErr.Error())
			sse.SendEvent("done", "{}")
			return
		}
		triageResult = result
		h.upsertCache(sess.BundleSHA256, func(c *analysis.CachedResult) {
			c.Triage = triageResult
		})
		if h.db != nil {
			if raw, err := json.Marshal(triageResult); err == nil {
				if err := h.db.SaveTriage(ctx, sessionID, triageResult.SeverityScore, triageResult.ClusterHealth, string(raw)); err != nil {
					slog.Warn("failed to save triage to DB", "sessionID", sessionID, "err", err)
				}
			}
		}
	}

	var buf bytes.Buffer
	triageData := struct {
		*analysis.TriageResult
		FromCache bool
	}{triageResult, fromCache}
	if err := h.tmpl.ExecuteTemplate(&buf, "risk_card", triageData); err != nil {
		slog.Error("failed to render risk_card template", "err", err)
		sendErrorPartial(sse, "triage-update", "Failed to render results")
	} else {
		if err := sse.SendHTML("triage-update", buf.String()); err != nil {
			slog.Warn("triage SSE send failed (client disconnected?)", "err", err)
			return
		}
	}

	sse.SendEvent("done", "{}")
}

// HandleTimelineSSE streams Phase 2 (timeline) analysis results via SSE.
// The rendered timeline HTML partial is sent as the "timeline-update" event.
func (h *Handler) HandleTimelineSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	sess, ok := h.store.Get(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	sse, err := NewSSEWriter(w, r)
	if err != nil {
		slog.Error("SSE timeline: failed to create SSE writer", "sessionID", sessionID, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if sess.BundleData == nil {
		slog.Warn("SSE timeline: BundleData is nil, proceeding with empty data", "sessionID", sessionID)
	}

	ctx := r.Context()

	var timelineResult *analysis.TimelineResult
	if cached, hit := h.cache.Get(sess.BundleSHA256); hit && cached.Timeline != nil {
		slog.Info("timeline cache hit", "sha256_prefix", sess.BundleSHA256[:8])
		timelineResult = cached.Timeline
	} else {
		slog.Info("timeline cache miss, running analysis", "sha256_prefix", sess.BundleSHA256[:8])
		data := bundleDataOrEmpty(sess)
		result, runErr := analysis.RunTimeline(ctx, h.client, data, h.cfg.StubMode)
		if runErr != nil {
			select {
			case <-ctx.Done():
				return
			default:
			}
			slog.Error("timeline analysis failed", "err", runErr)
			sendErrorPartial(sse, "timeline-update", "Analysis unavailable — "+runErr.Error())
			sse.SendEvent("done", "{}")
			return
		}
		timelineResult = result
		h.upsertCache(sess.BundleSHA256, func(c *analysis.CachedResult) {
			c.Timeline = timelineResult
		})
		if h.db != nil {
			if raw, err := json.Marshal(timelineResult); err == nil {
				if err := h.db.SaveTimeline(ctx, sessionID, string(raw)); err != nil {
					slog.Warn("failed to save timeline to DB", "sessionID", sessionID, "err", err)
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := h.tmpl.ExecuteTemplate(&buf, "timeline", timelineResult); err != nil {
		slog.Error("failed to render timeline template", "err", err)
		sendErrorPartial(sse, "timeline-update", "Failed to render results")
	} else {
		if err := sse.SendHTML("timeline-update", buf.String()); err != nil {
			slog.Warn("timeline SSE send failed (client disconnected?)", "err", err)
			return
		}
	}

	sse.SendEvent("done", "{}")
}

// HandleRCASSE streams Phase 3 (RCA) analysis results via SSE.
// Text chunks are sent as "rca-chunk" events; a final "done" event signals completion.
// Context cancellation (client disconnect) stops streaming immediately.
func (h *Handler) HandleRCASSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	sess, ok := h.store.Get(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	sse, err := NewSSEWriter(w, r)
	if err != nil {
		slog.Error("SSE rca: failed to create SSE writer", "sessionID", sessionID, "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if sess.BundleData == nil {
		slog.Warn("SSE rca: BundleData is nil, proceeding with empty data", "sessionID", sessionID)
	}

	ctx := r.Context()

	// Check cache — if RCA text is cached, stream it in chunks to simulate
	// streaming behaviour and maintain a consistent UI experience.
	if cached, hit := h.cache.Get(sess.BundleSHA256); hit && cached.RCAText != "" {
		slog.Info("RCA cache hit, replaying", "sha256_prefix", sess.BundleSHA256[:8])
		if err := streamTextAsSSE(ctx, sse, cached.RCAText); err != nil {
			slog.Warn("RCA cache replay interrupted", "err", err)
			return
		}
		sse.SendEvent("done", "{}")
		return
	}

	slog.Info("RCA cache miss, running analysis", "sha256_prefix", sess.BundleSHA256[:8])

	// Use a goroutine + channel pattern so we can select on context cancellation.
	data := bundleDataOrEmpty(sess)
	rcaWriter := &rcaChunkWriter{sse: sse}

	type rcaResult struct {
		text string
		err  error
	}
	resultCh := make(chan rcaResult, 1)

	// Accumulate RCA text while streaming it chunk by chunk.
	var textBuf strings.Builder
	accumWriter := &accumulatingWriter{delegate: rcaWriter, buf: &textBuf}

	go func() {
		runErr := analysis.RunRCA(ctx, h.client, data, h.cfg.StubMode, accumWriter)
		resultCh <- rcaResult{text: textBuf.String(), err: runErr}
	}()

	select {
	case <-ctx.Done():
		slog.Info("RCA SSE client disconnected", "sessionID", sessionID)
		return // goroutine exits via ctx when RunRCA respects context
	case res := <-resultCh:
		if res.err != nil {
			slog.Error("RCA analysis failed", "err", res.err)
			sse.SendEvent("rca-error", template.HTMLEscapeString(res.err.Error()))
			sse.SendEvent("done", "{}")
			return
		}
		// Store completed RCA text in cache.
		h.upsertCache(sess.BundleSHA256, func(c *analysis.CachedResult) {
			c.RCAText = res.text
		})
		if h.db != nil {
			if err := h.db.SaveRCA(ctx, sessionID, res.text); err != nil {
				slog.Warn("failed to save RCA to DB", "sessionID", sessionID, "err", err)
			}
		}
	}

	sse.SendEvent("done", "{}")
}

// HandleChat processes a synchronous chat message and returns rendered message bubbles.
// It appends both the user message and assistant response to the session's chat history.
func (h *Handler) HandleChat(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	sess, ok := h.store.Get(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "failed to parse form", http.StatusBadRequest)
		return
	}

	message := strings.TrimSpace(r.FormValue("message"))
	if message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	// Snapshot history before appending the new user message.
	history := make([]analysis.ChatMessage, len(sess.ChatHistory))
	copy(history, sess.ChatHistory)

	data := bundleDataOrEmpty(sess)
	response, err := analysis.RunChat(r.Context(), h.client, data, history, message, h.cfg.StubMode)
	if err != nil {
		slog.Error("chat RunChat failed", "sessionID", sessionID, "err", err)
		response = "Sorry, I encountered an error: " + err.Error()
	}

	// Persist both turns to session history.
	sess.ChatHistory = append(history,
		analysis.ChatMessage{Role: "user", Content: message},
		analysis.ChatMessage{Role: "assistant", Content: response},
	)
	h.store.Set(sessionID, sess)

	if h.db != nil {
		h.db.InsertChatMessage(r.Context(), db.ChatMessage{ID: uuid.New().String(), BundleID: sessionID, Role: "user", Content: message})
		h.db.InsertChatMessage(r.Context(), db.ChatMessage{ID: uuid.New().String(), BundleID: sessionID, Role: "assistant", Content: response})
	}

	type chatData struct {
		UserMessage string
		Response    string
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "chat_messages", chatData{
		UserMessage: message,
		Response:    response,
	}); err != nil {
		slog.Error("chat template execution failed", "err", err)
	}
}

// HandleChatSSE streams a chat response token-by-token via SSE.
// The message is read from the "message" query parameter.
// It emits "chat-chunk" events during streaming, then a final "done" event.
// Context cancellation (client disconnect) stops streaming immediately.
func (h *Handler) HandleChatSSE(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	sess, ok := h.store.Get(sessionID)
	if !ok {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	message := strings.TrimSpace(r.URL.Query().Get("message"))
	if message == "" {
		http.Error(w, "message query param is required", http.StatusBadRequest)
		return
	}

	sse, err := NewSSEWriter(w, r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx := r.Context()

	// Snapshot history before appending new user turn.
	history := make([]analysis.ChatMessage, len(sess.ChatHistory))
	copy(history, sess.ChatHistory)

	// Persist user message immediately so it survives even if stream is interrupted.
	sess.ChatHistory = append(sess.ChatHistory, analysis.ChatMessage{Role: "user", Content: message})
	h.store.Set(sessionID, sess)
	if h.db != nil {
		h.db.InsertChatMessage(ctx, db.ChatMessage{ID: uuid.New().String(), BundleID: sessionID, Role: "user", Content: message})
	}

	data := bundleDataOrEmpty(sess)

	type streamResult struct {
		text string
		err  error
	}
	resultCh := make(chan streamResult, 1)
	var textBuf strings.Builder
	accumWriter := &accumulatingWriter{
		delegate: &chatChunkWriter{sse: sse},
		buf:      &textBuf,
	}

	go func() {
		runErr := analysis.RunChatStream(ctx, h.client, data, history, message, h.cfg.StubMode, accumWriter)
		resultCh <- streamResult{text: textBuf.String(), err: runErr}
	}()

	select {
	case <-ctx.Done():
		slog.Info("chat SSE client disconnected", "sessionID", sessionID)
		return
	case res := <-resultCh:
		if res.err != nil {
			slog.Error("chat stream failed", "sessionID", sessionID, "err", res.err)
			sse.SendEvent("chat-error", template.HTMLEscapeString(res.err.Error()))
			sse.SendEvent("done", "{}")
			return
		}
		// Save assistant response to session history.
		if current, exists := h.store.Get(sessionID); exists {
			current.ChatHistory = append(current.ChatHistory, analysis.ChatMessage{
				Role: "assistant", Content: res.text,
			})
			h.store.Set(sessionID, current)
		}
		if h.db != nil {
			// User message was already persisted at stream start; save assistant turn.
			h.db.InsertChatMessage(ctx, db.ChatMessage{ID: uuid.New().String(), BundleID: sessionID, Role: "assistant", Content: res.text})
		}
	}

	sse.SendEvent("done", "{}")
}

// HandleSuggestions returns rendered suggested starter question pills for the
// chat panel, based on the cached triage result for the session.
func (h *Handler) HandleSuggestions(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	sess, ok := h.store.Get(sessionID)
	if !ok {
		w.WriteHeader(http.StatusOK)
		return
	}

	var triage *analysis.TriageResult
	if cached, hit := h.cache.Get(sess.BundleSHA256); hit && cached.Triage != nil {
		triage = cached.Triage
	}

	questions := generateSuggestedQuestions(triage)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := h.tmpl.ExecuteTemplate(w, "chat_suggestions", questions); err != nil {
		slog.Error("suggestions template failed", "err", err)
	}
}

// generateSuggestedQuestions produces up to 4 context-aware question strings
// based on the top issues in the triage result. It always includes two
// canonical remediation questions as anchors.
func generateSuggestedQuestions(triage *analysis.TriageResult) []string {
	anchors := []string{"What should I fix first?", "Give me all the kubectl commands to remediate this"}

	if triage == nil {
		return anchors
	}

	var contextual []string
	for _, issue := range triage.TopIssues {
		if len(contextual) >= 2 {
			break
		}
		pod := issue.AffectedPod
		if pod == "" {
			pod = "this workload"
		}
		switch issue.Category {
		case "oom":
			contextual = append(contextual, fmt.Sprintf("Why is %s getting OOMKilled?", pod))
		case "crash-loop":
			contextual = append(contextual, fmt.Sprintf("What is causing %s to crash?", pod))
		case "image-pull":
			contextual = append(contextual, fmt.Sprintf("How do I fix the image pull error on %s?", pod))
		case "config":
			contextual = append(contextual, fmt.Sprintf("What config is missing for %s?", pod))
		case "resource":
			contextual = append(contextual, fmt.Sprintf("Why is %s pending?", pod))
		}
	}

	questions := append(contextual, anchors...)
	if len(questions) > 4 {
		questions = questions[:4]
	}
	return questions
}

// HandleBundles serves GET /bundles — the bundle history page for the current user's org.
func (h *Handler) HandleBundles(w http.ResponseWriter, r *http.Request) {
	data := map[string]any{"StubMode": h.cfg.StubMode}
	if user, ok := auth.FromContext(r.Context()); ok {
		data["User"] = user
	}

	if h.db == nil {
		// No database — render empty state with informational message.
		if err := h.bundlesTmpl.ExecuteTemplate(w, "bundles.html", data); err != nil {
			slog.Error("template execution failed", "template", "bundles.html", "err", err)
		}
		return
	}

	user, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	items, err := h.db.GetBundlesByOrg(r.Context(), user.Username)
	if err != nil {
		slog.Error("GetBundlesByOrg failed", "org", user.Username, "err", err)
		items = nil
	}
	data["Bundles"] = items

	if err := h.bundlesTmpl.ExecuteTemplate(w, "bundles.html", data); err != nil {
		slog.Error("template execution failed", "template", "bundles.html", "err", err)
	}
}

// HandleBundleDownload serves GET /bundles/{id}/download — streams the original bundle file.
func (h *Handler) HandleBundleDownload(w http.ResponseWriter, r *http.Request) {
	if h.db == nil {
		http.Error(w, "storage not configured", http.StatusServiceUnavailable)
		return
	}
	bundleID := r.PathValue("id")
	user, ok := auth.FromContext(r.Context())
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	b, err := h.db.GetBundleByID(r.Context(), bundleID, user.Username)
	if err != nil {
		slog.Error("download: db error", "bundleID", bundleID, "err", err)
		http.Error(w, "storage error", http.StatusInternalServerError)
		return
	}
	if b == nil {
		http.Error(w, "bundle not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, b.Filename))
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(b.FileData)))
	w.Write(b.FileData)
}

// restoreSessionFromDB reconstructs an in-memory session from a DB bundle record,
// re-extracts the bundle, and loads cached analysis results.
// Returns nil if extraction fails.
func (h *Handler) restoreSessionFromDB(ctx context.Context, b *db.Bundle, orgID string) *session.Session {
	tmpDir, err := bundle.Extract(ctx, bytes.NewReader(b.FileData), bundle.MaxTotalSizeBytes)
	if err != nil {
		slog.Warn("restoreSession: extraction failed", "bundleID", b.ID, "err", err)
		return nil
	}
	bundleData, _ := bundle.Parse(ctx, tmpDir)

	sess := &session.Session{
		ID:           b.ID,
		BundleDir:    tmpDir,
		BundleSHA256: b.SHA256,
		BundleData:   bundleData,
		CreatedAt:    time.Now(),
	}
	h.store.Set(b.ID, sess)

	// Load prior analysis results into the in-memory cache so SSE endpoints
	// serve from cache immediately without re-calling Claude.
	if dbAnalysis, err := h.db.GetAnalysisByBundleID(ctx, b.ID); err == nil && dbAnalysis != nil {
		h.restoreAnalysisCache(b.SHA256, dbAnalysis)
	}

	slog.Info("session restored from DB", "bundleID", b.ID)
	return sess
}

// restoreAnalysisCache loads persisted analysis JSON back into the in-memory cache.
func (h *Handler) restoreAnalysisCache(sha256Hash string, a *db.Analysis) {
	if a.TriageJSON != "" {
		var triage analysis.TriageResult
		if err := json.Unmarshal([]byte(a.TriageJSON), &triage); err == nil {
			h.upsertCache(sha256Hash, func(c *analysis.CachedResult) { c.Triage = &triage })
		}
	}
	if a.TimelineJSON != "" {
		var timeline analysis.TimelineResult
		if err := json.Unmarshal([]byte(a.TimelineJSON), &timeline); err == nil {
			h.upsertCache(sha256Hash, func(c *analysis.CachedResult) { c.Timeline = &timeline })
		}
	}
	if a.RCAMarkdown != "" {
		h.upsertCache(sha256Hash, func(c *analysis.CachedResult) { c.RCAText = a.RCAMarkdown })
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

// bundleDataOrEmpty returns the session's parsed BundleData, falling back to
// an empty struct if parsing failed or hasn't been done yet.
func bundleDataOrEmpty(sess *session.Session) *bundle.BundleData {
	if sess.BundleData != nil {
		return sess.BundleData
	}
	return &bundle.BundleData{}
}

// upsertCache fetches (or creates) a CachedResult for the given SHA256 and
// applies the mutator function before writing it back to the cache.
func (h *Handler) upsertCache(sha256 string, mutate func(*analysis.CachedResult)) {
	cached, ok := h.cache.Get(sha256)
	if !ok || cached == nil {
		cached = &analysis.CachedResult{}
	}
	mutate(cached)
	h.cache.Set(sha256, cached)
}

// sendErrorPartial sends an error HTML fragment as a named SSE event.
func sendErrorPartial(sse *SSEWriter, event, msg string) {
	html := `<div class="p-6"><div class="flex items-start gap-3 p-4 bg-red-50 border border-red-100 rounded-lg">` +
		`<svg class="w-5 h-5 text-red-500 mt-0.5 flex-shrink-0" fill="currentColor" viewBox="0 0 20 20" aria-hidden="true">` +
		`<path fill-rule="evenodd" d="M10 18a8 8 0 100-16 8 8 0 000 16zM8.707 7.293a1 1 0 00-1.414 1.414L8.586 10l-1.293 1.293a1 1 0 101.414 1.414L10 11.414l1.293 1.293a1 1 0 001.414-1.414L11.414 10l1.293-1.293a1 1 0 00-1.414-1.414L10 8.586 8.707 7.293z" clip-rule="evenodd"/>` +
		`</svg><p class="text-sm text-red-700">` + template.HTMLEscapeString(msg) + `</p></div></div>`
	sse.SendHTML(event, html)
}

// streamTextAsSSE sends a cached full text string as individual SSE chunk events
// by replaying it in fixed-size pieces (same as stub streaming behaviour).
func streamTextAsSSE(ctx context.Context, sse *SSEWriter, text string) error {
	const chunkSize = 50
	runes := []rune(text)
	for i := 0; i < len(runes); i += chunkSize {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		end := i + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		if err := sse.SendEvent("rca-chunk", string(runes[i:end])); err != nil {
			return fmt.Errorf("streamTextAsSSE: %w", err)
		}
	}
	return nil
}

// accumulatingWriter wraps an io.Writer delegate and simultaneously accumulates
// all written bytes into a strings.Builder for caching after streaming.
type accumulatingWriter struct {
	delegate io.Writer
	buf      *strings.Builder
}

// Write writes to the delegate and accumulates into the buffer.
func (a *accumulatingWriter) Write(p []byte) (int, error) {
	a.buf.Write(p) // strings.Builder.Write never returns an error
	return a.delegate.Write(p)
}

// jsonError writes a JSON error body with the given HTTP status code.
func jsonError(w http.ResponseWriter, msg string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
