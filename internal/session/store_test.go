package session

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestStore_BasicCRUD(t *testing.T) {
	store := NewStore(30 * time.Minute)

	sess := store.New("/tmp/test-bundle")
	if sess.ID == "" {
		t.Fatal("New() returned session with empty ID")
	}
	if sess.BundleDir != "/tmp/test-bundle" {
		t.Errorf("BundleDir = %q, want %q", sess.BundleDir, "/tmp/test-bundle")
	}

	got, ok := store.Get(sess.ID)
	if !ok {
		t.Fatalf("Get(%q) not found after Set", sess.ID)
	}
	if got.ID != sess.ID {
		t.Errorf("Get returned ID %q, want %q", got.ID, sess.ID)
	}

	store.Delete(sess.ID)

	_, ok = store.Get(sess.ID)
	if ok {
		t.Error("Get() found session after Delete")
	}
}

func TestStore_GetMissing(t *testing.T) {
	store := NewStore(30 * time.Minute)
	_, ok := store.Get("nonexistent-id")
	if ok {
		t.Error("Get() returned true for nonexistent session")
	}
}

func TestStore_DeleteRemovesBundleDir(t *testing.T) {
	// Create a real temp dir to verify os.RemoveAll is called.
	tmpDir, err := os.MkdirTemp("", "autopsy-session-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}

	store := NewStore(30 * time.Minute)
	sess := store.New(tmpDir)

	store.Delete(sess.ID)

	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Errorf("expected bundle dir %s to be removed after Delete, stat err: %v", tmpDir, err)
		os.RemoveAll(tmpDir)
	}
}

func TestStore_TTLExpiry(t *testing.T) {
	// Create a real temp dir to verify cleanup.
	tmpDir, err := os.MkdirTemp("", "autopsy-ttl-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}

	store := &Store{
		sessions: make(map[string]*Session),
		ttl:      1 * time.Millisecond, // very short TTL for testing
	}

	sess := store.New(tmpDir)
	sess.CreatedAt = time.Now().Add(-1 * time.Hour) // fake an old session
	store.Set(sess.ID, sess)

	store.deleteExpired()

	_, ok := store.Get(sess.ID)
	if ok {
		t.Error("expired session still present after deleteExpired()")
		os.RemoveAll(tmpDir)
	}

	// Verify the bundle dir was removed.
	if _, err := os.Stat(tmpDir); !os.IsNotExist(err) {
		t.Errorf("expected bundle dir to be removed after TTL expiry, stat err: %v", err)
		os.RemoveAll(tmpDir)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	store := NewStore(30 * time.Minute)

	const goroutines = 20
	const opsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := range goroutines {
		go func(n int) {
			defer wg.Done()
			for j := range opsPerGoroutine {
				key := fmt.Sprintf("session-%d-%d", n, j)
				sess := &Session{
					ID:        key,
					BundleDir: "",
					CreatedAt: time.Now(),
				}
				store.Set(key, sess)
				store.Get(key)
				if j%5 == 0 {
					store.Delete(key)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestStore_Len(t *testing.T) {
	store := NewStore(30 * time.Minute)
	if store.Len() != 0 {
		t.Errorf("new store Len() = %d, want 0", store.Len())
	}

	s1 := store.New("")
	s2 := store.New("")
	if store.Len() != 2 {
		t.Errorf("Len() = %d, want 2", store.Len())
	}

	store.Delete(s1.ID)
	if store.Len() != 1 {
		t.Errorf("Len() = %d, want 1 after delete", store.Len())
	}
	store.Delete(s2.ID)
}
