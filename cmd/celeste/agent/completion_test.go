package agent

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

func TestMarkerOnLine(t *testing.T) {
	o := DefaultOptions()
	for text, want := range map[string]bool{
		"TASK_COMPLETE: wrote a.go":                                     true,
		"Done.\n\nTASK_COMPLETE: summary":                               true,
		"**TASK_COMPLETE:** wrote a.go":                                 true,
		"## task_complete: wrote a.go":                                  true,
		"TASK_COMPLETE":                                                 true,
		"I will say TASK_COMPLETE when the tests pass.":                 false,
		"Not yet: TASK_COMPLETE comes after the build.\nRunning it now": false,
		"": false,
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
