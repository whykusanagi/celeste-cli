package sessions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionInfo summarises a JSONL session file without loading every entry.
type SessionInfo struct {
	ID         string
	Path       string
	Title      string // derived from first user message
	EntryCount int
	CreatedAt  time.Time
	UpdatedAt  time.Time // file mtime
}

// ReadSession reads all entries from a JSONL file. Malformed lines are
// silently skipped so that a partially-corrupt file can still be recovered.
func ReadSession(path string) ([]LogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("sessions: open: %w", err)
	}
	defer f.Close()

	var entries []LogEntry
	scanner := bufio.NewScanner(f)
	// Allow lines up to 1 MB (tool results can be large).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			// Skip malformed lines — corruption recovery.
			continue
		}
		entries = append(entries, entry)
	}
	if err := scanner.Err(); err != nil {
		return entries, fmt.Errorf("sessions: scan: %w", err)
	}
	return entries, nil
}

// ListSessions scans sessionDir for .jsonl files and returns metadata sorted
// by modification time (newest first).
func ListSessions(sessionDir string) ([]SessionInfo, error) {
	pattern := filepath.Join(sessionDir, "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("sessions: glob: %w", err)
	}

	infos := make([]SessionInfo, 0, len(matches))
	for _, p := range matches {
		info, err := inspectSession(p)
		if err != nil {
			continue // skip unreadable files
		}
		infos = append(infos, info)
	}

	// Newest first.
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].UpdatedAt.After(infos[j].UpdatedAt)
	})

	return infos, nil
}

// inspectSession builds a SessionInfo by reading the file header (first user
// message) and stat-ing the file.
func inspectSession(path string) (SessionInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return SessionInfo{}, err
	}

	id := strings.TrimSuffix(filepath.Base(path), ".jsonl")

	si := SessionInfo{
		ID:        id,
		Path:      path,
		UpdatedAt: fi.ModTime(),
	}

	// Scan file to get entry count, first timestamp, and title.
	f, err := os.Open(path)
	if err != nil {
		return si, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		si.EntryCount++

		// CreatedAt = timestamp of the first entry.
		if si.EntryCount == 1 {
			si.CreatedAt = entry.Timestamp
		}

		// Title = content of the first "user" entry (truncated).
		if si.Title == "" && entry.Type == "user" {
			si.Title = truncateTitle(entry.Content, 60)
		}
	}

	return si, nil
}

// truncateTitle shortens s to maxLen, breaking at a word boundary.
func truncateTitle(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	s = s[:maxLen]
	if idx := strings.LastIndex(s, " "); idx > 0 {
		s = s[:idx]
	}
	return s + "..."
}
