// Package session manages per-upload session state with TTL-based cleanup.
package session

import (
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yourusername/autopsy/internal/analysis"
	"github.com/yourusername/autopsy/internal/bundle"
)

// Session holds all state associated with a single bundle upload.
type Session struct {
	// ID is the unique session identifier (UUID).
	ID string

	// BundleDir is the path to the extracted bundle temp directory.
	BundleDir string

	// BundleSHA256 is the hex-encoded SHA256 hash of the uploaded .tar.gz bytes.
	// Used as the cache key for analysis results.
	BundleSHA256 string

	// BundleData holds the parsed bundle contents. Nil until parsing completes.
	BundleData *bundle.BundleData

	// Analysis holds the AI analysis results. Nil until analysis completes.
	Analysis *analysis.Result

	// ChatHistory is the ordered list of chat turns for this session.
	ChatHistory []analysis.ChatMessage

	// CreatedAt is when the session was created.
	CreatedAt time.Time
}

// Store is a thread-safe in-memory session map with TTL cleanup.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	ttl      time.Duration
}

// NewStore creates a Store and starts a background goroutine that
// deletes expired sessions every 5 minutes.
func NewStore(ttl time.Duration) *Store {
	s := &Store{
		sessions: make(map[string]*Session),
		ttl:      ttl,
	}
	go s.cleanupLoop()
	return s
}

// New creates a new Session with a generated UUID, stores it, and returns it.
func (s *Store) New(bundleDir string) *Session {
	sess := &Session{
		ID:        uuid.New().String(),
		BundleDir: bundleDir,
		CreatedAt: time.Now(),
	}
	s.Set(sess.ID, sess)
	return sess
}

// Get retrieves a session by ID. Returns (nil, false) if not found.
func (s *Store) Get(id string) (*Session, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

// Set stores or overwrites a session.
func (s *Store) Set(id string, sess *Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = sess
}

// Delete removes a session and cleans up its extracted bundle directory.
func (s *Store) Delete(id string) {
	s.mu.Lock()
	sess, ok := s.sessions[id]
	if ok {
		delete(s.sessions, id)
	}
	s.mu.Unlock()

	if ok && sess.BundleDir != "" {
		if err := os.RemoveAll(sess.BundleDir); err != nil {
			slog.Warn("session delete: failed to remove bundle dir",
				"sessionID", id, "dir", sess.BundleDir, "err", err)
		}
	}
}

// Len returns the number of active sessions.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// cleanupLoop runs every 5 minutes and deletes sessions older than the TTL.
func (s *Store) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		s.deleteExpired()
	}
}

func (s *Store) deleteExpired() {
	now := time.Now()

	s.mu.RLock()
	var expired []string
	for id, sess := range s.sessions {
		if now.Sub(sess.CreatedAt) > s.ttl {
			expired = append(expired, id)
		}
	}
	s.mu.RUnlock()

	for _, id := range expired {
		slog.Info("session TTL expired, deleting", "sessionID", id)
		s.Delete(id)
	}
}
