// Package sessions provides JSONL-based conversation logging for session
// persistence and resume. It operates alongside the existing JSON session
// system in config/session.go — that system tracks config state (endpoint,
// model, NSFW mode) while this package handles append-only conversation replay.
package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LogEntry represents a single line in the JSONL session log.
type LogEntry struct {
	Type      string    `json:"type"`                 // "user", "assistant", "tool_call", "tool_result", "system"
	Content   string    `json:"content"`
	Role      string    `json:"role,omitempty"`
	ToolName  string    `json:"tool_name,omitempty"`
	ToolID    string    `json:"tool_id,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ephemeralTypes lists entry types that should never be persisted.
var ephemeralTypes = map[string]bool{
	"progress": true,
	"typing":   true,
	"ping":     true,
}

// SessionWriter appends LogEntry records to a JSONL file.
type SessionWriter struct {
	file    *os.File
	encoder *json.Encoder
	path    string
	mu      sync.Mutex
}

// NewSessionWriter creates a writer that appends to
// <sessionDir>/<sessionID>.jsonl, creating parent directories as needed.
func NewSessionWriter(sessionDir string, sessionID string) (*SessionWriter, error) {
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return nil, fmt.Errorf("sessions: create dir: %w", err)
	}

	p := filepath.Join(sessionDir, sessionID+".jsonl")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("sessions: open file: %w", err)
	}

	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false) // keep content readable

	return &SessionWriter{
		file:    f,
		encoder: enc,
		path:    p,
	}, nil
}

// WriteEntry appends a single JSON line. Ephemeral types (progress, typing,
// ping) are silently dropped.
func (w *SessionWriter) WriteEntry(entry LogEntry) error {
	if ephemeralTypes[entry.Type] {
		return nil
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.encoder.Encode(entry); err != nil {
		return fmt.Errorf("sessions: encode entry: %w", err)
	}
	// Flush to disk so partial crashes don't lose the last turn.
	return w.file.Sync()
}

// Close flushes and closes the underlying file.
func (w *SessionWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

// Path returns the absolute path of the JSONL file.
func (w *SessionWriter) Path() string {
	return w.path
}
