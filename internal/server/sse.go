// Package server provides the HTTP handlers and middleware for Autopsy.
package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// SSEWriter wraps an http.ResponseWriter to write Server-Sent Events.
// It sets the required SSE headers and provides helpers to send named events.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	ctx     context.Context
}

// NewSSEWriter validates that the ResponseWriter supports flushing, sets the
// required SSE headers, and returns a ready-to-use SSEWriter.
// It returns an error if the ResponseWriter does not implement http.Flusher.
func NewSSEWriter(w http.ResponseWriter, r *http.Request) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming not supported by this response writer")
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	return &SSEWriter{w: w, flusher: flusher, ctx: r.Context()}, nil
}

// SendEvent writes a named SSE event with the given data.
// Multi-line data is correctly encoded: each line is prefixed with "data: ".
// Returns an error if the client has disconnected or if the write fails.
func (s *SSEWriter) SendEvent(event, data string) error {
	select {
	case <-s.ctx.Done():
		return s.ctx.Err()
	default:
	}

	lines := strings.Split(data, "\n")
	if _, err := fmt.Fprintf(s.w, "event: %s\n", event); err != nil {
		return fmt.Errorf("SSEWriter.SendEvent write event: %w", err)
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(s.w, "data: %s\n", line); err != nil {
			return fmt.Errorf("SSEWriter.SendEvent write data: %w", err)
		}
	}
	if _, err := fmt.Fprintf(s.w, "\n"); err != nil {
		return fmt.Errorf("SSEWriter.SendEvent write terminator: %w", err)
	}
	s.flusher.Flush()
	return nil
}

// SendHTML is a named alias for SendEvent that documents intent: the payload
// is an HTML fragment that HTMX will swap into the DOM.
func (s *SSEWriter) SendHTML(event, html string) error {
	return s.SendEvent(event, html)
}

// Done returns the context's Done channel so callers can select on
// client disconnection alongside other work channels.
func (s *SSEWriter) Done() <-chan struct{} {
	return s.ctx.Done()
}

// rcaChunkWriter implements io.Writer and forwards each Write call as an
// SSE event. It is used to bridge analysis.RunRCA's io.Writer interface
// with the SSE stream.
type rcaChunkWriter struct {
	sse *SSEWriter
}

// Write sends the given bytes as a single "rca-chunk" SSE event.
func (w *rcaChunkWriter) Write(p []byte) (int, error) {
	if err := w.sse.SendEvent("rca-chunk", string(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// chatChunkWriter implements io.Writer and forwards each Write call as a
// "chat-chunk" SSE event, bridging analysis.RunChatStream with the SSE stream.
type chatChunkWriter struct {
	sse *SSEWriter
}

// Write sends the given bytes as a single "chat-chunk" SSE event.
func (w *chatChunkWriter) Write(p []byte) (int, error) {
	if err := w.sse.SendEvent("chat-chunk", string(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}
