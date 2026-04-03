package grimoire

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxStatusBytes = 2048

// GitSnapshot captures the current git state for system prompt injection.
type GitSnapshot struct {
	Branch        string
	MainBranch    string
	Status        string // short status output
	RecentCommits string // last 5 commits oneline
	UserName      string
}

// CaptureGitSnapshot runs 5 git commands in parallel and returns the snapshot.
// Returns nil if not in a git repo. Truncates status to 2KB max.
func CaptureGitSnapshot(workDir string) *GitSnapshot {
	type kv struct {
		key string
		val string
	}

	commands := []struct {
		key  string
		args []string
	}{
		{"branch", []string{"git", "rev-parse", "--abbrev-ref", "HEAD"}},
		{"main", []string{"git", "symbolic-ref", "refs/remotes/origin/HEAD"}},
		{"status", []string{"git", "status", "--short"}},
		{"log", []string{"git", "log", "--oneline", "-n", "5"}},
		{"user", []string{"git", "config", "user.name"}},
	}

	results := make([]kv, len(commands))
	var wg sync.WaitGroup

	for i, c := range commands {
		wg.Add(1)
		go func(idx int, key string, args []string) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, args[0], args[1:]...)
			cmd.Dir = workDir
			out, err := cmd.Output()
			if err != nil {
				results[idx] = kv{key, ""}
				return
			}
			results[idx] = kv{key, strings.TrimSpace(string(out))}
		}(i, c.key, c.args)
	}
	wg.Wait()

	vals := make(map[string]string, len(results))
	for _, r := range results {
		vals[r.key] = r.val
	}

	// If we can't determine the branch, we're not in a git repo.
	if vals["branch"] == "" {
		return nil
	}

	status := vals["status"]
	if len(status) > maxStatusBytes {
		status = status[:maxStatusBytes] + "\n... (truncated)"
	}

	mainBranch := vals["main"]
	// Parse "refs/remotes/origin/main" -> "main"
	if idx := strings.LastIndex(mainBranch, "/"); idx >= 0 {
		mainBranch = mainBranch[idx+1:]
	}

	return &GitSnapshot{
		Branch:        vals["branch"],
		MainBranch:    mainBranch,
		Status:        status,
		RecentCommits: vals["log"],
		UserName:      vals["user"],
	}
}

// FormatForPrompt formats the snapshot for system prompt injection.
func (s *GitSnapshot) FormatForPrompt() string {
	if s == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString("# Git Context\n")
	b.WriteString("This is a snapshot at conversation start and will not update.\n\n")

	fmt.Fprintf(&b, "Current branch: %s\n", s.Branch)
	if s.MainBranch != "" {
		fmt.Fprintf(&b, "Main branch: %s\n", s.MainBranch)
	}
	if s.UserName != "" {
		fmt.Fprintf(&b, "User: %s\n", s.UserName)
	}

	if s.Status != "" {
		b.WriteString("\n## Status\n```\n")
		b.WriteString(s.Status)
		b.WriteString("\n```\n")
	}

	if s.RecentCommits != "" {
		b.WriteString("\n## Recent Commits\n```\n")
		b.WriteString(s.RecentCommits)
		b.WriteString("\n```\n")
	}

	return b.String()
}
