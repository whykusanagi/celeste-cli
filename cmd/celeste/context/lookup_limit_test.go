package ctxmgr

import "testing"

// The distinction the config validator depends on: for a known model the limit
// is knowledge and can be used as an upper bound; for anything else it is a
// conservative fallback and must not be treated as one.
func TestLookupModelLimit_KnownVsUnknown(t *testing.T) {
	limit, known := LookupModelLimit("fugu")
	if !known {
		t.Fatal("fugu should be a known model")
	}
	if limit != ModelLimits["fugu"] {
		t.Errorf("limit = %d, want %d", limit, ModelLimits["fugu"])
	}

	// A local model's name is a filesystem path and is never in the table.
	limit, known = LookupModelLimit("/Users/me/models/qwen3.8-27b/4-bit")
	if known {
		t.Error("a local model path must not report as known")
	}
	if limit != ModelLimits["default"] {
		t.Errorf("fallback = %d, want the default %d", limit, ModelLimits["default"])
	}

	// Empty behaves the same way.
	if _, known := LookupModelLimit(""); known {
		t.Error("empty model must not report as known")
	}
}

// GetModelLimit keeps its old signature and behaviour.
func TestGetModelLimit_UnchangedBehaviour(t *testing.T) {
	if got := GetModelLimit("fugu"); got != ModelLimits["fugu"] {
		t.Errorf("known model: got %d", got)
	}
	if got := GetModelLimit("nope"); got != ModelLimits["default"] {
		t.Errorf("unknown model: got %d, want default", got)
	}
}
