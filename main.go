// Package main is the entry point for the Autopsy support bundle analyzer.
package main

import (
	"html/template"
	"log/slog"
	"net/http"
	"os"

	"github.com/yourusername/autopsy/internal/config"
	"github.com/yourusername/autopsy/internal/server"
)

func main() {
	cfg := config.Load()
	config.LogStartup(cfg)

	tmpl := template.Must(template.ParseGlob("templates/*.html"))

	h := server.NewHandler(cfg, tmpl)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", h.HandleIndex)
	mux.HandleFunc("POST /upload", h.HandleUpload)
	mux.HandleFunc("GET /healthz", h.HandleHealthz)

	addr := ":" + cfg.Port
	slog.Info("Autopsy listening", "addr", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
