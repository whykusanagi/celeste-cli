package config

import "testing"

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
	fallback, _ := LookupModelLimit("")
	if limit != fallback {
		t.Errorf("limit = %d, want the conservative fallback %d", limit, fallback)
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

// An unknown model on a hosted provider still gets the fallback.
func TestResolveContextLimit_UnknownModelHosted(t *testing.T) {
	limit, known := ResolveContextLimit("https://api.openai.com/v1", "some-new-model", 0)
	if known {
		t.Error("an unlisted model must not report as known")
	}
	if fallback, _ := LookupModelLimit(""); limit != fallback {
		t.Errorf("limit = %d, want fallback %d", limit, fallback)
	}
}
