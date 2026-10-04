package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts/promptstest"
)

// personaRunner is a runner on the fake provider with a context_limit, a
// captured stderr and the test persona.
func personaRunner(t *testing.T, srv *fakeprovider.Server, contextLimit int, mutate func(*Options)) (*Runner, *bytes.Buffer) {
	t.Helper()
	promptstest.Install(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.EnablePlanning, opts.RequireVerification, opts.AutoApproveTools = false, false, true
	opts.RequestTimeout = 10 * time.Second
	if mutate != nil {
		mutate(&opts)
	}
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10, ContextLimit: contextLimit}
	var errOut bytes.Buffer
	r, err := NewRunner(cfg, opts, io.Discard, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r, &errOut
}

func firstSystem(t *testing.T, srv *fakeprovider.Server) string {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the provider")
	}
	msg := reqs[0].Body["messages"].([]any)[0].(map[string]any)
	if msg["role"] != "system" {
		t.Fatalf("first message is %v", msg["role"])
	}
	return msg["content"].(string)
}

func profileText(t *testing.T, p prompts.Profile) string {
	t.Helper()
	pp, err := prompts.LoadProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	return pp.SystemPrompt
}

func TestAgentRunsOnTheSpineProfile(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r, _ := personaRunner(t, srv, 200000, nil)
	_, _ = r.RunGoal(context.Background(), "say hi")
	if !strings.HasPrefix(firstSystem(t, srv), profileText(t, prompts.ProfileSpine)) {
		t.Fatal("agent system prompt does not start with the spine profile")
	}
}

func TestAgentPersonaLevelOff(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r, _ := personaRunner(t, srv, 200000, func(o *Options) { o.PersonaLevel = prompts.PersonaOff })
	_, _ = r.RunGoal(context.Background(), "review this")
	sys := firstSystem(t, srv)
	if !strings.Contains(sys, profileText(t, prompts.ProfileOff)) || strings.Contains(sys, "Voice Modulation:") ||
		strings.Contains(sys, profileText(t, prompts.ProfileLite)) {
		t.Fatal("an off run carries more than identity, honesty and the voice boundary rule")
	}
}

// The agent's window is its own model's (context_limit 9100 is used by no
// other test, so the one-time notice is this test's).
// It says so once (#321): the notice line on stderr, and no [persona] log
// line repeating it.
func TestAgentSmallWindowStepsDownAndSaysSo(t *testing.T) {
	var logs bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&logs)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r, errOut := personaRunner(t, srv, 9100, nil)
	_, _ = r.RunGoal(context.Background(), "say hi")
	if !strings.HasPrefix(firstSystem(t, srv), profileText(t, prompts.ProfileLite)) {
		t.Fatal("a 9.1k window did not step the agent down to lite")
	}
	if !strings.Contains(errOut.String(), "lite profile instead of spine") {
		t.Fatalf("stderr lacks the notice: %q", errOut.String())
	}
	if got := strings.Count(errOut.String()+logs.String(), "lite profile instead of spine"); got != 1 {
		t.Fatalf("the notice is reported %d times, want 1: stderr %q, log %q", got, errOut.String(), logs.String())
	}
}

// #310: at 8,192 the agent's first request (persona and tool schemas)
// leaves the history room, and stderr says once that the tools were
// fitted (context_limit 8192: the notice is keyed by the tool counts too,
// so another test's fit does not swallow it).
func TestAgentSmallWindowFitsTheTools(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r, errOut := personaRunner(t, srv, 8192, nil)
	_, _ = r.RunGoal(context.Background(), "say hi")
	defs, _ := srv.Requests()[0].Body["tools"].([]any)
	b, _ := json.Marshal(defs)
	prefix := len(firstSystem(t, srv))/4 + len(b)/4
	if room := compact.HistoryBudget(8192, prefix); room < 8192/4 {
		t.Fatalf("prefix %d tokens (%d tools) leaves %d for history", prefix, len(defs), room)
	}
	if got := strings.Count(errOut.String(), "too small for all the tool definitions"); got > 1 {
		t.Fatalf("the tool notice shows %d times", got)
	}
}
