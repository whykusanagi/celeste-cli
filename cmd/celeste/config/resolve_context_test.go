package config

import (
	"testing"

	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
)

// A fresh profile inherits the seed default's model, so pointing one at a local
// server left it carrying "fugu". The limits table has fugu at 1,000,000, which
// produced a confident 1M budget for a server that might have 8k: celeste would
// never compact and the request would overflow. A hit in the table is
// coincidence for a local endpoint, not knowledge.
func TestResolveContextLimit_LocalIgnoresCoincidentalModelMatch(t *testing.T) {
	limit, known := ResolveContextLimit("http://127.0.0.1:8080/v1", "fugu", 0)
	if known {
		t.Error("a local endpoint must never report a model-table hit as known")
	}
	if limit != ctxmgr.LocalDefaultLimit {
		t.Errorf("limit = %d, want the local fallback %d", limit, ctxmgr.LocalDefaultLimit)
	}
}

// The same model on its real provider keeps its real window.
func TestResolveContextLimit_HostedKeepsModelDefault(t *testing.T) {
	limit, known := ResolveContextLimit("https://api.sakana.ai/v1", "fugu", 0)
	if !known {
		t.Error("a hosted provider's known model must stay known")
	}
	if want := GetModelLimit("fugu"); limit != want {
		t.Errorf("limit = %d, want %d", limit, want)
	}
}

// An explicit setting is the user's knowledge and outranks everything.
func TestResolveContextLimit_OverrideWins(t *testing.T) {
	for _, url := range []string{"http://127.0.0.1:8080/v1", "https://api.sakana.ai/v1"} {
		limit, known := ResolveContextLimit(url, "fugu", 32768)
		if !known || limit != 32768 {
			t.Errorf("%s: got (%d, %v), want (32768, true)", url, limit, known)
		}
	}
}

// An unknown model on a hosted provider gets 128k, not the local 8k (#201).
func TestResolveContextLimit_UnknownModelHosted(t *testing.T) {
	limit, known := ResolveContextLimit("https://api.openai.com/v1", "some-new-model", 0)
	if known {
		t.Error("an unlisted model must not report as known")
	}
	if limit != 128000 {
		t.Errorf("limit = %d, want 128000", limit)
	}
}

// An unknown model on a local endpoint keeps the small local default (#201).
func TestResolveContextLimit_UnknownModelLocal(t *testing.T) {
	limit, known := ResolveContextLimit("http://localhost:11434/v1", "llama3", 0)
	if known || limit != ctxmgr.LocalDefaultLimit {
		t.Errorf("got (%d, %v), want (%d, false)", limit, known, ctxmgr.LocalDefaultLimit)
	}
}

// The guessed-window warning fires once per model, not on every resolve.
func TestUnknownContextNotice_Once(t *testing.T) {
	if UnknownContextNotice("notice-once-model", 128000) == "" {
		t.Fatal("first call must return the warning")
	}
	if got := UnknownContextNotice("notice-once-model", 128000); got != "" {
		t.Errorf("second call returned %q, want empty", got)
	}
}
