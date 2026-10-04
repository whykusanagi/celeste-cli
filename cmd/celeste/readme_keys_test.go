package main

import (
	"strings"
	"testing"
)

// readmeSection returns the README text under heading up to the next heading
// of the same or a higher level.
func readmeSection(t *testing.T, readme, heading string) string {
	t.Helper()
	_, rest, ok := strings.Cut(readme, "\n"+heading+"\n")
	if !ok {
		t.Fatalf("README.md has no %q heading", heading)
	}
	level := strings.IndexByte(heading, ' ')
	lines := strings.Split(rest, "\n")
	for i, line := range lines {
		if n := len(line) - len(strings.TrimLeft(line, "#")); n > 0 && n <= level && strings.HasPrefix(line[n:], " ") {
			return strings.Join(lines[:i], "\n")
		}
	}
	return rest
}

// The keyboard table matches the TUI: Ctrl+C cancels and a double tap exits,
// Esc on an empty input interrupts a running turn (#172), and exit is typed
// without a slash (#314).
func TestReadmeKeyboardShortcuts(t *testing.T) {
	readme := repoDoc(t, "README.md")
	keys := readmeSection(t, readme, "### Keyboard Shortcuts")
	requireAll(t, "README Keyboard Shortcuts", keys,
		"| `Ctrl+C` | Cancel the running operation; press again within 3 seconds to exit |",
		"interrupts the running turn",
	)
	requireNone(t, "README Keyboard Shortcuts", keys, "Exit immediately", "Exit gracefully")
	requireNone(t, "README.md", readme, "| `/exit`, `/quit`, `/q` |")
}

// Anthropic usage is counted, cache tokens included (#193, #314).
func TestReadmeAnthropicTokenTracking(t *testing.T) {
	readme := repoDoc(t, "README.md")
	_, tracking, ok := strings.Cut(readme, "**Token Tracking Support by Provider:**")
	if !ok {
		t.Fatal("README.md has no token tracking list")
	}
	full, _, _ := strings.Cut(tracking, "**No Support**")
	requireAll(t, "README token tracking (full support)", full, "Anthropic Claude")
	requireNone(t, "README.md", readme, "automatic tracking isn't wired up")
}

// The README's comparison carries docs/COMPARISON.md's list of what celeste
// keeps and links there instead of a 1.x-era table (#314).
func TestReadmeComparisonFollowsComparisonDoc(t *testing.T) {
	readme := repoDoc(t, "README.md")
	cmp := readmeSection(t, readme, "## 📊 How Celeste Compares")
	requireAll(t, "README How Celeste Compares", cmp,
		"(docs/COMPARISON.md)", "Claude Code", "Codex CLI", "opencode", "Crush",
		"Gemini CLI", "oh-my-pi", "pi",
		"A code graph with structural review", "MCP-server mode", "A character",
	)
	requireNone(t, "README How Celeste Compares", cmp, "OpenClaw", "Picobot", "gptme", "54MB")
}
