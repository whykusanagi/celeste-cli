package llm

import (
	"strings"
	"testing"
)

func TestValidateToolArgs(t *testing.T) {
	cases := []struct {
		name    string
		tool    string
		args    string
		wantErr bool
	}{
		{"empty args is ok for no-arg tools", "noop", "", false},
		{"valid json object", "speak", `{"text":"hello"}`, false},
		{"valid json with nested", "speak", `{"text":"hi","ssml":true}`, false},
		{"truncated object", "speak", `{"text":"hel`, true},
		{"garbage", "speak", `{text:`, true},
		{"bare word", "speak", `text`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := validateToolArgs(c.tool, c.args)
			if (got != "") != c.wantErr {
				t.Fatalf("validateToolArgs(%q, %q) = %q; wantErr=%v", c.tool, c.args, got, c.wantErr)
			}
		})
	}
}

func TestStripUnbackedAudioClaim(t *testing.T) {
	claim := "Done! Audio saved: speech_1700000000.mp3 (12345 bytes)"
	plain := "Here is the answer to your question."

	got := StripUnbackedAudioClaim(claim, false)
	if strings.Contains(got, "Audio saved:") {
		t.Fatalf("expected audio claim stripped when no TTS ran, got %q", got)
	}
	if got == "" {
		t.Fatalf("expected a replacement message, got empty")
	}
	if got := StripUnbackedAudioClaim(claim, true); got != claim {
		t.Fatalf("expected claim preserved when TTS ran, got %q", got)
	}
	if got := StripUnbackedAudioClaim(plain, false); got != plain {
		t.Fatalf("expected plain text untouched, got %q", got)
	}
}

func TestValidateToolArgs_FlagsCorruptedThenValid(t *testing.T) {
	intact := `{"text":"a very long script that exercises multi-chunk streaming","ssml":false}`
	truncated := `{"text":"a very long script that exercises multi-ch`
	if got := validateToolArgs("speak", intact); got != "" {
		t.Fatalf("intact payload should be valid, got %q", got)
	}
	if got := validateToolArgs("speak", truncated); got == "" {
		t.Fatalf("truncated payload should be flagged")
	}
}

func TestStripUnbackedSpawnClaim(t *testing.T) {
	fake := "Subagent spawned: subagent-sleep-task (id: task-47)"
	// No spawn ran → fabricated claim is replaced.
	got := StripUnbackedSpawnClaim(fake, false)
	if got == fake || !strings.Contains(got, "no subagent was actually spawned") {
		t.Fatalf("expected fabricated spawn claim to be stripped, got %q", got)
	}
	// A real spawn ran → content passes through unchanged.
	if got := StripUnbackedSpawnClaim(fake, true); got != fake {
		t.Fatalf("expected passthrough when spawn ran, got %q", got)
	}
	// Unrelated text is never touched.
	plain := "Here is a summary of the repo."
	if got := StripUnbackedSpawnClaim(plain, false); got != plain {
		t.Fatalf("expected unrelated text untouched, got %q", got)
	}
}
