package session

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestStore_SetAndGet(t *testing.T) {
	store := NewStore(30 * time.Minute)

	dir := t.TempDir()
	sess := store.New(dir)

	got, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("Get() returned false after Set()")
	}
	if got.ID != sess.ID {
		t.Errorf("ID = %q, want %q", got.ID, sess.ID)
	}
	if got.BundleDir != dir {
		t.Errorf("BundleDir = %q, want %q", got.BundleDir, dir)
	}
}

func TestStore_GetMissing(t *testing.T) {
	store := NewStore(30 * time.Minute)

	_, ok := store.Get("does-not-exist")
	if ok {
		t.Error("Get() should return false for unknown ID")
	}
}

func TestStore_Delete(t *testing.T) {
	store := NewStore(30 * time.Minute)

	dir := t.TempDir()
	sess := store.New(dir)

	store.Delete(sess.ID)

	_, ok := store.Get(sess.ID)
	if ok {
		t.Error("Get() should return false after Delete()")
	}
}

func TestStore_DeleteRemovesBundleDir(t *testing.T) {
	store := NewStore(30 * time.Minute)

	// Create a real temp dir with a file in it
	dir := t.TempDir()
	testFile := dir + "/test.txt"
	if err := os.WriteFile(testFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sess := store.New(dir)
	store.Delete(sess.ID)

	// BundleDir should be gone
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("BundleDir still exists after Delete(): %s", dir)
	}
}

func TestStore_UniqueIDs(t *testing.T) {
	store := NewStore(30 * time.Minute)

	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		sess := store.New(t.TempDir())
		if ids[sess.ID] {
			t.Errorf("duplicate session ID generated: %s", sess.ID)
		}
		ids[sess.ID] = true
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	// This test is designed to catch races — run with -race
	store := NewStore(30 * time.Minute)

	const goroutines = 50
	const opsPerGoroutine = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				dir := t.TempDir()
				sess := store.New(dir)

				// Concurrent read
				_, _ = store.Get(sess.ID)

				// Concurrent delete (some will find it, some won't — both OK)
				if j%3 == 0 {
					store.Delete(sess.ID)
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestStore_TTLExpiry(t *testing.T) {
	// Very short TTL for testing
	ttl := 50 * time.Millisecond
	store := NewStore(ttl)

	dir := t.TempDir()
	sess := store.New(dir)

	// Should exist immediately
	if _, ok := store.Get(sess.ID); !ok {
		t.Fatal("session should exist immediately after creation")
	}

	// Wait for TTL to pass
	time.Sleep(ttl * 3)

	// Manually trigger cleanup (or verify it ran)
	store.sweepExpired()

	if _, ok := store.Get(sess.ID); ok {
		t.Error("session should be expired after TTL")
	}
}

func TestStore_UpdateSession(t *testing.T) {
	store := NewStore(30 * time.Minute)

	dir := t.TempDir()
	sess := store.New(dir)

	// Update the session with analysis results
	sess.ChatHistory = append(sess.ChatHistory, ChatMessage{
		Role:    "user",
		Content: "Why is my pod crashing?",
	})
	store.Set(sess.ID, sess)

	got, ok := store.Get(sess.ID)
	if !ok {
		t.Fatal("session not found after Set()")
	}
	if len(got.ChatHistory) != 1 {
		t.Errorf("ChatHistory len = %d, want 1", len(got.ChatHistory))
	}
	if got.ChatHistory[0].Content != "Why is my pod crashing?" {
		t.Errorf("ChatHistory[0].Content = %q", got.ChatHistory[0].Content)
	}
}

func TestStore_MultipleSessionsIsolated(t *testing.T) {
	store := NewStore(30 * time.Minute)

	sessions := make([]*Session, 10)
	for i := 0; i < 10; i++ {
		dir := t.TempDir()
		sess := store.New(dir)
		sess.ChatHistory = append(sess.ChatHistory, ChatMessage{
			Role:    "user",
			Content: fmt.Sprintf("message from session %d", i),
		})
		store.Set(sess.ID, sess)
		sessions[i] = sess
	}

	// Each session should only have its own chat history
	for i, sess := range sessions {
		got, ok := store.Get(sess.ID)
		if !ok {
			t.Errorf("session %d not found", i)
			continue
		}
		if len(got.ChatHistory) != 1 {
			t.Errorf("session %d: expected 1 chat message, got %d", i, len(got.ChatHistory))
		}
		expected := fmt.Sprintf("message from session %d", i)
		if got.ChatHistory[0].Content != expected {
			t.Errorf("session %d: content = %q, want %q", i, got.ChatHistory[0].Content, expected)
		}
	}
}
