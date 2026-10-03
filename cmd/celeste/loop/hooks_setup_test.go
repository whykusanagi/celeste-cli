package loop

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// hooksJSON renders an F0 hooks.json (F0 Task 1 shape).
func hooksJSON(t *testing.T, defs ...map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"hooks": defs})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func hookDef(event, matcher, command string) map[string]any {
	d := map[string]any{"event": event, "command": command}
	if matcher != "" {
		d["matcher"] = matcher
	}
	return d
}

func writeCall(l *Loop) callsOutcome {
	return l.runCalls(context.Background(), []llm.ToolCallResult{{ID: "1", Name: "write_file", Arguments: `{"path":"x.txt","content":"y"}`}}, l.Limits.withDefaults())
}

// A trusted global PreToolUse hook blocks a call in agent mode, through the
// registry. (The loop has no tool-hook path at all, so it cannot fire twice.)
func TestSetupWiresToolHooksIntoTheRegistry(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("PreToolUse", "write_file", hooktest.Command(t, "deny", "frozen in test")),
	))
	env, _ := mustSetup(t, ModeAgent, t.TempDir())
	env.Trust()
	l := &Loop{Tools: env.Registry, Limits: DefaultLimits(), SpillDir: t.TempDir()}
	if out := writeCall(l); !strings.Contains(out.messages[0].Content, "Blocked by pre-tool hook: frozen in test") {
		t.Fatalf("got %s", out.messages[0].Content)
	}
}

// A PreToolUse "ask" reaches the run's Gate through tools.WithPrompt; with
// no Gate (agent CLI, MCP) the loop's headless prompt denies it.
func TestSetupHookAskUsesTheGate(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("PreToolUse", "write_file", hooktest.Command(t, "ask", "sure?")),
	))
	env, _ := mustSetup(t, ModeAgent, t.TempDir())
	env.Trust() // the policy alone would allow the write; the hook forces the prompt

	l := &Loop{Tools: env.Registry, Limits: DefaultLimits(), SpillDir: t.TempDir()}
	if out := writeCall(l); !strings.Contains(out.messages[0].Content, "Permission denied") {
		t.Fatalf("without a Gate: %s", out.messages[0].Content)
	}

	asks := 0
	l.Gate = GateFunc(func(context.Context, tools.PermissionRequest) tools.PermissionResponse {
		asks++
		return tools.PermissionResponse{Decision: "allow_once"}
	})
	if out := writeCall(l); strings.Contains(out.messages[0].Content, "denied") || asks != 1 {
		t.Fatalf("with a Gate: asks=%d content=%s", asks, out.messages[0].Content)
	}
}

// Non-interactive modes never approve repo hooks: they are skipped with
// F0's warning (runner.go "hooks: skipping …").
func TestSetupSkipsUntrustedRepoHooksInAgentMode(t *testing.T) {
	setupHome(t)
	ws := t.TempDir()
	write(t, filepath.Join(ws, ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("PreToolUse", "write_file", hooktest.Command(t, "deny", "repo says no")),
	))
	env, w := mustSetup(t, ModeAgent, ws)
	env.Trust()
	if !strings.Contains(w.all(), "skipping") {
		t.Fatalf("warnings = %q, want the skipped repo hooks named", w.all())
	}
	l := &Loop{Tools: env.Registry, Limits: DefaultLimits(), SpillDir: t.TempDir()}
	if out := writeCall(l); strings.Contains(out.messages[0].Content, "repo says no") {
		t.Fatalf("an untrusted repo hook ran: %s", out.messages[0].Content)
	}
}

