package orchestrator

import (
	"slices"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/personacrypt/personacrypttest"
)

// Orchestrator lanes (the primary, the debate reviewer and the defense)
// report to the orchestrator, not to the user: they run Celeste's off level
// (identity, honesty, voice boundary), not the full persona (W7 ruling 5;
// owner ruling on #265).
func TestLaneOptionsRunPersonaOff(t *testing.T) {
	opts := laneAgentOptions()
	if opts.PersonaLevel != prompts.PersonaOff {
		t.Fatalf("lane persona level = %q, want %q", opts.PersonaLevel, prompts.PersonaOff)
	}
	if !opts.Nested {
		t.Fatal("a lane is part of the caller's run (Nested)")
	}
}

// systemPrompt is the system message of one OpenAI-style request.
func systemPrompt(t *testing.T, r fakeprovider.Request) string {
	t.Helper()
	msgs, _ := r.Body["messages"].([]any)
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "system" {
			s, _ := mm["content"].(string)
			return s
		}
	}
	t.Fatalf("no system message in request to %s", r.Path)
	return ""
}

// Every request of a real code-lane run with a debate (primary, reviewer,
// defense) carries the off level: Celeste's identity line and the honesty
// rule, then the off profile (the voice boundary). With the sealed test
// persona, whose full profile is far larger than the off text, no request
// carries the full profile; the keyless build runs the public persona.
func TestOrchestratorLanesSendTheOffPersona(t *testing.T) {
	const honesty = "unless a tool actually returned that result this turn"
	for _, build := range []string{"test-key", "keyless"} {
		t.Run(build, func(t *testing.T) {
			orchWorkspace(t)
			if build == "keyless" {
				if prompts.HasPersonaKey() {
					t.Skip("this test binary was built with a persona key")
				}
			} else {
				t.Cleanup(prompts.UsePersonaSource(personacrypttest.FS(prompts.VoiceBoundary), personacrypttest.Key))
			}
			full, err := prompts.LoadProfile(prompts.ProfileFull)
			if err != nil {
				t.Fatal(err)
			}
			off, err := prompts.LoadProfile(prompts.ProfileOff)
			if err != nil {
				t.Fatal(err)
			}
			if (build == "keyless") != off.Public || full.Public != off.Public {
				t.Fatalf("persona: full public %v, off public %v", full.Public, off.Public)
			}

			primary := fakeprovider.NewOpenAI(t,
				fakeprovider.Turn{Text: "1. Fix it"},
				fakeprovider.Turn{Text: "TASK_COMPLETE: fixed"},
				fakeprovider.Turn{Text: "1. Address the review"},
				fakeprovider.Turn{Text: "TASK_COMPLETE: revised"},
			)
			reviewer := fakeprovider.NewOpenAI(t,
				fakeprovider.Turn{Text: "1. Review it"},
				fakeprovider.Turn{Text: `TASK_COMPLETE: [{"file":"main.go","line":1,"severity":"high","description":"broken"}]`},
			)
			cfg := fakeOrchCfg(reviewer)
			cfg.Orchestrator = &config.OrchestratorConfig{DebateRounds: 1, Lanes: map[string]config.LaneConfig{
				"code": {Primary: "fake-primary", PrimaryBaseURL: primary.BaseURL(), PrimaryAPIKey: "pk", Reviewer: "fake-model"},
			}}
			runOrch(t, New(cfg), "fix the bug in main.go")

			reqs := slices.Concat(primary.Requests(), reviewer.Requests())
			if len(reqs) != 6 {
				t.Fatalf("got %d requests, want 6 (primary, defense, reviewer)", len(reqs))
			}
			for i, r := range reqs {
				sys := systemPrompt(t, r)
				if !strings.HasPrefix(sys, "You are Celeste") || !strings.Contains(sys, honesty) ||
					!strings.Contains(sys, prompts.VoiceBoundary) || !strings.Contains(sys, off.SystemPrompt) {
					t.Fatalf("request %d does not carry identity, honesty and the voice boundary:\n%.300s", i, sys)
				}
				if build == "test-key" && strings.Contains(sys, full.SystemPrompt) {
					t.Fatalf("request %d carries the full persona", i)
				}
			}
		})
	}
}
