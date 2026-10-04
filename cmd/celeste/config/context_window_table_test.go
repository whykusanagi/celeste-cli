package config

import (
	"bytes"
	"log"
	"testing"

	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
)

// windowUnverified lists providers whose default model has no window we
// could confirm from a published source. Venice serves its catalog live and
// does not publish a window for its default; guessing high overflows, and
// the 128K fallback plus the one-time notice is the honest answer there.
var windowUnverified = map[string]bool{"venice": true}

// #319: the registry's default Anthropic model was missing from the table,
// so every fresh Anthropic profile ran on a guessed 128K window and showed
// the unknown-model warning. Every provider's default must be known.
func TestEveryProviderDefaultModelHasKnownWindow(t *testing.T) {
	for _, name := range providers.ListProviders() {
		caps, _ := providers.GetProvider(name)
		if caps.DefaultModel == "" || windowUnverified[name] {
			continue
		}
		if _, known := LookupModelLimit(caps.DefaultModel); !known {
			t.Errorf("provider %s: default model %q has no context window in the table", name, caps.DefaultModel)
		}
	}
}

// Every model the static Anthropic catalog offers is in the table with
// the window the catalog advertises. (Claude through OpenRouter is
// OpenRouter's model listing, with its own windows.)
func TestEveryCatalogClaudeModelHasKnownWindow(t *testing.T) {
	for _, p := range []string{"anthropic"} {
		for _, m := range providers.StaticModels(p) {
			got, known := LookupModelLimit(m.ID)
			if !known {
				t.Errorf("%s catalog model %q has no context window in the table", p, m.ID)
				continue
			}
			if m.ContextWindow > 0 && got != m.ContextWindow {
				t.Errorf("%s catalog model %q: table says %d, catalog says %d", p, m.ID, got, m.ContextWindow)
			}
		}
	}
}

// The current Claude line (aliases and dated snapshot IDs) with their real
// windows. 4.5-and-earlier models are 200K without the 1M beta header,
// which celeste never sends.
func TestCurrentClaudeModelWindows(t *testing.T) {
	want := map[string]int{
		"claude-fable-5-1":           1000000,
		"claude-fable-5":             1000000,
		"claude-opus-5-5":            1000000,
		"claude-opus-5":              1000000,
		"claude-opus-4-8":            1000000,
		"claude-opus-4-7":            1000000,
		"claude-opus-4-6":            1000000,
		"claude-sonnet-5-5":          1000000,
		"claude-sonnet-5":            1000000,
		"claude-sonnet-4-6":          1000000,
		"claude-haiku-4-5":           200000,
		"claude-haiku-4-5-20251001":  200000,
		"claude-sonnet-4-5":          200000,
		"claude-sonnet-4-5-20250929": 200000,
		"claude-opus-4-5":            200000,
		"claude-opus-4-5-20251101":   200000,
		"claude-opus-4-1":            200000,
		"claude-opus-4-1-20250805":   200000,
	}
	for id, w := range want {
		got, known := ctxmgr.LookupModelLimit(id)
		if !known || got != w {
			t.Errorf("%s: got (%d, %v), want (%d, true)", id, got, known, w)
		}
	}
}

// #319: the notice was also written to the standard logger, which prints
// to the terminal underneath the TUI's alternate screen and left fragments
// on it. Callers show the notice themselves.
func TestUnknownContextNotice_DoesNotWriteStandardLogger(t *testing.T) {
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	defer func() { log.SetOutput(prev); log.SetFlags(flags) }()
	if UnknownContextNotice("notice-no-log-model", 128000) == "" {
		t.Fatal("first notice is empty")
	}
	if buf.Len() != 0 {
		t.Errorf("notice went to the standard logger: %q", buf.String())
	}
}
