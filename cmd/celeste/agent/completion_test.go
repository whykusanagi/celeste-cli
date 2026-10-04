package agent

import (
	"bytes"
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/decide"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/steer"
)

func TestMarkerOnLine(t *testing.T) {
	o := DefaultOptions()
	for text, want := range map[string]bool{
		"TASK_COMPLETE: wrote a.go":                                     true,
		"Done.\n\nTASK_COMPLETE: summary":                               true,
		"**TASK_COMPLETE:** wrote a.go":                                 true,
		"## task_complete: wrote a.go":                                  true,
		"TASK_COMPLETE":                                                 true,
		"TASK_COMPLETE - wrote a.go":                                    true,
		"TASK_COMPLETED: not actually done":                             false,
		"Working.\nTASK_COMPLETE_LATER: after tests":                    false,
		"I will say TASK_COMPLETE when the tests pass.":                 false,
		"Not yet: TASK_COMPLETE comes after the build.\nRunning it now": false,
		"": false,
		// #330: models that print progress markers (the agent prompt asks
		// for STEP_DONE: <n>) on the lines before the completion marker.
		"STEP_DONE: 1\nTASK_COMPLETE: wrote hello.txt\n- files: hello.txt\n- checks: read it back": true,
		"STEP_DONE: 1\nSTEP_DONE: 2\n\n**TASK_COMPLETE:** done\nSummary follows.":                  true,
		"step_done: 1\ntask_complete: done\nnotes":                                                 true,
		// Only progress markers before the completion marker are skipped.
		"Wrote it.\nTASK_COMPLETE: done\nSTEP_DONE: 3":         false,
		"STEP_DONE: 1\nTASK_COMPLETE_LATER: after tests\nmore": false,
		"STEP_DONE: 1": false,
		"STEP_DONE: 1\nSTEP_DONE: 2\nTASK_COMPLETE": true,
		// A reasoning block whose opening tag the chat template sent
		// (qwen3 on servers without a reasoning parser): only the reply
		// after </think> is judged.
		"The user wants hello.txt.\nI will answer TASK_COMPLETE: after.\n</think>\n\nSTEP_DONE: 1\nTASK_COMPLETE: wrote hello.txt\nfiles: hello.txt": true,
		"TASK_COMPLETE: fixed the </think> parser\ndetails":                        true,
		"Okay, TASK_COMPLETE: is what I must say.\n</think>\nStill working.\nmore": false,
	} {
		if got := markerOnLine(text, o); got != want {
			t.Errorf("markerOnLine(%q) = %v, want %v", text, got, want)
		}
	}
	o.RequireCompletionMarker = false
	if !markerOnLine("any reply", o) {
		t.Error("without RequireCompletionMarker any reply completes")
	}
}

// steerRunner is fakeRunner with config changes, its errOut captured.
func steerRunner(t *testing.T, srv *fakeprovider.Server, mutate func(*config.Config)) (*Runner, *bytes.Buffer) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.EnablePlanning = false
	opts.AutoApproveTools = true
	opts.RequestTimeout = 10 * time.Second
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}
	if mutate != nil {
		mutate(cfg)
	}
	var errOut bytes.Buffer
	r, err := NewRunner(cfg, opts, io.Discard, &errOut)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r, &errOut
}

const promise = "Next I run the tests; TASK_COMPLETE: comes after that."

// completion_gate "on": a marker in the middle of a sentence no longer
// completes the run (2.0 W3).
func TestCompletionGateOnNeedsAnAnchoredMarker(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: promise}, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r, _ := steerRunner(t, srv, func(c *config.Config) { c.CompletionGate = "on" })
	st, err := r.RunGoal(context.Background(), "finish")
	if err != nil || st.Status != StatusCompleted || len(srv.Requests()) != 2 {
		t.Fatalf("status=%s requests=%d err=%v", st.Status, len(srv.Requests()), err)
	}
}

// The default (shadow) keeps the substring check and says where the gate
// would differ.
func TestCompletionGateShadowKeepsTheOldCheck(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: promise})
	r, errOut := steerRunner(t, srv, nil)
	st, err := r.RunGoal(context.Background(), "finish")
	if err != nil || st.Status != StatusCompleted || len(srv.Requests()) != 1 {
		t.Fatalf("status=%s requests=%d err=%v", st.Status, len(srv.Requests()), err)
	}
	if !strings.Contains(errOut.String(), "completion gate (shadow): would reject") {
		t.Errorf("errOut = %q", errOut.String())
	}
}

