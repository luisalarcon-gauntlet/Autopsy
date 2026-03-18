// Package config reads and validates environment variables for Autopsy.
package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"
)

const (
	defaultPort           = "8080"
	defaultMaxBundleMB    = 250
	defaultSessionTTL     = 30 * time.Minute
	defaultCacheTTLHours  = 2
)

// Config holds all runtime configuration for Autopsy.
type Config struct {
	// Port is the TCP port the HTTP server listens on.
	Port string

	// AnthropicAPIKey is the API key for Claude. Empty means stub mode.
	AnthropicAPIKey string

	// MaxBundleMB is the maximum upload size in megabytes.
	MaxBundleMB int64

	// StubMode disables real Claude API calls and returns canned responses.
	StubMode bool

	// SessionTTL is how long sessions (and their temp dirs) are kept.
	SessionTTL time.Duration

	// DatabaseURL is the PostgreSQL connection string (e.g. from Railway's DATABASE_URL).
	// Empty means no persistence — the app runs fully in-memory.
	DatabaseURL string
}

// Load reads configuration from environment variables, applying defaults where
// values are absent or unparseable. It logs a warning and enables StubMode if
// ANTHROPIC_API_KEY is not set.
func Load() Config {
	cfg := Config{
		Port:            getEnv("PORT", defaultPort),
		AnthropicAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		MaxBundleMB:     int64(getEnvInt("MAX_BUNDLE_MB", defaultMaxBundleMB)),
		StubMode:        getEnvBool("STUB_MODE", false),
		SessionTTL:      time.Duration(getEnvInt("SESSION_TTL_MINUTES", int(defaultSessionTTL.Minutes()))) * time.Minute,
		DatabaseURL:     os.Getenv("DATABASE_URL"),
	}

	if cfg.AnthropicAPIKey == "" {
		slog.Warn("ANTHROPIC_API_KEY is not set — running in stub mode")
		cfg.StubMode = true
	}

	return cfg
}

// LogStartup prints a summary of the active config at startup. The API key is
// masked to its last 4 characters for safety.
func LogStartup(cfg Config) {
	maskedKey := maskKey(cfg.AnthropicAPIKey)
	slog.Info("Autopsy configuration",
		"port", cfg.Port,
		"stub_mode", cfg.StubMode,
		"max_bundle_mb", cfg.MaxBundleMB,
		"session_ttl", cfg.SessionTTL,
		"api_key", maskedKey,
	)
}

func maskKey(key string) string {
	if key == "" {
		return "(not set)"
	}
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return n
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		slog.Warn("invalid bool env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return b
}
