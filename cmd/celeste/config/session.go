// Package config provides configuration management for Celeste CLI.
// This file handles session persistence (conversation history).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/clock"
)

// Session represents a saved conversation session.
type Session struct {
	ID         string           `json:"id"`
	Name       string           `json:"name,omitempty"`
	CreatedAt  time.Time        `json:"created_at"`
	UpdatedAt  time.Time        `json:"updated_at"`
	Messages   []SessionMessage `json:"messages"`
	NSFWMode   bool             `json:"nsfw_mode,omitempty"`
	Metadata   map[string]any   `json:"metadata,omitempty"`
	TokenCount int              `json:"token_count,omitempty"` // Estimated token count
	Model      string           `json:"model,omitempty"`       // Track model for limits

	// NEW: Enhanced tracking
	UsageMetrics *UsageMetrics `json:"usage_metrics,omitempty"` // Detailed usage tracking
	Provider     string        `json:"provider,omitempty"`      // Provider (openai, venice, etc)
	MaxContext   int           `json:"max_context,omitempty"`   // Model's max context window

	// Workspace is the absolute directory the chat ran in (2.0 W4 ruling
	// 1): `celeste resume` and /session list show this project's
	// sessions first. Older sessions have none.
	Workspace string `json:"workspace,omitempty"`
}

