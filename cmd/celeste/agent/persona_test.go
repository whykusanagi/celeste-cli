package agent

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

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
func TestAgentSmallWindowStepsDownAndSaysSo(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r, errOut := personaRunner(t, srv, 9100, nil)
	_, _ = r.RunGoal(context.Background(), "say hi")
	if !strings.HasPrefix(firstSystem(t, srv), profileText(t, prompts.ProfileLite)) {
		t.Fatal("a 9.1k window did not step the agent down to lite")
	}
	if !strings.Contains(errOut.String(), "lite profile instead of spine") {
		t.Fatalf("stderr lacks the notice: %q", errOut.String())
	}
}
