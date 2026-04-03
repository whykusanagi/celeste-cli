package sessions

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Manager coordinates session writing, listing, and auto-resume for a project.
type Manager struct {
	writer     *SessionWriter
	sessionDir string
	projectID  string // hash of git root or cwd
	sessionID  string
}

// NewManager creates a Manager scoped to the project that contains cwd.
// The projectID is derived from the git repository root (if available) or the
// cwd itself, hashed to a short hex string.
func NewManager(cwd string) (*Manager, error) {
	pid := projectHash(cwd)

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("sessions: home dir: %w", err)
	}
	sessDir := filepath.Join(homeDir, ".celeste", "sessions", pid)

	return &Manager{
		sessionDir: sessDir,
		projectID:  pid,
	}, nil
}

// StartSession creates a new JSONL log file and sets up the writer.
func (m *Manager) StartSession() error {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	w, err := NewSessionWriter(m.sessionDir, id)
	if err != nil {
		return err
	}
	m.writer = w
	m.sessionID = id
	return nil
}

// LogTurn writes a conversation turn. role should be one of the LogEntry.Type
// values: "user", "assistant", "tool_call", "tool_result", "system".
func (m *Manager) LogTurn(role, content string) error {
	if m.writer == nil {
		return fmt.Errorf("sessions: no active session")
	}
	return m.writer.WriteEntry(LogEntry{
		Type:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
}

// GetRecentSession returns the most recently modified session that is younger
// than maxAge, or nil if none qualifies.
func (m *Manager) GetRecentSession(maxAge time.Duration) (*SessionInfo, error) {
	sessions, err := ListSessions(m.sessionDir)
	if err != nil {
		return nil, err
	}
	if len(sessions) == 0 {
		return nil, nil
	}
	// sessions are already sorted newest-first by ListSessions.
	newest := sessions[0]
	if time.Since(newest.UpdatedAt) > maxAge {
		return nil, nil
	}
	return &newest, nil
}

// ResumeSession loads entries from an existing JSONL session and opens a new
// writer that appends to it.
func (m *Manager) ResumeSession(id string) ([]LogEntry, error) {
	p := filepath.Join(m.sessionDir, id+".jsonl")
	entries, err := ReadSession(p)
	if err != nil {
		return nil, err
	}

	w, err := NewSessionWriter(m.sessionDir, id)
	if err != nil {
		return nil, err
	}
	m.writer = w
	m.sessionID = id
	return entries, nil
}

// ListSessions returns all sessions for this project.
func (m *Manager) ListSessions() ([]SessionInfo, error) {
	return ListSessions(m.sessionDir)
}

// SessionID returns the current session ID, or empty if none is active.
func (m *Manager) SessionID() string {
	return m.sessionID
}

// SessionDir returns the project-specific session directory.
func (m *Manager) SessionDir() string {
	return m.sessionDir
}

// ProjectID returns the computed project hash.
func (m *Manager) ProjectID() string {
	return m.projectID
}

// Close flushes and closes the writer.
func (m *Manager) Close() error {
	if m.writer == nil {
		return nil
	}
	return m.writer.Close()
}

// projectHash returns a short hex hash identifying the project root.
func projectHash(cwd string) string {
	root := gitRoot(cwd)
	if root == "" {
		root = cwd
	}
	h := sha256.Sum256([]byte(root))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars — unique enough
}

// gitRoot returns the git repository root for cwd, or "" if not in a repo.
func gitRoot(cwd string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
