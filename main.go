// Package main is the entry point for the Autopsy support bundle analyzer.
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/yourusername/autopsy/internal/config"
)

func main() {
	cfg := config.Load()
	config.LogStartup(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Autopsy is running"))
	})

	addr := ":" + cfg.Port
	slog.Info("Autopsy listening", "addr", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