// qwen3Final is a qwen3:14b-style final reply from the 2.0 local smoke
// (#328 L8): a progress marker on the line before the completion marker,
// then the deliverables.
const qwen3Final = "STEP_DONE: 1\nTASK_COMPLETE: Created hello.txt with the requested greeting.\n\n- Files: hello.txt (new)\n- Commands: cat hello.txt -> hello world\n- Risks: none"

// #330: the shadow gate agrees with the old check on a reply whose
// progress markers come before TASK_COMPLETE, with the reasoning streamed
// as Ollama sends it (a separate reasoning field) or inlined in <think>.
func TestCompletionGateAcceptsProgressMarkersBeforeTheMarker(t *testing.T) {
	for name, final := range map[string]fakeprovider.Turn{
		"reasoning field": {ReasoningDeltas: []string{"The file is written and read back. ", "I should finish with TASK_COMPLETE: now."}, Deltas: []string{"STEP_DONE: 1\n", qwen3Final[len("STEP_DONE: 1\n"):]}},
		"inline think":    {Deltas: []string{"<think>\nI wrote hello.txt; reply TASK_COMPLETE: next.\n</think>\n\n", qwen3Final}},
	} {
		t.Run(name, func(t *testing.T) {
			for _, mode := range []string{"shadow", "on"} {
				srv := fakeprovider.NewOpenAI(t, final)
				r, errOut := steerRunner(t, srv, func(c *config.Config) { c.CompletionGate = mode })
				st, err := r.RunGoal(context.Background(), "write hello.txt")
				if err != nil || st.Status != StatusCompleted || len(srv.Requests()) != 1 {
					t.Fatalf("%s: status=%s requests=%d err=%v", mode, st.Status, len(srv.Requests()), err)
				}
				if strings.Contains(errOut.String(), "completion gate (shadow)") {
					t.Errorf("%s: the gate disagreed: %q", mode, errOut.String())
				}
			}
		})
	}
}

// With the watchdog on, a completion that claims success after an
// unchecked edit is vetoed once; the next completion is accepted.
func TestCompletionGateBallotVetoesOnce(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"a.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: wrote a.txt, all tests pass"},
		fakeprovider.Turn{Text: "TASK_COMPLETE: wrote a.txt, all tests pass"},
	)
	r, _ := steerRunner(t, srv, func(c *config.Config) { c.CompletionGate, c.Watchdog = "on", "on" })
	st, err := r.RunGoal(context.Background(), "write a.txt")
	if err != nil || st.Status != StatusCompleted || st.GateVetoes != 1 {
		t.Fatalf("status=%s vetoes=%d err=%v", st.Status, st.GateVetoes, err)
	}
	reqs := srv.Requests()
	if len(reqs) != 3 {
		t.Fatalf("requests = %d, want 3", len(reqs))
	}
	msgs := reqs[2].Body["messages"].([]any)
	if last := msgs[len(msgs)-1].(map[string]any)["content"].(string); !strings.Contains(last, "watchdog found no tool result") {
		t.Errorf("the vetoed run continued with %q", last)
	}
}

// The watchdog in shadow never vetoes, even with the gate on: the ballot
// is asked and logged only.
func TestCompletionGateShadowWatchdogNeverVetoes(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"a.txt","content":"hi"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: wrote a.txt, all tests pass"},
	)
	r, errOut := steerRunner(t, srv, func(c *config.Config) { c.CompletionGate, c.Watchdog = "on", "shadow" })
	st, err := r.RunGoal(context.Background(), "write a.txt")
	if err != nil || st.Status != StatusCompleted || st.GateVetoes != 0 || len(srv.Requests()) != 2 {
		t.Fatalf("status=%s vetoes=%d requests=%d err=%v", st.Status, st.GateVetoes, len(srv.Requests()), err)
	}
	if !strings.Contains(errOut.String(), "watchdog (completion): claims_unverified_success") {
		t.Errorf("errOut = %q", errOut.String())
	}
}

// countOracle answers every ballot the same way and counts the asks.
type countOracle struct {
	mu  sync.Mutex
	n   int
	ans map[string]decide.Answer
}

func (o *countOracle) Ask(context.Context, string, []decide.Question) (map[string]decide.Answer, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.n++
	return o.ans, nil
}

