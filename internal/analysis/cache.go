package analysis

import (
	"log/slog"
	"sync"
	"time"
)

const cacheTTL = 2 * time.Hour

// CachedResult stores all three analysis phase results keyed by bundle SHA256.
type CachedResult struct {
	// Triage is the Phase 1 result.
	Triage *TriageResult

	// Timeline is the Phase 2 result.
	Timeline *TimelineResult

	// RCAText is the accumulated Phase 3 streaming text.
	RCAText string

	// CachedAt is when this result was stored.
	CachedAt time.Time
}

// Cache is a thread-safe in-memory store for analysis results keyed by bundle SHA256 hash.
// Entries expire after cacheTTL (2 hours).
type Cache struct {
	mu      sync.RWMutex
	entries map[string]*CachedResult
}

// NewCache creates an empty Cache and starts a background TTL cleanup goroutine.
func NewCache() *Cache {
	c := &Cache{
		entries: make(map[string]*CachedResult),
	}
	go c.cleanupLoop()
	return c
}

// Get retrieves a cached result by bundle SHA256. Returns (nil, false) on miss or expiry.
func (c *Cache) Get(sha256 string) (*CachedResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[sha256]
	if !ok {
		return nil, false
	}
	if time.Since(entry.CachedAt) > cacheTTL {
		return nil, false
	}
	return entry, true
}

// Set stores an analysis result under the given bundle SHA256 key.
func (c *Cache) Set(sha256 string, result *CachedResult) {
	result.CachedAt = time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[sha256] = result
}

// Len returns the number of cache entries (including potentially expired ones).
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// cleanupLoop runs every 30 minutes and evicts expired cache entries.
func (c *Cache) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.evictExpired()
	}
}

func (c *Cache) evictExpired() {
	now := time.Now()

	c.mu.RLock()
	var expired []string
	for sha, entry := range c.entries {
		if now.Sub(entry.CachedAt) > cacheTTL {
			expired = append(expired, sha)
		}
	}
	c.mu.RUnlock()

	if len(expired) == 0 {
		return
	}

	c.mu.Lock()
	for _, sha := range expired {
		delete(c.entries, sha)
	}
	c.mu.Unlock()

	slog.Info("cache: evicted expired entries", "count", len(expired))
}
