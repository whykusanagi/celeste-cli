package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

type fakeAgentRunner struct {
	listRunsFn func(limit int) ([]agent.RunSummary, error)
	resumeFn   func(ctx context.Context, runID string) (*agent.RunState, error)
	runGoalFn  func(ctx context.Context, goal string) (*agent.RunState, error)
	closed     chan struct{} // closed by Close; nil = not recorded
}

func (f *fakeAgentRunner) Close() {
	if f.closed != nil {
		close(f.closed)
	}
}

func (f *fakeAgentRunner) ListRuns(limit int) ([]agent.RunSummary, error) {
	if f.listRunsFn != nil {
		return f.listRunsFn(limit)
	}
	return nil, nil
}

func (f *fakeAgentRunner) Resume(ctx context.Context, runID string) (*agent.RunState, error) {
	if f.resumeFn != nil {
		return f.resumeFn(ctx, runID)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeAgentRunner) RunGoal(ctx context.Context, goal string) (*agent.RunState, error) {
	if f.runGoalFn != nil {
		return f.runGoalFn(ctx, goal)
	}
	return nil, errors.New("not implemented")
}

// /agent list reads the checkpoint store; it never builds a runner.
func TestExecuteAgentCommandListRuns(t *testing.T) {
	originalFactory, originalList := newAgentRunnerForTUI, listAgentRunsForTUI
	t.Cleanup(func() { newAgentRunnerForTUI, listAgentRunsForTUI = originalFactory, originalList })

	newAgentRunnerForTUI = func(cfg *config.Config, options agent.Options, out io.Writer, errOut io.Writer) (agentRunnerAPI, error) {
		t.Fatal("/agent list built a runner")
		return nil, nil
	}
	listAgentRunsForTUI = func(limit int) ([]agent.RunSummary, error) {
		require.Equal(t, 20, limit)
		return []agent.RunSummary{
			{
				RunID:     "run-123",
				Goal:      "fix tests",
				Status:    agent.StatusCompleted,
				UpdatedAt: time.Date(2026, 3, 3, 10, 0, 0, 0, time.UTC),
				Turn:      3,
				ToolCalls: 2,
			},
		}, nil
	}

	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{
			APIKey:  "test-key",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4o-mini",
		},
	}

	output, err := adapter.executeAgentCommand([]string{"list-runs"})
	require.NoError(t, err)
	assert.Contains(t, output, "Recent Agent Runs (1):")
	assert.Contains(t, output, "run-123")
}

func TestExecuteAgentCommandGoal(t *testing.T) {
	originalFactory := newAgentRunnerForTUI
	t.Cleanup(func() { newAgentRunnerForTUI = originalFactory })

	closed := make(chan struct{})
	newAgentRunnerForTUI = func(cfg *config.Config, options agent.Options, out io.Writer, errOut io.Writer) (agentRunnerAPI, error) {
		return &fakeAgentRunner{
			closed: closed,
			runGoalFn: func(ctx context.Context, goal string) (*agent.RunState, error) {
				require.Equal(t, "build release notes", goal)
				return &agent.RunState{
					RunID:                 "run-456",
					Status:                agent.StatusCompleted,
					Turn:                  2,
					ToolCallCount:         1,
					LastAssistantResponse: "TASK_COMPLETE: done",
				}, nil
			},
		}, nil
	}

	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{
			APIKey:  "test-key",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4o-mini",
		},
	}

	output, err := adapter.executeAgentCommand([]string{"build", "release", "notes"})
	require.NoError(t, err)
	assert.Contains(t, output, "Run ID: run-456")
	assert.Contains(t, output, "Status: completed")
	assert.Contains(t, output, "Final Response:")
	select {
	case <-closed:
	default:
		t.Fatal("the /agent runner was not closed")
	}
}

func TestExecuteAgentCommandRequiresCredentials(t *testing.T) {
	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{
			APIKey:  "",
			BaseURL: "https://api.openai.com/v1",
			Model:   "gpt-4o-mini",
		},
	}

	output, err := adapter.executeAgentCommand([]string{"list-runs"})
	require.Error(t, err)
	assert.Equal(t, "", strings.TrimSpace(output))
	assert.Contains(t, err.Error(), "no API key or Google credentials configured")
}

// I2 (#144/#151 W6b review): /agent used its own inline ADC-only check
// instead of the shared needsAPIKey helper chat startup uses, so a keyless
// local endpoint was wrongly refused here.
func TestExecuteAgentCommandLocalEndpointNeedsNoKey(t *testing.T) {
	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{
			APIKey:  "",
			BaseURL: "http://127.0.0.1:8080/v1",
			Model:   "local-model",
		},
	}

	// "help" needs no runner and no checkpoint store, so this pins only the
	// credential check, not listAgentRunsForTUI's own behavior.
	output, err := adapter.executeAgentCommand([]string{"help"})
	require.NoError(t, err, "a keyless local endpoint must not be refused")
	assert.NotEmpty(t, output)
}

