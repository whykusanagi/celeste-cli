package prompts

import (
	"flag"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
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
	useTestPersona(t)
	prev := confirmActionsEnabled
	confirmActionsEnabled = func() bool { return confirm }
	t.Cleanup(func() { confirmActionsEnabled = prev })
	prevNow := now
	now = func() time.Time { return time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC) }
	t.Cleanup(func() { now = prevNow })
}

const testContract = "AGENT CONTRACT: inspect, act, verify."

// goldenText shows a prompt with Static as a marker naming its profile and
// Dynamic verbatim. Persona bytes never go in a golden (W5 ruling 19), and a
// persona update can't churn these.
func goldenText(p Prompt) string {
	static := "(none)"
	if p.Static != "" {
		static = "<<PERSONA:" + string(p.Profile) + ">>"
	}
	return "STATIC " + static + "\n==== DYNAMIC ====\n" + p.Dynamic + "\n"
}

// Golden files pin the prompt for each mode (#170, W5). Regenerate with
// go test ./cmd/celeste/prompts -run TestComposeGolden -update
func TestComposeGolden(t *testing.T) {
	cases := []struct {
		name    string
		confirm bool
		opts    ComposeOptions
	}{
		{"chat", false, ComposeOptions{Mode: ModeChat}},
		{"chat_confirm", true, ComposeOptions{Mode: ModeChat}},
		{"chat_context", false, ComposeOptions{Mode: ModeChat, ProjectContext: "PROJECT", GitSnapshot: "GIT", Memories: "# Project Memories\n\nMEMORY"}},
		{"chat_window_8192", false, ComposeOptions{Mode: ModeChat, Window: 8192}},
		{"agent", true, ComposeOptions{Mode: ModeAgent, Contract: testContract, ProjectContext: "PROJECT", GitSnapshot: "GIT"}},
		{"agent_off", true, ComposeOptions{Mode: ModeAgent, PersonaLevel: PersonaOff, Contract: testContract}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			composeEnv(t, tc.confirm)
			p := Compose(tc.opts)
			if p.Static != personaStatic(mustProfile(p.Profile)) {
				t.Fatalf("Static is not the %s profile's bytes", p.Profile)
			}
			got := goldenText(p)
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

// Static is exactly the mode's profile (spec W5: chat full, agent spine),
// and String() puts it first.
func TestComposeStaticIsTheProfile(t *testing.T) {
	composeEnv(t, false)
	if mustProfile(ProfileFull).Public {
		t.Fatal("composeEnv should install the test persona")
	}
	for mode, want := range map[Mode]Profile{ModeChat: ProfileFull, ModeAgent: ProfileSpine} {
		p := Compose(ComposeOptions{Mode: mode, Contract: testContract})
		if p.Profile != want || p.Static != mustProfile(want).SystemPrompt {
			t.Errorf("mode %d: Static is %q, want the %s profile", mode, p.Profile, want)
		}
		if p.String() != p.Static+"\n\n"+p.Dynamic {
			t.Errorf("mode %d: String() is not Static, a blank line, Dynamic", mode)
		}
	}
}

// Dynamic follows the spec's order: sliders, identity, (mode rules),
// context files/grimoire, git, memories, date (W5 ruling 8).
func TestComposeDynamicOrder(t *testing.T) {
	composeEnv(t, false)
	p := Compose(ComposeOptions{Mode: ModeChat, ProjectContext: "PROJECT-MARK", GitSnapshot: "GIT-MARK", Memories: "# Project Memories\n\nMEMORY-MARK"})
	order := []string{"Voice Modulation:", "Current User Identity:", "Task Execution Rules:", "# Project Context (.grimoire)", "GIT-MARK", "MEMORY-MARK", "Current date: 2026-10-01"}
	last := -1
	for _, mark := range order {
		i := strings.Index(p.Dynamic, mark)
		if i < 0 {
			t.Fatalf("Dynamic lacks %q", mark)
		}
		if i <= last {
			t.Fatalf("%q is out of order in Dynamic", mark)
		}
		last = i
	}
}

// Static never moves with anything dynamic, so provider prefix caches hit
// across confirm mode, sliders, context, git, memories and the date.
func TestComposeStaticIgnoresDynamicInputs(t *testing.T) {
	composeEnv(t, false)
	base := Compose(ComposeOptions{Mode: ModeChat}).Static
	composeEnv(t, true)
	now = func() time.Time { return time.Date(2027, 1, 2, 0, 0, 0, 0, time.UTC) }
	sliders := config.DefaultSliderConfig()
	sliders.Flirt = 0
	other := Compose(ComposeOptions{Mode: ModeChat, Sliders: sliders, ProjectContext: "P", GitSnapshot: "G", Memories: "M"})
	if other.Static != base {
		t.Fatal("Static changed with dynamic inputs")
	}
	if !strings.Contains(other.Dynamic, "Current date: 2027-01-02") {
		t.Fatal("the date line did not follow the clock")
	}
}

// Agent runs never get the chat task rules or confirm mode: a headless run
// has nobody to confirm with, and the rules contradict the agent contract.
func TestComposeAgentModeExcludesChatRules(t *testing.T) {
	composeEnv(t, true)
	got := Compose(ComposeOptions{Mode: ModeAgent, Contract: testContract}).String()
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
	if got := Compose(ComposeOptions{Mode: ModeChat, Contract: testContract}).String(); strings.Contains(got, testContract) {
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
	got := Compose(ComposeOptions{Mode: ModeAgent, Contract: testContract, Sliders: override}).String()
	if n := strings.Count(got, "Voice Modulation:"); n != 1 {
		t.Fatalf("want exactly one Voice Modulation block, got %d", n)
	}
	if !strings.Contains(got, ComposeSliderPrompt(override)) {
		t.Error("prompt does not carry the override's slider block")
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// The off level (a typed explore or review subagent, 2.0 W4e) is Celeste's
// identity line, the honesty rule and the voice boundary (the ProfileOff
// profile), then the contract and project context: never less, and no full
// profile, user identity, sliders or chat rules. Checked with the sealed
// test persona and with the keyless build's public persona.
func TestComposePersonaOff(t *testing.T) {
	for _, build := range []string{"test-key", "keyless"} {
		t.Run(build, func(t *testing.T) {
			if build == "keyless" {
				if personaKey != "" {
					t.Skip("this test binary was built with a persona key")
				}
				tempHome(t)
				prev := confirmActionsEnabled
				confirmActionsEnabled = func() bool { return true }
				t.Cleanup(func() { confirmActionsEnabled = prev })
			} else {
				composeEnv(t, true)
			}
			if off := mustProfile(ProfileOff); !strings.Contains(off.SystemPrompt, VoiceBoundary) || (build == "keyless") != off.Public {
				t.Fatalf("off profile: public %v, %.80q", off.Public, off.SystemPrompt)
			}
			for _, mode := range []Mode{ModeAgent, ModeChat} {
				got := Compose(ComposeOptions{Mode: mode, PersonaLevel: PersonaOff, Contract: testContract, ProjectContext: "PROJECT", GitSnapshot: "GIT"}).String()
				want := publicIdentity + "\n\n" + publicHonesty + "\n\n" + mustProfile(ProfileOff).SystemPrompt
				if !strings.HasPrefix(got, want) {
					t.Fatalf("%v: off prompt does not start with identity, honesty and voice boundary:\n%s", mode, got)
				}
				for _, part := range []string{publicIdentity, publicHonesty, VoiceBoundary, "PROJECT", "GIT"} {
					if !strings.Contains(got, part) {
						t.Fatalf("%v: off prompt lacks %.40q:\n%s", mode, part, got)
					}
				}
				if strings.Contains(got, taskExecutionPrompt) || strings.Contains(got, confirmModePrompt) ||
					strings.Contains(got, ComposeSliderPrompt(config.LoadSliders())) {
					t.Fatalf("%v: off prompt has chat rules or sliders:\n%s", mode, got)
				}
				// With the sealed persona the full profile is far more than
				// the off text; the keyless build's full profile is the same
				// three parts, so there is nothing more to leave out.
				if build == "test-key" && strings.Contains(got, mustProfile(ProfileFull).SystemPrompt) {
					t.Fatalf("%v: off prompt has the full profile", mode)
				}
				if (mode == ModeAgent) != strings.Contains(got, testContract) {
					t.Fatalf("%v: contract presence wrong:\n%s", mode, got)
				}
			}
		})
	}
}

// No level drops the persona: an unknown one composes the mode's default
// profile.
func TestComposeUnknownPersonaLevelIsTheModeDefault(t *testing.T) {
	composeEnv(t, false)
	got := Compose(ComposeOptions{Mode: ModeAgent, PersonaLevel: "none", Contract: testContract})
	if got != Compose(ComposeOptions{Mode: ModeAgent, Contract: testContract}) || got.Profile != ProfileSpine {
		t.Fatalf("unknown level must compose the mode's default profile, got %s", got.Profile)
	}
}
