// Package main is the entry point for the Autopsy support bundle analyzer.
package main

import (
	"context"
	"html/template"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/joho/godotenv"
	"github.com/yourusername/autopsy/internal/config"
	"github.com/yourusername/autopsy/internal/db"
	"github.com/yourusername/autopsy/internal/seed"
	"github.com/yourusername/autopsy/internal/server"
)

// version is set at build time via -ldflags "-X main.version=<git-sha>".
var version = "v1.0.0"

func main() {
	_ = godotenv.Load()
	cfg := config.Load()
	config.LogStartup(cfg)

	// Parse separate template sets per page so {{define "content"}} blocks
	// in different pages don't collide in the same template namespace.
	partials := "templates/partials/*.html"
	layout := "templates/layout.html"

	uploadTmpl := template.Must(template.ParseFS(templateFS, layout, partials, "templates/upload.html"))
	reportTmpl := template.Must(template.ParseFS(templateFS, layout, partials, "templates/report.html"))
	loginTmpl := template.Must(template.ParseFS(templateFS, "templates/login.html"))
	isvTmpl := template.Must(template.ParseFS(templateFS, layout, "templates/dashboard_isv.html"))
	platformTmpl := template.Must(template.ParseFS(templateFS, layout, "templates/dashboard_platform.html"))
	bundlesTmpl := template.Must(template.ParseFS(templateFS, layout, "templates/bundles.html"))
	customerTmpl := template.Must(template.ParseFS(templateFS, layout, "templates/customer_detail.html"))

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
	h.SetVersion(version)
	h.SetTemplate(uploadTmpl)
	h.SetReportTemplate(reportTmpl)
	h.SetLoginTemplate(loginTmpl)
	h.SetISVTemplate(isvTmpl)
	h.SetPlatformTemplate(platformTmpl)
	h.SetBundlesTemplate(bundlesTmpl)
	h.SetCustomerTemplate(customerTmpl)

	// Connect to PostgreSQL if DATABASE_URL is set.
	// Failure is non-fatal: app runs fully in-memory without a DB.
	if cfg.DatabaseURL != "" {
		dbConn, err := db.Open(cfg.DatabaseURL)
		if err != nil {
			slog.Warn("database connection failed — running without persistence", "err", err)
		} else if err := dbConn.Migrate(context.Background()); err != nil {
			slog.Warn("database migration failed — running without persistence", "err", err)
			dbConn.Close()
		} else {
			h.SetDB(dbConn)
			defer dbConn.Close()
			// Seed demo data for Airbyte org on every startup (idempotent).
			seed.Run(context.Background(), dbConn)
		}
	} else {
		slog.Warn("DATABASE_URL not set — running without persistence")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", h.HandleLoginPage)
	mux.HandleFunc("POST /login", h.HandleLoginPost)
	mux.HandleFunc("GET /logout", h.HandleLogout)
	mux.HandleFunc("GET /{$}", h.HandleHome)     // role-specific dashboard
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

	// Customer detail page
	mux.HandleFunc("GET /customers/{customerSlug}", h.HandleCustomerDetail)

	// Bundle history, detail, download, and status cycling
	mux.HandleFunc("GET /bundles", h.HandleBundles)
	mux.HandleFunc("GET /bundles/{id}", h.HandleBundleDetail)
	mux.HandleFunc("PATCH /bundles/{id}/status", h.HandleBundleStatus)
	mux.HandleFunc("GET /bundles/{id}/download", h.HandleBundleDownload)

	// Debug (cache inspection — dev only)
	mux.HandleFunc("GET /debug/cache", h.HandleDebugCache)

	// Wrap mux with auth, request logging, and panic recovery.
	handler := server.RequestLogger(server.PanicRecovery(server.RequireAuth(mux)))

	addr := ":" + cfg.Port
	slog.Info("Autopsy listening", "addr", addr)

	if err := http.ListenAndServe(addr, handler); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
