package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"
)

type MemoryEntry struct {
	ID        string    `json:"id"`
	Category  string    `json:"category"`
	Content   string    `json:"content"`
	Goal      string    `json:"goal,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ProjectMemory struct {
	Workspace    string        `json:"workspace"`
	WorkspaceKey string        `json:"workspace_key"`
	UpdatedAt    time.Time     `json:"updated_at"`
	Entries      []MemoryEntry `json:"entries"`
}

type ProjectMemoryStore struct {
	memoryDir string
}

func NewProjectMemoryStore(baseDir string) (*ProjectMemoryStore, error) {
	if baseDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".celeste")
	}

	memoryDir := filepath.Join(baseDir, "agent", "memory")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return nil, fmt.Errorf("create memory dir: %w", err)
	}

	return &ProjectMemoryStore{memoryDir: memoryDir}, nil
}

func (s *ProjectMemoryStore) Load(workspace string) (*ProjectMemory, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	normalizedWorkspace := normalizeWorkspacePath(workspace)
	workspaceKey := workspaceMemoryKey(normalizedWorkspace)
	path := filepath.Join(s.memoryDir, workspaceKey+".json")

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ProjectMemory{
				Workspace:    normalizedWorkspace,
				WorkspaceKey: workspaceKey,
				Entries:      []MemoryEntry{},
			}, nil
		}
		return nil, fmt.Errorf("read project memory: %w", err)
	}

	var memory ProjectMemory
	if err := json.Unmarshal(data, &memory); err != nil {
		return nil, fmt.Errorf("parse project memory: %w", err)
	}
	if memory.Workspace == "" {
		memory.Workspace = normalizedWorkspace
	}
	if memory.WorkspaceKey == "" {
		memory.WorkspaceKey = workspaceKey
	}
	if memory.Entries == nil {
		memory.Entries = []MemoryEntry{}
	}
	return &memory, nil
}

func (s *ProjectMemoryStore) Save(memory *ProjectMemory) error {
	if memory == nil {
		return fmt.Errorf("project memory is nil")
	}
	if strings.TrimSpace(memory.Workspace) == "" {
		return fmt.Errorf("workspace is required")
	}

	memory.Workspace = normalizeWorkspacePath(memory.Workspace)
	if strings.TrimSpace(memory.WorkspaceKey) == "" {
		memory.WorkspaceKey = workspaceMemoryKey(memory.Workspace)
	}
	if memory.Entries == nil {
		memory.Entries = []MemoryEntry{}
	}

	data, err := json.MarshalIndent(memory, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal project memory: %w", err)
	}
	path := filepath.Join(s.memoryDir, memory.WorkspaceKey+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write project memory: %w", err)
	}
	return nil
}

func (s *ProjectMemoryStore) Append(workspace string, entries []MemoryEntry, maxEntries int) (*ProjectMemory, error) {
	memory, err := s.Load(workspace)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return memory, nil
	}

	now := time.Now()
	indexByFingerprint := make(map[string]int, len(memory.Entries))
	for i := range memory.Entries {
		indexByFingerprint[memoryEntryFingerprint(memory.Entries[i])] = i
	}

	for _, entry := range entries {
		entry.Content = strings.TrimSpace(entry.Content)
		if entry.Content == "" {
			continue
		}
		entry.Category = normalizeMemoryCategory(entry.Category)
		if entry.ID == "" {
			entry.ID = generateMemoryEntryID(now, len(memory.Entries))
		}
		if entry.CreatedAt.IsZero() {
			entry.CreatedAt = now
		}
		entry.UpdatedAt = now

		fingerprint := memoryEntryFingerprint(entry)
		if idx, ok := indexByFingerprint[fingerprint]; ok {
			existing := memory.Entries[idx]
			entry.ID = existing.ID
			entry.CreatedAt = existing.CreatedAt
			memory.Entries[idx] = entry
			continue
		}

		memory.Entries = append(memory.Entries, entry)
		indexByFingerprint[fingerprint] = len(memory.Entries) - 1
	}

	sortMemoryEntries(memory.Entries)
	if maxEntries <= 0 {
		maxEntries = DefaultOptions().MemoryMaxEntries
	}
	if len(memory.Entries) > maxEntries {
		memory.Entries = memory.Entries[:maxEntries]
	}
	memory.UpdatedAt = now
	if err := s.Save(memory); err != nil {
		return nil, err
	}
	return memory, nil
}

func (m *ProjectMemory) Recall(query string, limit int) []MemoryEntry {
	if m == nil || len(m.Entries) == 0 {
		return nil
	}
	if limit <= 0 {
		limit = DefaultOptions().MemoryRecallLimit
	}

	query = strings.TrimSpace(query)
	if query == "" {
		result := append([]MemoryEntry(nil), m.Entries...)
		sortMemoryEntries(result)
		if len(result) > limit {
			result = result[:limit]
		}
		return result
	}

	type scoredEntry struct {
		entry MemoryEntry
		score int
	}

	queryLower := strings.ToLower(query)
	queryTokens := tokenizeForMatch(queryLower)
	scored := make([]scoredEntry, 0, len(m.Entries))

	for _, entry := range m.Entries {
		score := scoreMemoryEntry(entry, queryLower, queryTokens)
		if score <= 0 {
			continue
		}
		scored = append(scored, scoredEntry{entry: entry, score: score})
	}

	if len(scored) == 0 {
		fallback := append([]MemoryEntry(nil), m.Entries...)
		sortMemoryEntries(fallback)
		if len(fallback) > limit {
			fallback = fallback[:limit]
		}
		return fallback
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score == scored[j].score {
			return scored[i].entry.UpdatedAt.After(scored[j].entry.UpdatedAt)
		}
		return scored[i].score > scored[j].score
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	result := make([]MemoryEntry, 0, len(scored))
	for _, item := range scored {
		result = append(result, item.entry)
	}
	return result
}

func normalizeWorkspacePath(workspace string) string {
	trimmed := strings.TrimSpace(workspace)
	if trimmed == "" {
		return ""
	}
	if abs, err := filepath.Abs(trimmed); err == nil {
		return filepath.Clean(abs)
	}
	return filepath.Clean(trimmed)
}

func workspaceMemoryKey(workspace string) string {
	hash := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(workspace))))
	return hex.EncodeToString(hash[:16])
}

func memoryEntryFingerprint(entry MemoryEntry) string {
	return normalizeMemoryCategory(entry.Category) + "\n" + strings.ToLower(strings.Join(strings.Fields(entry.Content), " "))
}

func normalizeMemoryCategory(category string) string {
	category = strings.TrimSpace(strings.ToLower(category))
	if category == "" {
		return "note"
	}
	return category
}

func generateMemoryEntryID(now time.Time, n int) string {
	return fmt.Sprintf("%s-%03d", now.UTC().Format("20060102T150405.000000000"), n%1000)
}

func sortMemoryEntries(entries []MemoryEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].UpdatedAt.Equal(entries[j].UpdatedAt) {
			return entries[i].CreatedAt.After(entries[j].CreatedAt)
		}
		return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
	})
}

func tokenizeForMatch(input string) []string {
	if input == "" {
		return nil
	}
	tokens := make([]string, 0, 8)
	seen := map[string]struct{}{}
	var b strings.Builder
	flush := func() {
		if b.Len() == 0 {
			return
		}
		token := b.String()
		b.Reset()
		if len(token) < 2 {
			return
		}
		if _, ok := seen[token]; ok {
			return
		}
		seen[token] = struct{}{}
		tokens = append(tokens, token)
	}

	for _, r := range input {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			continue
		}
		flush()
	}
	flush()
	return tokens
}

func scoreMemoryEntry(entry MemoryEntry, queryLower string, queryTokens []string) int {
	text := strings.ToLower(strings.TrimSpace(strings.Join([]string{
		entry.Category,
		entry.Content,
		entry.Goal,
		entry.Status,
	}, " ")))
	if text == "" {
		return 0
	}

	score := 0
	if queryLower != "" && strings.Contains(text, queryLower) {
		score += 5
	}
	for _, token := range queryTokens {
		if strings.Contains(text, token) {
			score++
		}
	}
	switch entry.Category {
	case "blocker", "failure", "verification_failure":
		score += 1
	}
	if !entry.UpdatedAt.IsZero() && time.Since(entry.UpdatedAt) <= 7*24*time.Hour {
		score += 1
	}
	return score
}