// I2: the inline check's "&&" also meant a stale google_use_adc left over
// from an earlier Vertex/Gemini setup silently waved through a profile
// --set-url had since repointed at a provider that genuinely needs a key.
func TestExecuteAgentCommandStaleADCOnNonGoogleStillNeedsKey(t *testing.T) {
	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{
			APIKey:       "",
			BaseURL:      "https://api.openai.com/v1",
			Model:        "gpt-4o-mini",
			GoogleUseADC: true,
		},
	}

	_, err := adapter.executeAgentCommand([]string{"list-runs"})
	require.Error(t, err, "a stale ADC flag on a non-Google base_url must not skip the key check")
	assert.Contains(t, err.Error(), "no API key or Google credentials configured")
}

// runBatch executes a command, expanding a tea.BatchMsg, and returns the
// messages produced.
func runBatch(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for _, c := range batch {
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func(c tea.Cmd) { done <- c() }(c)
		select {
		case m := <-done:
			out = append(out, m)
		case <-time.After(200 * time.Millisecond):
			// A read still waiting on the run; not needed here.
		}
	}
	return out
}

// /agent runs under a cancellable context handed to the TUI, and routes Ask
// decisions through the TUI's permission prompt (#172).
func TestRunGoalWithProgressIsCancellableAndPromptsForApproval(t *testing.T) {
	originalFactory := newAgentRunnerForTUI
	t.Cleanup(func() { newAgentRunnerForTUI = originalFactory })

	gotPrompt := make(chan bool, 1)
	gotOpts := make(chan agent.Options, 1)
	cancelled := make(chan struct{})
	closed := make(chan struct{})
	newAgentRunnerForTUI = func(cfg *config.Config, options agent.Options, out io.Writer, errOut io.Writer) (agentRunnerAPI, error) {
		gotPrompt <- options.PromptFunc != nil
		gotOpts <- options
		return &fakeAgentRunner{
			closed: closed,
			runGoalFn: func(ctx context.Context, goal string) (*agent.RunState, error) {
				<-ctx.Done()
				close(cancelled)
				return nil, ctx.Err()
			},
		}, nil
	}

	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{APIKey: "test-key", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"},
		promptFn: func(tools.PermissionRequest) tools.PermissionResponse {
			return tools.PermissionResponse{Decision: "allow_once"}
		},
	}

	var cancel context.CancelFunc
	for _, msg := range runBatch(t, adapter.runGoalWithProgress([]string{"long", "task"}, 1)) {
		if start, ok := msg.(tui.StreamStartMsg); ok {
			cancel = start.Cancel
		}
	}
	require.NotNil(t, cancel, "runGoalWithProgress must hand the TUI a cancel func")

	select {
	case ok := <-gotPrompt:
		assert.True(t, ok, "the agent run was not given the TUI permission prompt")
	case <-time.After(2 * time.Second):
		t.Fatal("runner was never created")
	}

	cancel()
	select {
	case <-cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelling did not stop the agent run")
	}
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("the /agent runner was not closed when the goal ended")
	}
	opts := <-gotOpts
	assert.True(t, opts.Nested, "the TUI session already fired SessionStart; /agent runs are nested")
	assert.NotNil(t, opts.Warn, "/agent warnings must reach the chat, not io.Discard")
}

// I2 (#144/#151 W6b review): runGoalWithProgress's own inline ADC-only check
// wrongly refused a keyless local endpoint.
func TestRunGoalWithProgressLocalEndpointNeedsNoKey(t *testing.T) {
	originalFactory := newAgentRunnerForTUI
	t.Cleanup(func() { newAgentRunnerForTUI = originalFactory })

	newAgentRunnerForTUI = func(cfg *config.Config, options agent.Options, out io.Writer, errOut io.Writer) (agentRunnerAPI, error) {
		return &fakeAgentRunner{
			runGoalFn: func(ctx context.Context, goal string) (*agent.RunState, error) {
				return &agent.RunState{RunID: "run-local", Status: agent.StatusCompleted}, nil
			},
		}, nil
	}

	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{APIKey: "", BaseURL: "http://127.0.0.1:8080/v1", Model: "local-model"},
	}

	var gotError string
	var gotComplete bool
	for _, msg := range runBatch(t, adapter.runGoalWithProgress([]string{"do", "a", "thing"}, 1)) {
		if p, ok := msg.(tui.AgentProgressMsg); ok {
			switch p.Kind {
			case tui.AgentProgressError:
				gotError = p.Text
			case tui.AgentProgressComplete:
				gotComplete = true
			}
		}
	}
	assert.Empty(t, gotError, "a keyless local endpoint must not be refused")
	assert.True(t, gotComplete, "the run must have reached completion")
}

