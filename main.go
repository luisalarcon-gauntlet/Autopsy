// Package main is the entry point for the Autopsy support bundle analyzer.
package main

import (
	"html/template"
	"log/slog"
	"net/http"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/yourusername/autopsy/internal/config"
	"github.com/yourusername/autopsy/internal/server"
)

func main() {
	cfg := config.Load()
	config.LogStartup(cfg)

	// Parse all templates from the embedded FS at startup — panic on failure (startup only).
	// embed.FS ensures the binary is self-contained; no template files needed on disk at runtime.
	tmpl := template.Must(template.ParseFS(templateFS, "templates/*.html", "templates/partials/*.html"))

	// The Anthropic client reads ANTHROPIC_API_KEY from the environment automatically.
	// In stub mode the client exists but Claude calls are bypassed.
	client := anthropic.NewClient()

	h := server.NewHandler(cfg, &client)
	h.SetTemplate(tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.HandleIndex)
	mux.HandleFunc("POST /upload", h.HandleUpload)
	mux.HandleFunc("GET /report/{sessionID}", h.HandleReport)
	mux.HandleFunc("GET /healthz", h.HandleHealthz)

	// SSE analysis streams
	mux.HandleFunc("GET /stream/{sessionID}/triage", h.HandleTriageSSE)
	mux.HandleFunc("GET /stream/{sessionID}/timeline", h.HandleTimelineSSE)
	mux.HandleFunc("GET /stream/{sessionID}/rca", h.HandleRCASSE)

	// Chat (sync post + streaming SSE endpoint + suggestions)
	mux.HandleFunc("POST /chat/{sessionID}", h.HandleChat)
	mux.HandleFunc("GET /chat/{sessionID}/stream", h.HandleChatSSE)
	mux.HandleFunc("GET /suggestions/{sessionID}", h.HandleSuggestions)

	// Debug (cache inspection — dev only)
	mux.HandleFunc("GET /debug/cache", h.HandleDebugCache)

	// Wrap mux with request logging and panic recovery.
	handler := server.RequestLogger(server.PanicRecovery(mux))

	addr := ":" + cfg.Port
	slog.Info("Autopsy listening", "addr", addr)

	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