// SessionStart is the adopter's call, not Setup's: nested runs skip it.
func TestSetupStartSessionIsExplicit(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("SessionStart", "", hooktest.Command(t, "context", "session-note-probe")),
	))
	env, _ := mustSetup(t, ModeAgent, t.TempDir())
	if strings.Contains(env.ProjectContext, "session-note-probe") {
		t.Fatal("Setup must not fire SessionStart itself")
	}
	env.StartSession(context.Background(), "startup")
	if !strings.Contains(env.ProjectContext, "# Session Start Hook Context\n\nsession-note-probe") ||
		!strings.Contains(env.SystemPrompt(PromptOptions{}).String(), "session-note-probe") {
		t.Fatalf("SessionStart context missing: %q", env.ProjectContext)
	}
	if env.Hooks == nil {
		t.Fatal("Env.Hooks must be the hooks runner")
	}
}

// A Load error (here: a relative home, as when os.UserHomeDir fails)
// disables hooks with the TUI's warning text; Setup carries on.
func TestSetupWarnsWhenHooksFailToLoad(t *testing.T) {
	setupHome(t)
	w := &warnings{}
	env := &Env{Mode: ModeAgent, Workspace: t.TempDir(), Registry: tools.NewRegistry(), opts: SetupOptions{SessionID: "s", Warn: w.add}}
	env.setupHooks("relative-home")
	if env.Hooks != nil {
		t.Fatal("Env.Hooks must be nil when loading failed")
	}
	if got := w.all(); !strings.HasPrefix(got, "hooks disabled: ") || !strings.HasSuffix(got, "(no hooks run this session, including global guards)") {
		t.Fatalf("warnings = %q, want the hooks-disabled warning", got)
	}
	env.StartSession(context.Background(), "startup") // nil runner: no-op
}

// SessionStartContext fires SessionStart without touching the Env, so a
// shared Env (MCP chat) gives each call its own session context.
func TestSessionStartContextLeavesTheEnvAlone(t *testing.T) {
	home := setupHome(t)
	write(t, filepath.Join(home, ".celeste", "hooks.json"), hooksJSON(t,
		hookDef("SessionStart", "", hooktest.Command(t, "context", "per-call-marker")),
	))
	env, _ := mustSetup(t, ModeMCPChat, t.TempDir())
	before := env.ProjectContext
	got := env.SessionStartContext(context.Background(), "startup")
	if got != "per-call-marker" {
		t.Fatalf("SessionStartContext = %q", got)
	}
	if env.ProjectContext != before {
		t.Fatal("SessionStartContext changed the Env")
	}
	if sys := env.SystemPrompt(PromptOptions{Session: got}).String(); !strings.Contains(sys, "# Session Start Hook Context\n\nper-call-marker") {
		t.Fatalf("session context missing:\n%s", sys)
	}
	if strings.Contains(env.SystemPrompt(PromptOptions{}).String(), "per-call-marker") {
		t.Fatal("SystemPrompt carries a call's session context")
	}
}

// The chat passes its own approver for repo hooks; non-interactive modes
// ignore one, so an untrusted repo hook never runs there (F0).
func TestSetupApproverIsUsedOnlyInChatMode(t *testing.T) {
	for _, tc := range []struct {
		mode Mode
		want bool
	}{{ModeChat, true}, {ModeAgent, false}, {ModeMCPChat, false}} {
		t.Run(tc.mode.String(), func(t *testing.T) {
			setupHome(t)
			ws := t.TempDir()
			write(t, filepath.Join(ws, ".celeste", "hooks.json"), hooksJSON(t,
				hookDef("PreToolUse", "write_file", hooktest.Command(t, "deny", "repo hook ran")),
			))
			asked := false
			env, err := Setup(tc.mode, testCfg(), ws, SetupOptions{
				Warn:    func(string) {},
				Approve: func(hooks.Source, hooks.TrustStatus) bool { asked = true; return true },
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(env.Close)
			if asked != tc.want || env.Hooks.Has(hooks.EventPreToolUse) != tc.want {
				t.Fatalf("asked=%v has=%v, want %v", asked, env.Hooks.Has(hooks.EventPreToolUse), tc.want)
			}
		})
	}
}