// A run with verification commands is checked by the runtime after the
// marker: the gate asks no ballot and never vetoes (final review M4).
func TestCompletionGateLeavesVerifiedRunsToTheRuntime(t *testing.T) {
	o := &countOracle{ans: map[string]decide.Answer{steer.QUnverified: {P: 0.9}}}
	s := steer.New(steer.Options{Watchdog: "on", Oracle: o})
	defer s.Close()
	r := &Runner{gateMode: config.ModeOn, errOut: io.Discard}
	st := &RunState{Options: DefaultOptions()}
	st.Options.RequireVerification = true
	st.Options.VerificationCommands = []string{"go test ./..."}
	complete, vetoed := r.completion(context.Background(), st, "TASK_COMPLETE: done", s)
	if !complete || vetoed || o.n != 0 || st.GateVetoes != 0 {
		t.Errorf("complete=%v vetoed=%v asks=%d vetoes=%d", complete, vetoed, o.n, st.GateVetoes)
	}
	st.Options.VerificationCommands = nil
	if _, vetoed := r.completion(context.Background(), st, "TASK_COMPLETE: done", s); !vetoed {
		t.Error("without verification commands the ballot must veto")
	}
}

// A veto settles the unverified-success finding: a watchdog concern about
// it still pending is not given as well (final review M6).
func TestCompletionGateVetoSettlesThePendingConcern(t *testing.T) {
	o := &countOracle{ans: map[string]decide.Answer{steer.QUnverified: {P: 0.9}}}
	s := steer.New(steer.Options{Watchdog: "on", Oracle: o, Every: 1})
	defer s.Close()
	s.Request(0, nil)
	s.Observe(loop.Event{Kind: loop.EventAssistant, Text: "done", ToolNames: []string{"bash"}})
	s.Observe(loop.Event{Kind: loop.EventTurnEnd})
	s.Wait() // a concern about unverified success is pending
	r := &Runner{gateMode: config.ModeOn, errOut: io.Discard}
	st := &RunState{Options: DefaultOptions()}
	if _, vetoed := r.completion(context.Background(), st, "TASK_COMPLETE: done", s); !vetoed {
		t.Fatal("no veto")
	}
	for _, b := range []loop.Boundary{loop.BoundaryTools, loop.BoundaryRun} {
		for _, rem := range s.Reminders(b) {
			if strings.Contains(rem.Text, "claim success") {
				t.Errorf("the settled concern was given too: %q", rem.Text)
			}
		}
	}
}

// #330: the task-complete-before-verify rule, in shadow (the default), on
// a qwen3-style run whose reasoning names TASK_COMPLETE: it stays quiet
// when the edit was checked, and says it would interrupt when it was not.
// Reasoning (a separate field, or inlined in <think>) never reaches it.
func TestTaskCompleteRuleOnReasoningModelRun(t *testing.T) {
	write := fakeprovider.ToolCall{ID: "w", Name: "write_file", Args: `{"path":"hello.txt","content":"hello world\n"}`}
	check := fakeprovider.ToolCall{ID: "c", Name: "bash", Args: `{"command":"cat hello.txt"}`}
	think := []string{"I must write hello.txt, check it, then reply\n", "TASK_COMPLETE: with the summary.\n"}
	for name, tc := range map[string]struct {
		turns []fakeprovider.Turn
		fires bool
	}{
		"checked": {turns: []fakeprovider.Turn{
			{ReasoningDeltas: think, ToolCalls: []fakeprovider.ToolCall{write}},
			{ReasoningDeltas: think, ToolCalls: []fakeprovider.ToolCall{check}},
			{ReasoningDeltas: think, Deltas: []string{qwen3Final}},
		}},
		"checked, inline think": {turns: []fakeprovider.Turn{
			{ToolCalls: []fakeprovider.ToolCall{write}},
			{ToolCalls: []fakeprovider.ToolCall{check}},
			{Deltas: []string{"<think>\n" + strings.Join(think, ""), "</think>\n\n", qwen3Final}},
		}},
		"unchecked": {turns: []fakeprovider.Turn{
			{ReasoningDeltas: think, ToolCalls: []fakeprovider.ToolCall{write}},
			{ReasoningDeltas: think, Deltas: []string{qwen3Final}},
		}, fires: true},
	} {
		t.Run(name, func(t *testing.T) {
			srv := fakeprovider.NewOpenAI(t, tc.turns...)
			r, errOut := steerRunner(t, srv, nil)
			st, err := r.RunGoal(context.Background(), "write hello.txt")
			if err != nil || st.Status != StatusCompleted || len(srv.Requests()) != len(tc.turns) {
				t.Fatalf("status=%s requests=%d err=%v\n%s", st.Status, len(srv.Requests()), err, errOut)
			}
			fired := strings.Contains(errOut.String(), "stream rule task-complete-before-verify would interrupt")
			if fired != tc.fires {
				t.Errorf("fired=%v, want %v: %q", fired, tc.fires, errOut.String())
			}
			if strings.Contains(errOut.String(), "completion gate (shadow)") {
				t.Errorf("the gate disagreed: %q", errOut.String())
			}
		})
	}
}
