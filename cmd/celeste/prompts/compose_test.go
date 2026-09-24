package prompts

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden prompt files in testdata/")

// composeEnv isolates Compose from the real ~/.celeste (embedded essence,
// default user and sliders) and pins confirm mode.
func composeEnv(t *testing.T, confirm bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("HOME override is not honoured on Windows")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	prev := confirmActionsEnabled
	confirmActionsEnabled = func() bool { return confirm }
	t.Cleanup(func() { confirmActionsEnabled = prev })
}

// The persona core is replaced by a marker in the golden files so a persona
// update doesn't churn them; TestComposePersonaCoreIsPrefix pins the core.
const personaMarker = "<<PERSONA CORE>>"

const testContract = "AGENT CONTRACT: inspect, act, verify."

// Golden files pin the prompt for each mode (#170). Regenerate with
// go test ./prompts -run TestComposeGolden -update
func TestComposeGolden(t *testing.T) {
	cases := []struct {
		name    string
		confirm bool
		opts    ComposeOptions
	}{
		{"chat", false, ComposeOptions{Mode: ModeChat}},
		{"chat_confirm", true, ComposeOptions{Mode: ModeChat}},
		{"chat_context", false, ComposeOptions{Mode: ModeChat, ProjectContext: "PROJECT", GitSnapshot: "GIT"}},
		{"chat_skip_persona", true, ComposeOptions{Mode: ModeChat, SkipPersona: true, ProjectContext: "PROJECT"}},
		{"agent", true, ComposeOptions{Mode: ModeAgent, Contract: testContract, ProjectContext: "PROJECT", GitSnapshot: "GIT"}},
		{"agent_skip_persona", true, ComposeOptions{Mode: ModeAgent, SkipPersona: true, Contract: testContract}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			composeEnv(t, tc.confirm)
			got := strings.Replace(Compose(tc.opts), personaCore(), personaMarker, 1)
			path := filepath.Join("testdata", "compose_"+tc.name+".golden")
			if *updateGolden {
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read golden (run with -update to create): %v", err)
			}
			if got != string(want) {
				t.Errorf("prompt for %s differs from %s (run with -update if intended)\n--- got ---\n%s", tc.name, path, got)
			}
		})
	}
}

// The persona core comes first and unchanged in every persona mode, so the
// prompt prefix stays cacheable.
func TestComposePersonaCoreIsPrefix(t *testing.T) {
	composeEnv(t, true)
	core := personaCore()
	for _, mode := range []Mode{ModeChat, ModeAgent} {
		got := Compose(ComposeOptions{Mode: mode, Contract: testContract})
		if !strings.HasPrefix(got, core+"\n"+voiceBoundaryPrompt) {
			t.Errorf("mode %d: prompt does not start with the persona core then the voice boundary", mode)
		}
	}
}

// Agent runs never get the chat task rules or confirm mode: a headless run
// has nobody to confirm with, and the rules contradict the agent contract.
func TestComposeAgentModeExcludesChatRules(t *testing.T) {
	composeEnv(t, true)
	got := Compose(ComposeOptions{Mode: ModeAgent, Contract: testContract})
	for _, banned := range []string{taskExecutionPrompt, confirmModePrompt, "Wait for explicit user approval"} {
		if strings.Contains(got, banned) {
			t.Errorf("agent prompt contains chat-only text %q", firstLine(banned))
		}
	}
	if !strings.Contains(got, testContract) {
		t.Error("agent prompt is missing its contract")
	}
}

// Chat mode ignores Contract.
func TestComposeChatIgnoresContract(t *testing.T) {
	composeEnv(t, false)
	if got := Compose(ComposeOptions{Mode: ModeChat, Contract: testContract}); strings.Contains(got, testContract) {
		t.Error("chat prompt includes the agent contract")
	}
}

// A slider override replaces slider.json in the one slider block, rather than
// adding a second Voice Modulation block.
func TestComposeSliderOverride(t *testing.T) {
	composeEnv(t, false)
	override := config.DefaultSliderConfig()
	override.Flirt = 0
	override.Register = 10
	got := Compose(ComposeOptions{Mode: ModeAgent, Contract: testContract, Sliders: override})
	if n := strings.Count(got, "Voice Modulation:"); n != 1 {
		t.Fatalf("want exactly one Voice Modulation block, got %d", n)
	}
	if !strings.Contains(got, ComposeSliderPrompt(override)) {
		t.Error("prompt does not carry the override's slider block")
	}
}

// The legacy helpers are Compose in chat mode.
func TestLegacyHelpersUseCompose(t *testing.T) {
	composeEnv(t, false)
	if GetSystemPrompt(false) != Compose(ComposeOptions{Mode: ModeChat}) {
		t.Error("GetSystemPrompt differs from Compose chat mode")
	}
	if GetSystemPrompt(true) != "" {
		t.Error("GetSystemPrompt(true) should be empty")
	}
	want := Compose(ComposeOptions{Mode: ModeChat, ProjectContext: "P", GitSnapshot: "G"})
	if GetSystemPromptWithContext(false, "P", "G") != want {
		t.Error("GetSystemPromptWithContext differs from Compose")
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
