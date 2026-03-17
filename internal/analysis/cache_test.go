package analysis

import (
	"sync"
	"testing"
	"time"
)

func TestCacheSetAndGet(t *testing.T) {
	c := NewCache()

	result := &CachedResult{
		Triage:   &TriageResult{SeverityScore: 72, ClusterHealth: "degraded"},
		RCAText:  "some rca text",
		CachedAt: time.Now(),
	}

	c.Set("abc123", result)

	got, ok := c.Get("abc123")
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if got.Triage.SeverityScore != 72 {
		t.Errorf("expected SeverityScore=72, got %d", got.Triage.SeverityScore)
	}
}

func TestCacheMiss(t *testing.T) {
	c := NewCache()
	_, ok := c.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for unknown key")
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	c := NewCache()

	result := &CachedResult{
		RCAText:  "expired content",
		CachedAt: time.Now().Add(-3 * time.Hour), // already expired
	}

	// Directly insert the expired entry (bypassing Set which sets CachedAt=now)
	c.mu.Lock()
	c.entries["expired-key"] = result
	c.mu.Unlock()

	_, ok := c.Get("expired-key")
	if ok {
		t.Error("expected cache miss for expired entry")
	}
}

func TestCacheConcurrentAccess(t *testing.T) {
	c := NewCache()
	const workers = 20

	var wg sync.WaitGroup
	wg.Add(workers)

	for i := 0; i < workers; i++ {
		go func(n int) {
			defer wg.Done()
			key := "key"
			c.Set(key, &CachedResult{RCAText: "data"})
			c.Get(key)
		}(i)
	}
	wg.Wait()
}

func TestCacheLen(t *testing.T) {
	c := NewCache()
	if c.Len() != 0 {
		t.Errorf("expected empty cache, got %d entries", c.Len())
	}

	c.Set("a", &CachedResult{})
	c.Set("b", &CachedResult{})

	if c.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", c.Len())
	}
}