// I2: a stale google_use_adc left over from an earlier Vertex/Gemini setup
// must not wave a --set-url-repointed, genuinely-key-needing profile through
// runGoalWithProgress's check either (agent_run.go's check already caught
// this; the inline TUI copy did not).
func TestRunGoalWithProgressStaleADCOnNonGoogleStillNeedsKey(t *testing.T) {
	originalFactory := newAgentRunnerForTUI
	t.Cleanup(func() { newAgentRunnerForTUI = originalFactory })
	builtRunner := make(chan struct{}, 1)
	newAgentRunnerForTUI = func(cfg *config.Config, options agent.Options, out io.Writer, errOut io.Writer) (agentRunnerAPI, error) {
		builtRunner <- struct{}{}
		return &fakeAgentRunner{}, nil
	}

	adapter := &TUIClientAdapter{
		baseConfig: &config.Config{APIKey: "", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini", GoogleUseADC: true},
	}

	var gotError string
	for _, msg := range runBatch(t, adapter.runGoalWithProgress([]string{"do", "a", "thing"}, 1)) {
		if p, ok := msg.(tui.AgentProgressMsg); ok && p.Kind == tui.AgentProgressError {
			gotError = p.Text
		}
	}
	assert.Equal(t, "no API key or credentials configured", gotError)
	select {
	case <-builtRunner:
		t.Error("a stale ADC flag on a non-Google base_url must not skip the key check")
	default:
	}
}

// /agent warnings go to the chat through the TUI's hook-warning path.
func TestTUIAgentWarnReachesChat(t *testing.T) {
	var got []string
	notify := func(s string) { got = append(got, s) }
	hookNotify.Store(&notify)
	t.Cleanup(func() { hookNotify.Store(nil) })
	tuiAgentOptions(nil).Warn("hooks: something failed")
	assert.Equal(t, []string{"hooks: something failed"}, got)
}

// Progress sends never block the agent on a UI that has stopped reading.
func TestSendAgentProgressDoesNotBlock(t *testing.T) {
	ch := make(chan tui.AgentProgressMsg) // unbuffered, nobody reading
	done := make(chan struct{})
	go func() {
		sendAgentProgress(ch, tui.AgentProgressMsg{Kind: tui.AgentProgressToolCall, Text: "x"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("an intermediate progress send blocked")
	}
}

// /agent's goal is the user's: UserPromptSubmit sees it once (2.0 F2e).
func TestTUIAgentOptionsCheckTheGoal(t *testing.T) {
	if !tuiAgentOptions(nil).CheckGoal {
		t.Fatal("/agent runs skip UserPromptSubmit on the goal")
	}
}

// An /agent goal run's context names the run (2.0 F2e), so the permission
// and ask requests it raises carry the tag to the chat.
func TestRunGoalWithProgressTagsItsContext(t *testing.T) {
	originalFactory := newAgentRunnerForTUI
	t.Cleanup(func() { newAgentRunnerForTUI = originalFactory })
	got := make(chan tui.RunOwner, 1)
	newAgentRunnerForTUI = func(*config.Config, agent.Options, io.Writer, io.Writer) (agentRunnerAPI, error) {
		return &fakeAgentRunner{runGoalFn: func(ctx context.Context, _ string) (*agent.RunState, error) {
			got <- tui.RunOwnerFrom(ctx)
			return &agent.RunState{Status: agent.StatusCompleted}, nil
		}}, nil
	}
	adapter := &TUIClientAdapter{baseConfig: &config.Config{APIKey: "k", BaseURL: "https://api.openai.com/v1", Model: "gpt-4o-mini"}}
	for _, msg := range runBatch(t, adapter.runGoalWithProgress([]string{"do", "it"}, 5)) {
		switch m := msg.(type) {
		case tui.StreamStartMsg:
			if m.AgentRun != 5 {
				t.Errorf("StreamStartMsg.AgentRun = %d, want 5", m.AgentRun)
			}
		case tui.AgentProgressMsg:
			if m.AgentRun != 5 {
				t.Errorf("AgentProgressMsg(%v).AgentRun = %d, want 5", m.Kind, m.AgentRun)
			}
		}
	}
	select {
	case o := <-got:
		if o != (tui.RunOwner{Kind: tui.OwnerAgent, Run: 5}) {
			t.Fatalf("run context owner = %+v", o)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the goal never ran")
	}
}
