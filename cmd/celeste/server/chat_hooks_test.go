package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/hooktest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
)

func userTexts(req fakeprovider.Request) []string {
	msgs, _ := req.Body["messages"].([]any)
	var out []string
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "user" {
			s, _ := mm["content"].(string)
			out = append(out, s)
		}
	}
	return out
}

func globalHooks(t *testing.T, defs ...map[string]any) {
	t.Helper()
	writeFile(t, filepath.Join(testHome(t), ".celeste", "hooks.json"), hooksJSON(t, defs...))
}

func TestMCPChatSessionStartContextReachesTheModel(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	cfg, ws := contractCfg(t, fp)
	globalHooks(t, hookDef("SessionStart", "", hooktest.Command(t, "context", "session-marker-42")))
	if r := chatVia(t, cfg, ws, "hi"); r.IsError || r.Text != "ok" {
		t.Fatalf("got %+v", r)
	}
	if sys := systemText(fp.Requests()[0]); !strings.Contains(sys, "# Session Start Hook Context\n\nsession-marker-42") {
		t.Fatalf("SessionStart context missing:\n%s", sys)
	}
}

// SessionStart fires per call on a shared Env, and its context never
// accumulates in the Env.
func TestMCPChatSessionStartFiresPerCall(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"})
	cfg, ws := contractCfg(t, fp)
	globalHooks(t, hookDef("SessionStart", "", hooktest.Command(t, "context", "session-marker-42")))
	srv := chatServer(t, cfg)
	for _, p := range []string{"first", "second"} {
		if _, err := srv.runChatMode(context.Background(), cfg.CelesteConfig, p, ws); err != nil {
			t.Fatal(err)
		}
	}
	for i, req := range fp.Requests() {
		if n := strings.Count(systemText(req), "session-marker-42"); n != 1 {
			t.Errorf("call %d: marker appears %d times, want 1", i+1, n)
		}
	}
}

func TestMCPChatUserPromptSubmitDenyRefusesTheCall(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "never sent"})
	cfg, ws := contractCfg(t, fp)
	globalHooks(t, hookDef("UserPromptSubmit", "", hooktest.Command(t, "deny", "no prompts today")))
	r := chatVia(t, cfg, ws, "hi")
	if !r.IsError || !strings.HasPrefix(r.Text, "Error: prompt blocked by a UserPromptSubmit hook: no prompts today") {
		t.Fatalf("got %+v", r)
	}
	if n := len(fp.Requests()); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}

func TestMCPChatUserPromptSubmitContextTravelsWithThePrompt(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	cfg, ws := contractCfg(t, fp)
	globalHooks(t, hookDef("UserPromptSubmit", "", hooktest.Command(t, "context", "prompt-marker-7")))
	chatVia(t, cfg, ws, "hi")
	if users := userTexts(fp.Requests()[0]); len(users) != 1 || users[0] != "hi\n\n<hook-context>\nprompt-marker-7\n</hook-context>" {
		t.Fatalf("user messages = %q", users)
	}
}

// Review focus 5: a Stop deny continues a finished call once; the second
// deny is reported, not obeyed.
func TestMCPChatStopHookContinuesOnce(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "first"}, fakeprovider.Turn{Text: "second"}, fakeprovider.Turn{Text: "third"})
	cfg, ws := contractCfg(t, fp)
	globalHooks(t, hookDef("Stop", "", hooktest.Command(t, "deny", "keep going")))
	r := chatVia(t, cfg, ws, "hi")
	reqs := fp.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2 (one continuation)", len(reqs))
	}
	if users := userTexts(reqs[1]); users[len(users)-1] != "keep going" {
		t.Fatalf("continuation message = %q", users)
	}
	if r.IsError || !strings.HasPrefix(r.Text, "second") || !strings.Contains(r.Text, "asked the chat to continue again; ignored") {
		t.Fatalf("got %+v", r)
	}
}

