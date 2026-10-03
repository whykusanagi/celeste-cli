package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts/personacrypt/personacrypttest"
)

// useTestPersona makes this test run the synthetic persona sealed under the
// public test key, so the official-persona path is exercised without the
// real key. Tests that call it must not call t.Parallel: the persona is
// process-wide.
func useTestPersona(t *testing.T) {
	t.Helper()
	t.Cleanup(prompts.UsePersonaSource(personacrypttest.FS(prompts.VoiceBoundary), personacrypttest.Key))
}

// systemLines are the chat's system messages.
func systemLines(m tea.Model) []string {
	var out []string
	for _, msg := range chatMessages(m) {
		if msg.Role == "system" {
			out = append(out, msg.Content)
		}
	}
	return out
}

// Review Focus 7: a build without the key says at chat startup, every
// startup, that the full persona ships only in official releases. A test
// binary has no key, so it runs the public persona.
func TestFallbackChatStartupSaysWhereTheFullPersonaIs(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t)
	m, _, _ := chatAppWithContextLimit(t, srv, 200000)
	var hits int
	for _, line := range systemLines(m) {
		if strings.Contains(line, "public persona") && strings.Contains(line, "official release") {
			hits++
		}
	}
	if hits != 1 {
		t.Fatalf("startup shows the public-persona notice %d times, want 1: %q", hits, systemLines(m))
	}
}

// With the official persona active there is no notice.
func TestOfficialChatStartupHasNoPersonaNotice(t *testing.T) {
	useTestPersona(t)
	srv := fakeprovider.NewOpenAI(t)
	m, _, _ := chatAppWithContextLimit(t, srv, 200000)
	for _, line := range systemLines(m) {
		if strings.Contains(line, "public persona") {
			t.Fatalf("official build shows the persona notice: %q", line)
		}
	}
}
