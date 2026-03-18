// Package main is the entry point for the Autopsy support bundle analyzer.
package main

import (
	"html/template"
	"log"
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

	// Parse separate template sets per page so {{define "content"}} blocks in
	// upload.html and report.html don't collide in the same template namespace.
	partials := "templates/partials/*.html"
	layout := "templates/layout.html"

	uploadTmpl := template.Must(template.ParseFS(templateFS, layout, partials, "templates/upload.html"))
	reportTmpl := template.Must(template.ParseFS(templateFS, layout, partials, "templates/report.html"))

	for _, t := range uploadTmpl.Templates() {
		log.Println("loaded upload template:", t.Name())
	}
	for _, t := range reportTmpl.Templates() {
		log.Println("loaded report template:", t.Name())
	}

	// The Anthropic client reads ANTHROPIC_API_KEY from the environment automatically.
	// In stub mode the client exists but Claude calls are bypassed.
	client := anthropic.NewClient()

	h := server.NewHandler(cfg, &client)
	h.SetTemplate(uploadTmpl)
	h.SetReportTemplate(reportTmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.HandleIndex)   // exact root match
	mux.HandleFunc("GET /upload", h.HandleIndex) // upload page
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