// Review focus 5: Stop never fires after a guard stop.
func TestMCPChatStopHookSkipsGuardStops(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 5; i++ {
		turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"main.go"}`}}})
	}
	fp := fakeprovider.NewOpenAI(t, turns...)
	cfg, ws := contractCfg(t, fp)
	marker := filepath.Join(t.TempDir(), "stop-ran")
	globalHooks(t, hookDef("Stop", "", hooktest.Command(t, "record", marker)))
	r := chatVia(t, cfg, ws, "loop")
	if r.Text != "Stopped: the model made the identical tool call 3 times in a row (stuck loop)." {
		t.Fatalf("got %+v", r)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Stop fired after a guard stop: %v", err)
	}
}

// Review focus 5: a Stop deny on the 25th turn cannot extend the call.
func TestMCPChatStopHookNoTurnsLeft(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 24; i++ {
		turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{
			ID: fmt.Sprintf("w%d", i), Name: "write_file", Args: fmt.Sprintf(`{"path":"f%d.txt","content":"%d"}`, i, i),
		}}})
	}
	turns = append(turns, fakeprovider.Turn{Text: "done"}, fakeprovider.Turn{Text: "too far"})
	fp := fakeprovider.NewOpenAI(t, turns...)
	cfg, ws := contractCfg(t, fp)
	globalHooks(t, hookDef("Stop", "", hooktest.Command(t, "deny", "more")))
	r := chatVia(t, cfg, ws, "write")
	if n := len(fp.Requests()); n != 25 {
		t.Fatalf("requests = %d, want 25", n)
	}
	if !strings.HasPrefix(r.Text, "done") || !strings.Contains(r.Text, "the call has no turns left") {
		t.Fatalf("got %+v", r)
	}
}

// A failing lifecycle hook's warning comes back on its own call: the shared
// runner's load-time sink is detached, so only the call's ctx sink carries it.
func TestMCPChatLifecycleHookWarningsLandOnTheCall(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	cfg, ws := contractCfg(t, fp)
	globalHooks(t,
		hookDef("SessionStart", "", hooktest.Command(t, "exit", "1", "session-boom")),
		hookDef("Stop", "", hooktest.Command(t, "exit", "1", "stop-boom")))
	r := chatVia(t, cfg, ws, "hi")
	if r.IsError || !strings.HasPrefix(r.Text, "ok") {
		t.Fatalf("got %+v", r)
	}
	for _, want := range []string{"## Warnings", "SessionStart hook", "session-boom", "Stop hook", "stop-boom"} {
		if !strings.Contains(r.Text, want) {
			t.Errorf("result lacks %q:\n%s", want, r.Text)
		}
	}
}

// A failed UserPromptSubmit hook refuses the call (the chat UI's rule), and
// its failure warning rides on the error result.
func TestMCPChatUserPromptSubmitFailureRefusesTheCall(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "never sent"})
	cfg, ws := contractCfg(t, fp)
	globalHooks(t, hookDef("UserPromptSubmit", "", hooktest.Command(t, "exit", "1", "ups-boom")))
	r := chatVia(t, cfg, ws, "hi")
	if !r.IsError || !strings.HasPrefix(r.Text, "Error: prompt blocked by a UserPromptSubmit hook: hook failed:") || !strings.Contains(r.Text, "## Warnings") {
		t.Fatalf("got %+v", r)
	}
	if n := len(fp.Requests()); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
}

// The refusal text is part of the tool result the plugin sees: byte for byte
// the pre-loop server's (2.0 F2e moved the check into the loop).
func TestMCPChatUserPromptSubmitDenyTextIsExact(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "never sent"})
	cfg, ws := contractCfg(t, fp)
	globalHooks(t, hookDef("UserPromptSubmit", "", hooktest.Command(t, "deny", "no prompts today")))
	r := chatVia(t, cfg, ws, "hi")
	if want := "Error: prompt blocked by a UserPromptSubmit hook: no prompts today"; !r.IsError || r.Text != want {
		t.Fatalf("got %+v\nwant %q", r, want)
	}
}

// MCP chat's UserPromptSubmit runs in the loop (2.0 F2e), as the chat's
// does; without such hooks the loop takes the unchecked path.
func TestMCPChatLoopChecksThePrompt(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t)
	cfg, ws := contractCfg(t, fp)
	build := func() *loop.Loop {
		env, err := loop.Setup(loop.ModeMCPChat, cfg.CelesteConfig, ws, loop.SetupOptions{Warn: func(string) {}})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(env.Close)
		return newChatLoop(cfg.CelesteConfig, newChatClient(cfg.CelesteConfig, env.Registry, "", ""), env, "", "s")
	}
	if build().CheckPrompt != nil {
		t.Fatal("a loop without UserPromptSubmit hooks checks prompts")
	}
	globalHooks(t, hookDef("UserPromptSubmit", "", hooktest.Command(t, "context", "x")))
	if build().CheckPrompt == nil {
		t.Fatal("MCP chat's loop does not run UserPromptSubmit")
	}
}

// A Stop hook's continuation is the hook's instruction, not the caller's
// prompt: UserPromptSubmit sees only the prompt, once.
func TestMCPChatStopContinuationSkipsUserPromptSubmit(t *testing.T) {
	fp := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "first"}, fakeprovider.Turn{Text: "second"})
	cfg, ws := contractCfg(t, fp)
	globalHooks(t,
		hookDef("UserPromptSubmit", "", hooktest.Command(t, "denyif", "keep going", "continuation checked")),
		hookDef("Stop", "", hooktest.Command(t, "deny", "keep going")))
	r := chatVia(t, cfg, ws, "hi")
	if r.IsError || !strings.HasPrefix(r.Text, "second") {
		t.Fatalf("got %+v", r)
	}
	if n := len(fp.Requests()); n != 2 {
		t.Fatalf("requests = %d, want 2", n)
	}
}