// SessionMessage represents a message in a session.
type SessionMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`

	// Tool traffic (#174): without it a resumed session replays only the
	// prose, so the model loses what it read and did.
	ToolCalls  []SessionToolCall `json:"tool_calls,omitempty"`   // assistant: calls made
	ToolCallID string            `json:"tool_call_id,omitempty"` // tool: call answered
	Name       string            `json:"name,omitempty"`         // tool: function name

	// Hidden messages are sent to the model but not rendered (directives,
	// compaction summaries). Compacted messages were replaced by a summary:
	// rendered in the scrollback but no longer sent.
	Hidden    bool `json:"hidden,omitempty"`
	Compacted bool `json:"compacted,omitempty"`

	// ProviderBlocks is the message as the provider returned it (2.0 F3),
	// saved only while it still matches Content and ToolCalls.
	ProviderBlocks *ProviderBlocks `json:"provider_blocks,omitempty"`
}

// SessionToolCall is a tool call recorded on an assistant message.
type SessionToolCall struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Arguments        string `json:"arguments"`
	ThoughtSignature []byte `json:"thought_signature,omitempty"`
}

// GenerateNameFromMessage creates a session name from first user message.
// Extracts first 40-50 chars, intelligently truncates at word boundary.
func GenerateNameFromMessage(content string) string {
	// Remove newlines, trim spaces
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.TrimSpace(content)

	if len(content) == 0 {
		return "Untitled Session"
	}

	// Truncate at 50 chars, find last space to avoid cutting words
	if len(content) > 50 {
		content = content[:50]
		if idx := strings.LastIndex(content, " "); idx > 0 {
			content = content[:idx]
		}
		content += "..."
	}

	return content
}

// SessionManager manages session persistence.
type SessionManager struct {
	sessionsDir string
	currentID   string
}

// NewSessionManager creates a new session manager.
func NewSessionManager() *SessionManager {
	homeDir, _ := os.UserHomeDir()
	sessionsDir := filepath.Join(homeDir, ".celeste", "sessions")
	os.MkdirAll(sessionsDir, 0700)
	_ = os.Chmod(sessionsDir, 0700) // tighten a directory an older version made 0755

	return &SessionManager{
		sessionsDir: sessionsDir,
	}
}

// UniqueNanoID is the current time in nanoseconds from clock.Now, which
// never repeats in this process: Windows' clock is coarse, and two sessions
// with one ID overwrite each other's file.
func UniqueNanoID() string {
	return strconv.FormatInt(sessionClock().UnixNano(), 10)
}

// sessionClock is the clock UniqueNanoID reads; tests replace it to know
// the next ID.
var sessionClock = clock.Now

// newSessionID is a UniqueNanoID no session file in the directory has yet:
// another celeste process may have used it on the same clock tick. Two
// processes that both pick an ID before either saves can still collide;
// with nanosecond IDs that takes the same tick in both.
func (m *SessionManager) newSessionID() string {
	id := UniqueNanoID()
	for m.sessionsDir != "" && fileExists(filepath.Join(m.sessionsDir, id+".json")) {
		id = UniqueNanoID()
	}
	return id
}

// NewSession creates a new session with a unique ID.
func (m *SessionManager) NewSession() *Session {
	id := m.newSessionID()
	m.currentID = id

	return &Session{
		ID:        id,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages:  []SessionMessage{},
		Metadata:  make(map[string]any),
	}
}

// validSessionID rejects an id that is not a plain file name: a user's
// /session delete ../config must not reach outside the sessions directory.
func validSessionID(id string) error {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id ||
		strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("invalid session id %q", id)
	}
	return nil
}

// Save saves a session to disk.
func (m *SessionManager) Save(session *Session) error {
	if err := validSessionID(session.ID); err != nil {
		return err
	}
	session.UpdatedAt = time.Now()
	session.TokenCount = EstimateSessionTokens(session)

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	path := filepath.Join(m.sessionsDir, session.ID+".json")
	// Atomic and exactly 0600, also over a file an older version wrote 0644.
	if err := atomicfile.Write(path, data, 0600); err != nil {
		return err
	}

	// Update global analytics with this session's data
	analytics, err := LoadGlobalAnalytics()
	if err == nil && session.UsageMetrics != nil {
		analytics.UpdateFromSession(session)
		// Ignore errors from analytics save to not block session save
		_ = analytics.Save()
	}

	return nil
}

// Load loads a session by ID.
func (m *SessionManager) Load(id string) (*Session, error) {
	if err := validSessionID(id); err != nil {
		return nil, err
	}
	path := filepath.Join(m.sessionsDir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read session: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to parse session: %w", err)
	}

	// Auto-generate name for old sessions that don't have one
	if session.Name == "" && len(session.Messages) > 0 {
		for _, msg := range session.Messages {
			if msg.Role == "user" {
				session.Name = GenerateNameFromMessage(msg.Content)
				// Save the session with the new name
				_ = m.Save(&session) // Error intentionally ignored - name generation is best-effort
				break
			}
		}
	}

	m.currentID = id
	return &session, nil
}

// LoadSession is a global helper to load a session by numeric ID
func LoadSession(sessionID int64) (*Session, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	sessionsDir := filepath.Join(homeDir, ".celeste", "sessions")
	filename := fmt.Sprintf("%d.json", sessionID)
	path := filepath.Join(sessionsDir, filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read session: %w", err)
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("failed to parse session: %w", err)
	}

	return &session, nil
}

// LoadLatest loads the most recent session.
func (m *SessionManager) LoadLatest() (*Session, error) {
	sessions, err := m.List()
	if err != nil {
		return nil, err
	}

	if len(sessions) == 0 {
		return nil, fmt.Errorf("no sessions found")
	}

	// Sort by updated time (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.After(sessions[j].UpdatedAt)
	})

	return m.Load(sessions[0].ID)
}

// List returns all saved sessions.
func (m *SessionManager) List() ([]Session, error) {
	files, err := filepath.Glob(filepath.Join(m.sessionsDir, "*.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions: %w", err)
	}

	var sessions []Session
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		var session Session
		if err := json.Unmarshal(data, &session); err != nil {
			continue
		}

		sessions = append(sessions, session)
	}

	return sessions, nil
}

// Delete deletes a session by ID.
func (m *SessionManager) Delete(id string) error {
	if err := validSessionID(id); err != nil {
		return err
	}
	path := filepath.Join(m.sessionsDir, id+".json")
	return os.Remove(path)
}

// Clear deletes all sessions.
func (m *SessionManager) Clear() error {
	files, err := filepath.Glob(filepath.Join(m.sessionsDir, "*.json"))
	if err != nil {
		return err
	}

	for _, file := range files {
		os.Remove(file)
	}

	return nil
}

// GetCurrentID returns the current session ID.
func (m *SessionManager) GetCurrentID() string {
	return m.currentID
}

// AddMessage adds a message to the session and saves.
func (m *SessionManager) AddMessage(session *Session, role, content string) {
	// Auto-generate name from first user message
	if len(session.Messages) == 0 && role == "user" && session.Name == "" {
		session.Name = GenerateNameFromMessage(content)
	}

	session.Messages = append(session.Messages, SessionMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
	session.UpdatedAt = time.Now()
}

// AddMessageWithTokens adds a message to the session with token tracking.
func (m *SessionManager) AddMessageWithTokens(session *Session, role, content string, inputTokens, outputTokens int) {
	// Auto-generate name from first user message
	if len(session.Messages) == 0 && role == "user" && session.Name == "" {
		session.Name = GenerateNameFromMessage(content)
	}

	// Add the message
	session.Messages = append(session.Messages, SessionMessage{
		Role:      role,
		Content:   content,
		Timestamp: time.Now(),
	})
	session.UpdatedAt = time.Now()

	// Initialize UsageMetrics if needed
	if session.UsageMetrics == nil {
		session.UsageMetrics = NewUsageMetrics()
	}

	// Update usage metrics with token counts
	if inputTokens > 0 || outputTokens > 0 {
		session.UsageMetrics.Update(inputTokens, outputTokens, session.Model)
	}

	// Increment message count
	session.UsageMetrics.IncrementMessageCount()
}

// UpdateUsageMetrics updates the session's usage metrics with new token data.
func (s *Session) UpdateUsageMetrics(inputTokens, outputTokens int) {
	if s.UsageMetrics == nil {
		s.UsageMetrics = NewUsageMetrics()
	}
	s.UsageMetrics.Update(inputTokens, outputTokens, s.Model)
}

// InitializeUsageMetrics ensures the session has usage metrics initialized.
func (s *Session) InitializeUsageMetrics() {
	if s.UsageMetrics == nil {
		s.UsageMetrics = NewUsageMetrics()
	}
}

// ClearMessages clears all messages from the session.
func (s *Session) ClearMessages() {
	s.Messages = []SessionMessage{}
	s.UpdatedAt = time.Now()
}

// GetMessages returns all session messages.
func (s *Session) GetMessages() []SessionMessage {
	return s.Messages
}

// GetMessagesRaw returns messages as interface{} (for TUI interface compatibility).
func (s *Session) GetMessagesRaw() interface{} {
	return s.Messages
}

// SetMessagesRaw sets messages from interface{} (for TUI interface compatibility).
func (s *Session) SetMessagesRaw(msgs interface{}) {
	if sessionMsgs, ok := msgs.([]SessionMessage); ok {
		s.Messages = sessionMsgs
		s.UpdatedAt = time.Now()
	}
}

// SummarizeRaw returns summary as interface{} (for TUI interface compatibility).
func (s *Session) SummarizeRaw() interface{} {
	return s.Summarize()
}

// SetEndpoint stores the current endpoint in session metadata.
func (s *Session) SetEndpoint(endpoint string) {
	if s.Metadata == nil {
		s.Metadata = make(map[string]any)
	}
	s.Metadata["endpoint"] = endpoint
	// Also set the Provider field to match the endpoint
	s.Provider = endpoint
}

// GetEndpoint retrieves the endpoint from session metadata.
func (s *Session) GetEndpoint() string {
	if s.Metadata == nil {
		return ""
	}
	if endpoint, ok := s.Metadata["endpoint"].(string); ok {
		return endpoint
	}
	return ""
}

// GetProvider retrieves the provider name from the session.
func (s *Session) GetProvider() string {
	return s.Provider
}

// SetCommandHistory stores the input command history in session metadata.
func (s *Session) SetCommandHistory(history []string) {
	if s.Metadata == nil {
		s.Metadata = make(map[string]any)
	}
	s.Metadata["command_history"] = history
}

// GetCommandHistory retrieves the input command history from session metadata.
func (s *Session) GetCommandHistory() []string {
	if s.Metadata == nil {
		return nil
	}
	raw, ok := s.Metadata["command_history"]
	if !ok {
		return nil
	}
	// JSON round-trip stores []string as []interface{}
	switch v := raw.(type) {
	case []string:
		return v
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// SetModel stores the current model in session metadata.
func (s *Session) SetModel(model string) {
	if s.Metadata == nil {
		s.Metadata = make(map[string]any)
	}
	s.Metadata["model"] = model
}

// GetModel retrieves the model from session metadata.
func (s *Session) GetModel() string {
	if s.Metadata == nil {
		return ""
	}
	if model, ok := s.Metadata["model"].(string); ok {
		return model
	}
	return ""
}

// SetModelPinned records a /set-model --force pin, so a resume keeps the
// model instead of resolving it.
func (s *Session) SetModelPinned(pinned bool) {
	if s.Metadata == nil {
		s.Metadata = make(map[string]any)
	}
	if pinned {
		s.Metadata["model_pinned"] = true
	} else {
		delete(s.Metadata, "model_pinned")
	}
}

// GetModelPinned reports a saved /set-model --force pin.
func (s *Session) GetModelPinned() bool {
	pinned, _ := s.Metadata["model_pinned"].(bool)
	return pinned
}

// SetModelUnverified records that the session's model is a /set-model
// --force name no catalog listed, so a resume does not show it as valid.
func (s *Session) SetModelUnverified(unverified bool) {
	if s.Metadata == nil {
		s.Metadata = make(map[string]any)
	}
	if unverified {
		s.Metadata["model_unverified"] = true
	} else {
		delete(s.Metadata, "model_unverified")
	}
}

// GetModelUnverified reports what SetModelUnverified stored.
func (s *Session) GetModelUnverified() bool {
	unverified, _ := s.Metadata["model_unverified"].(bool)
	return unverified
}

// SetNSFWMode stores the NSFW mode in session.
func (s *Session) SetNSFWMode(enabled bool) {
	s.NSFWMode = enabled
}

// GetNSFWMode retrieves the NSFW mode from session.
func (s *Session) GetNSFWMode() bool {
	return s.NSFWMode
}

// SessionSummary provides a brief overview of a session.
type SessionSummary struct {
	ID           string         `json:"id"`
	Name         string         `json:"name,omitempty"`
	MessageCount int            `json:"message_count"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	FirstMessage string         `json:"first_message,omitempty"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	Workspace    string         `json:"workspace,omitempty"`
}

// Summarize returns a summary of the session.
func (s *Session) Summarize() SessionSummary {
	summary := SessionSummary{
		ID:           s.ID,
		Name:         s.Name,
		MessageCount: len(s.Messages),
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		Metadata:     s.Metadata,
		Workspace:    s.Workspace,
	}

	// Get first user message as preview
	for _, msg := range s.Messages {
		if msg.Role == "user" {
			preview := msg.Content
			if len(preview) > 50 {
				// Intelligently truncate at word boundary
				preview = preview[:50]
				if idx := strings.LastIndex(preview, " "); idx > 0 {
					preview = preview[:idx]
				}
				preview += "..."
			}
			summary.FirstMessage = preview
			break
		}
	}

	return summary
}

// SetWorkspace records the directory the session runs in.
func (s *Session) SetWorkspace(ws string) { s.Workspace = ws }

// GetWorkspace is the directory the session ran in ("" for older sessions).
func (s *Session) GetWorkspace() string { return s.Workspace }

// projectRoot is dir's git root, or dir itself (absolute, cleaned)
// outside a repository.
func projectRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return filepath.Clean(dir)
	}
	if root, ok := grimoire.GitRoot(abs); ok {
		return root
	}
	return abs
}

// SameProject reports whether two workspaces are the same project: the
// same git root, or the same directory when neither is in a repository
// (2.0 W4 ruling 1). An empty workspace is no project.
func SameProject(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return projectRoot(a) == projectRoot(b)
}

// SortForWorkspace splits sessions into ws's project's and the rest, each
// newest first (2.0 W4 ruling 2).
func SortForWorkspace(sessions []Session, ws string) (mine, others []Session) {
	var root string
	if ws != "" {
		root = projectRoot(ws)
	}
	roots := map[string]string{}
	for _, s := range sessions {
		if root != "" && s.Workspace != "" {
			r, ok := roots[s.Workspace]
			if !ok {
				r = projectRoot(s.Workspace)
				roots[s.Workspace] = r
			}
			if r == root {
				mine = append(mine, s)
				continue
			}
		}
		others = append(others, s)
	}
	newest := func(ss []Session) {
		sort.SliceStable(ss, func(i, j int) bool { return ss[i].UpdatedAt.After(ss[j].UpdatedAt) })
	}
	newest(mine)
	newest(others)
	return mine, others
}

// SetName updates the session name.
func (s *Session) SetName(name string) {
	s.Name = name
	s.UpdatedAt = time.Now()
}

// MergeSessions combines messages from two sessions chronologically.
func (m *SessionManager) MergeSessions(session1, session2 *Session) *Session {
	merged := &Session{
		ID:        m.newSessionID(),
		Name:      fmt.Sprintf("%s + %s", session1.Name, session2.Name),
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages:  []SessionMessage{},
		NSFWMode:  session1.NSFWMode, // Inherit from primary
		Metadata:  make(map[string]any),
		Model:     session1.Model,
	}

	// Combine messages from both sessions
	allMessages := append([]SessionMessage{}, session1.Messages...)
	allMessages = append(allMessages, session2.Messages...)

	// Sort by timestamp
	sort.SliceStable(allMessages, func(i, j int) bool {
		return allMessages[i].Timestamp.Before(allMessages[j].Timestamp)
	})

	merged.Messages = allMessages
	merged.TokenCount = EstimateSessionTokens(merged)

	return merged
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
