package agent

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// rulesRunner is fakeRunner with stream_rules set.
func rulesRunner(t *testing.T, srv *fakeprovider.Server, mode string) *Runner {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	opts := DefaultOptions()
	opts.Workspace = t.TempDir()
	opts.EnablePlanning = false
	opts.AutoApproveTools = true
	opts.RequestTimeout = 10 * time.Second
	cfg := &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10, StreamRules: mode}
	r, err := NewRunner(cfg, opts, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)
	return r
}

// stream_rules "on": an unbacked audio claim is cut short and the turn
// re-runs with the rule's reminder, through the real client (2.0 W3).
func TestAgentStreamRuleInterruptsAndReruns(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{Text: "TASK_COMPLETE: Audio saved: /tmp/x.mp3"},
		fakeprovider.Turn{Text: "TASK_COMPLETE: no audio was made; generate_speech is not available"},
	)
	r := rulesRunner(t, srv, "on")
	st, err := r.RunGoal(context.Background(), "make a voice line")
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != StatusCompleted || !strings.Contains(st.LastAssistantResponse, "no audio was made") {
		t.Fatalf("status=%s last=%q", st.Status, st.LastAssistantResponse)
	}
	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("requests = %d, want 2", len(reqs))
	}
	msgs := reqs[1].Body["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)["content"].(string)
	if !strings.Contains(last, "<system-reminder>") || !strings.Contains(last, "no text-to-speech tool has run") {
		t.Errorf("the re-run's last message = %q", last)
	}
}

// The default (shadow) never changes a run.
func TestAgentStreamRulesShadowByDefault(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "TASK_COMPLETE: Audio saved: /tmp/x.mp3"})
	r := rulesRunner(t, srv, "")
	st, err := r.RunGoal(context.Background(), "make a voice line")
	if err != nil || st.Status != StatusCompleted || len(srv.Requests()) != 1 {
		t.Fatalf("status=%s requests=%d err=%v", st.Status, len(srv.Requests()), err)
	}
}
