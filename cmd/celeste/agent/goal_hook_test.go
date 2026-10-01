package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
)

// goalHookRunner is fakeRunner with a global UserPromptSubmit hook running
// hooktest's args, planning on (a planning request is a model call too).
func goalHookRunner(t *testing.T, srv *fakeprovider.Server, check bool, args ...string) *Runner {
	t.Helper()
	r, _ := fakeRunner(t, srv, func(o *Options) {
		o.CheckGoal = check
		o.EnablePlanning = true
		o.PlanningExplicit = true
		home, _ := os.UserHomeDir() // fakeRunner already pointed HOME at a temp dir
		writeHooks(t, home, map[string]any{"event": "UserPromptSubmit", "command": hooktest.Command(t, args...)})
	})
	return r
}

// A user's goal passes UserPromptSubmit once at run start (2.0 F2e): a deny
// ends the run before any model call, with the hook's reason.
func TestAgentGoalBlockedByUserPromptSubmit(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "never sent"})
	r := goalHookRunner(t, srv, true, "deny", "no secrets")
	var progress []string
	r.options.OnProgress = func(k ProgressKind, text string, _, _ int) {
		if k == ProgressError {
			progress = append(progress, text)
		}
	}
	st, err := r.RunGoal(context.Background(), "my password is hunter2")
	if !errors.Is(err, ErrGoalBlocked) || err.Error() != "goal blocked by a UserPromptSubmit hook: no secrets" {
		t.Fatalf("err = %v, want ErrGoalBlocked with the hook's reason", err)
	}
	if st != nil {
		t.Fatalf("a blocked goal produced a run state: %+v", st)
	}
	if n := len(srv.Requests()); n != 0 {
		t.Fatalf("requests = %d, want 0", n)
	}
	if len(progress) != 1 || progress[0] != err.Error() {
		t.Fatalf("progress errors = %q, want the block reported once (the TUI's /agent ends on it)", progress)
	}
}

// The hook's context joins the goal the model receives, after it, as the
// chat sends it; the planning request carries it too. The run's goal stays
// as typed.
func TestAgentGoalCarriesUserPromptSubmitContext(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "1. do it"}, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r := goalHookRunner(t, srv, true, "context", "GOAL-CTX")
	st, err := r.RunGoal(context.Background(), "ship it")
	if err != nil {
		t.Fatal(err)
	}
	want := "ship it\n\n<hook-context>\nGOAL-CTX\n</hook-context>"
	for i, req := range srv.Requests() {
		if !strings.Contains(toJSONString(req.Body["messages"]), toJSONString(want)) {
			t.Errorf("request %d lacks the goal with its hook context", i)
		}
	}
	if st.Goal != "ship it" {
		t.Errorf("state goal = %q, want it as typed", st.Goal)
	}
}

// The check runs once per run, not on the planning, continue or Stop
// prompts, and only where the goal is the user's: subagents and lanes
// (CheckGoal false) skip it.
func TestAgentGoalCheckedOnceAndOnlyWhenAsked(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "seen")
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "1. do it"}, fakeprovider.Turn{Text: "not done yet"}, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r := goalHookRunner(t, srv, true, "denyif", "Continue", "a continue prompt was checked")
	if _, err := r.RunGoal(context.Background(), "ship it"); err != nil {
		t.Fatalf("a later prompt went through UserPromptSubmit: %v", err)
	}

	srv2 := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "1. do it"}, fakeprovider.Turn{Text: "TASK_COMPLETE: done"})
	r2 := goalHookRunner(t, srv2, false, "record", marker)
	if _, err := r2.RunGoal(context.Background(), "ship it"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("a run without CheckGoal ran UserPromptSubmit: %v", err)
	}
}
