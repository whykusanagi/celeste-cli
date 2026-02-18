// Package config provides configuration management for Celeste CLI.
// This file handles session persistence (conversation history).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
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
}

// SessionMessage represents a message in a session.
type SessionMessage struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
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
	os.MkdirAll(sessionsDir, 0755)

	return &SessionManager{
		sessionsDir: sessionsDir,
	}
}

// NewSession creates a new session with a unique ID.
func (m *SessionManager) NewSession() *Session {
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	m.currentID = id

	return &Session{
		ID:        id,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages:  []SessionMessage{},
		Metadata:  make(map[string]any),
	}
}

// Save saves a session to disk.
func (m *SessionManager) Save(session *Session) error {
	session.UpdatedAt = time.Now()
	session.TokenCount = EstimateSessionTokens(session)

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	path := filepath.Join(m.sessionsDir, session.ID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
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

// SetProvider stores the provider name in the session.
func (s *Session) SetProvider(provider string) {
	s.Provider = provider
}

// GetProvider retrieves the provider name from the session.
func (s *Session) GetProvider() string {
	return s.Provider
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

// SetNSFWMode stores the NSFW mode in session.
func (s *Session) SetNSFWMode(enabled bool) {
	s.NSFWMode = enabled
}

// GetNSFWMode retrieves the NSFW mode from session.
func (s *Session) GetNSFWMode() bool {
	return s.NSFWMode
}

// GetMessagesForLLM converts session messages to a format suitable for LLM.
func GetMessagesForLLM(session *Session) []map[string]string {
	var result []map[string]string
	for _, msg := range session.Messages {
		result = append(result, map[string]string{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}
	return result
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

// GetMessagesWithLimit returns messages with token limit applied.
func (s *Session) GetMessagesWithLimit(systemPromptTokens int) []SessionMessage {
	return TruncateToLimit(s.Messages, s.Model, systemPromptTokens)
}

// SetName updates the session name.
func (s *Session) SetName(name string) {
	s.Name = name
	s.UpdatedAt = time.Now()
}

// MergeSessions combines messages from two sessions chronologically.
func (m *SessionManager) MergeSessions(session1, session2 *Session) *Session {
	merged := &Session{
		ID:        fmt.Sprintf("%d", time.Now().UnixNano()),
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
	sort.Slice(allMessages, func(i, j int) bool {
		return allMessages[i].Timestamp.Before(allMessages[j].Timestamp)
	})

	merged.Messages = allMessages
	merged.TokenCount = EstimateSessionTokens(merged)

	return merged
}

// --- TUI Interface Compatibility Methods ---
// These methods use interface{} to avoid circular imports with the TUI package.

// NewSessionRaw returns a new session as interface{}.
func (m *SessionManager) NewSessionRaw() interface{} {
	return m.NewSession()
}

// SaveRaw saves a session (accepts interface{}).
func (m *SessionManager) SaveRaw(session interface{}) error {
	if s, ok := session.(*Session); ok {
		return m.Save(s)
	}
	return fmt.Errorf("invalid session type")
}

// LoadRaw loads a session by ID (returns interface{}).
func (m *SessionManager) LoadRaw(id string) (interface{}, error) {
	return m.Load(id)
}

// ListRaw lists all sessions (returns []interface{}).
func (m *SessionManager) ListRaw() ([]interface{}, error) {
	sessions, err := m.List()
	if err != nil {
		return nil, err
	}
	result := make([]interface{}, len(sessions))
	for i := range sessions {
		result[i] = &sessions[i]
	}
	return result, nil
}

// MergeSessionsRaw merges two sessions (accepts and returns interface{}).
func (m *SessionManager) MergeSessionsRaw(session1, session2 interface{}) interface{} {
	s1, ok1 := session1.(*Session)
	s2, ok2 := session2.(*Session)
	if !ok1 || !ok2 {
		return nil
	}
	return m.MergeSessions(s1, s2)
}
