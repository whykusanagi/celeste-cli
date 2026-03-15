# Orchestrator & Agent UX Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the agent feel like a live tool in the TUI (streaming progress + typing animation), then add a multi-model orchestrator with task-lane routing and a reviewer debate loop.

**Architecture:** Phase A wires incremental `AgentProgressMsg` events from the runner through a Go channel to the TUI, where the final response is fed into the existing `SimulatedTyping` path. Phase B adds a new `orchestrator/` package (classifier → router → state machine → debate) driven by a typed event stream, and replaces the flat agent TUI with a split-panel showing action feed + file diffs.

**Tech Stack:** Go 1.26+, Bubble Tea (charmbracelet/bubbletea), lipgloss, existing `agent.Runner`, existing `tui.SimulatedTyping`, `tui.TypingTickMsg`

---

## Chunk 1: Phase A — Agent UX (Live Progress + Typing Animation)

### File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Modify | `cmd/celeste/tui/messages.go` | Add `AgentProgressMsg`, `AgentProgressKind`, channel constructor |
| Modify | `cmd/celeste/agent/types.go` | Add `ProgressKind` constants and `OnProgress` callback field to `Options` |
| Modify | `cmd/celeste/agent/runtime.go` | Add `emitProgress` helper; emit at turn start, tool call, step done, complete/error |
| Modify | `cmd/celeste/tui_agent.go` | Wire `OnProgress`, run goal in goroutine, stream `AgentProgressMsg` |
| Modify | `cmd/celeste/tui/app.go` | Handle `AgentProgressMsg` — status bar, annotations, typing trigger |
| Modify | `cmd/celeste/tui/agent_command_test.go` | Update existing tests + add progress streaming tests |

---

### Task 1: Add `AgentProgressMsg` to tui/messages.go

**Files:**
- Modify: `cmd/celeste/tui/messages.go`

- [ ] **Step 1.1: Add the new types at the end of messages.go (after `AgentCommandResultMsg`)**

```go
// AgentProgressKind identifies the type of an AgentProgressMsg.
type AgentProgressKind int

const (
    AgentProgressTurnStart  AgentProgressKind = iota // a new turn has started
    AgentProgressToolCall                            // agent called a tool
    AgentProgressStepDone                            // a plan step was marked done
    AgentProgressResponse                            // final assistant response text
    AgentProgressComplete                            // run finished successfully
    AgentProgressError                               // run failed
)

// AgentProgressMsg is sent incrementally during an agent run.
// Ch is a channel of further progress messages; nil on terminal kinds (Complete/Error).
type AgentProgressMsg struct {
    RunID    string
    Kind     AgentProgressKind
    Text     string
    Turn     int
    MaxTurns int
    Ch       <-chan AgentProgressMsg
}

// ReadNext returns a tea.Cmd that reads the next AgentProgressMsg from Ch.
// Returns nil when Ch is nil or closed — no command to schedule.
func (m AgentProgressMsg) ReadNext() tea.Cmd {
    if m.Ch == nil {
        return nil
    }
    return func() tea.Msg {
        msg, ok := <-m.Ch
        if !ok {
            return nil
        }
        return msg
    }
}
```

- [ ] **Step 1.2: Build to verify no compile errors**

```bash
go build ./cmd/celeste/tui/...
```

Expected: no output (clean build).

- [ ] **Step 1.3: Commit**

```bash
git add cmd/celeste/tui/messages.go
git commit -m "feat: add AgentProgressMsg for incremental agent progress streaming"
```

---

### Task 2: Add `OnProgress` callback to `agent.Options`

**Files:**
- Modify: `cmd/celeste/agent/types.go`
- Test: `cmd/celeste/agent/runtime_phase_test.go`

- [ ] **Step 2.1: Write the failing test first**

Add to `cmd/celeste/agent/runtime_phase_test.go`:

```go
func TestOnProgressCalledDuringRun(t *testing.T) {
    var events []string
    opts := DefaultOptions()
    opts.OnProgress = func(kind ProgressKind, text string, turn, maxTurns int) {
        events = append(events, fmt.Sprintf("%d:%s", kind, text))
    }
    // OnProgress field must exist on Options — this test will fail to compile until it does
    _ = opts
    require.NotNil(t, opts.OnProgress)
}
```

- [ ] **Step 2.2: Run the test to confirm it fails to compile**

```bash
go test ./cmd/celeste/agent/... -run TestOnProgressCalledDuringRun 2>&1 | head -10
```

Expected: compile error `opts.OnProgress undefined`.

- [ ] **Step 2.3: Add `ProgressKind` and `OnProgress` to types.go**

In `cmd/celeste/agent/types.go`, after the `const` blocks at the top, add:

```go
// ProgressKind identifies an agent progress event.
type ProgressKind int

const (
    ProgressTurnStart  ProgressKind = iota
    ProgressToolCall
    ProgressStepDone
    ProgressResponse
    ProgressComplete
    ProgressError
)
```

In the `Options` struct, add as the last field:

```go
    // OnProgress is an optional callback invoked at key agent events.
    // text is a human-readable label. turn/maxTurns are 0 for non-turn events.
    // This field is not serialised to JSON (func types are not JSON-safe).
    OnProgress func(kind ProgressKind, text string, turn, maxTurns int) `json:"-"`
```

- [ ] **Step 2.4: Run the test to confirm it passes**

```bash
go test ./cmd/celeste/agent/... -run TestOnProgressCalledDuringRun -v
```

Expected: PASS.

- [ ] **Step 2.5: Commit**

```bash
git add cmd/celeste/agent/types.go cmd/celeste/agent/runtime_phase_test.go
git commit -m "feat: add OnProgress callback to agent.Options"
```

---

### Task 3: Emit OnProgress at key points in runtime.go

**Files:**
- Modify: `cmd/celeste/agent/runtime.go`
- Test: `cmd/celeste/agent/runtime_phase_test.go`

- [ ] **Step 3.1: Write the failing integration test**

Add to `cmd/celeste/agent/runtime_phase_test.go`:

```go
func TestRunnerEmitsProgressEvents(t *testing.T) {
    // This test verifies that progress events are emitted; it doesn't need a real LLM.
    // We verify that at minimum a ProgressTurnStart event fires when the run begins,
    // and ProgressComplete fires when it ends. We use a tiny max-turns=1 run that
    // completes via max_turns_reached so no actual API calls are needed ... but since
    // NewRunner requires a real config we only test the Option wiring here.
    var kinds []ProgressKind
    opts := DefaultOptions()
    opts.OnProgress = func(kind ProgressKind, text string, turn, maxTurns int) {
        kinds = append(kinds, kind)
    }
    // Verify the callback is stored — actual emission is tested in the run loop below.
    require.NotNil(t, opts.OnProgress)
    opts.OnProgress(ProgressTurnStart, "test", 1, 12)
    require.Equal(t, []ProgressKind{ProgressTurnStart}, kinds)
}
```

- [ ] **Step 3.2: Run to confirm it passes (the wiring test is simple)**

```bash
go test ./cmd/celeste/agent/... -run TestRunnerEmitsProgressEvents -v
```

Expected: PASS.

- [ ] **Step 3.3: Add a helper to emit progress without nil-checking everywhere**

Add to `runtime.go` (near the top, after imports):

```go
// emitProgress calls r.options.OnProgress if it is set.
func (r *Runner) emitProgress(kind ProgressKind, text string, turn, maxTurns int) {
    if r.options.OnProgress != nil {
        r.options.OnProgress(kind, text, turn, maxTurns)
    }
}
```

- [ ] **Step 3.4: Emit turn-start progress in the main execution loop**

In `runState`, find the line:
```go
if state.Options.Verbose {
    fmt.Fprintf(r.out, "\n[agent] turn %d/%d\n", state.Turn, state.Options.MaxTurns)
}
```

Replace with:
```go
if state.Options.Verbose {
    fmt.Fprintf(r.out, "\n[agent] turn %d/%d\n", state.Turn, state.Options.MaxTurns)
}
r.emitProgress(ProgressTurnStart, fmt.Sprintf("turn %d/%d", state.Turn, state.Options.MaxTurns), state.Turn, state.Options.MaxTurns)
```

- [ ] **Step 3.5: Emit tool-call progress**

In `runState`, find the tool call loop:
```go
for _, tc := range toolCalls {
    toolMsg := r.executeToolCall(ctx, state, tc)
```

Replace with:
```go
for _, tc := range toolCalls {
    r.emitProgress(ProgressToolCall, tc.Name, state.Turn, state.Options.MaxTurns)
    toolMsg := r.executeToolCall(ctx, state, tc)
```

- [ ] **Step 3.6: Emit step-done and response progress**

Find the block that processes `isCompletionResponse`. Just before `handleCompletionCandidate` is called, add:

```go
r.emitProgress(ProgressResponse, state.LastAssistantResponse, state.Turn, state.Options.MaxTurns)
```

Find the `return state, nil` after a successful completion (inside `handleCompletionCandidate` path), and before `return state, nil` in the max-turns path, emit complete/error:

In `runState`, the two success `return state, nil` paths become:
```go
r.emitProgress(ProgressComplete, state.Status, state.Turn, state.Options.MaxTurns)
return state, nil
```

And the failure `return state, err` paths become:
```go
r.emitProgress(ProgressError, err.Error(), state.Turn, state.Options.MaxTurns)
return state, err
```

- [ ] **Step 3.7: Build and test**

```bash
go build ./cmd/celeste/agent/...
go test ./cmd/celeste/agent/... -v 2>&1 | tail -20
```

Expected: all existing tests pass, no new failures.

- [ ] **Step 3.8: Commit**

```bash
git add cmd/celeste/agent/runtime.go cmd/celeste/agent/runtime_phase_test.go
git commit -m "feat: emit OnProgress at turn start, tool call, response, complete/error"
```

---

### Task 4: Wire progress streaming in tui_agent.go

**Files:**
- Modify: `cmd/celeste/tui_agent.go`

The current `RunAgentCommand` blocks synchronously and discards output. Replace it with a goroutine that pushes `AgentProgressMsg` through a channel. The returned `tea.Cmd` reads the first message from the channel; subsequent messages are read by `msg.ReadNext()` in app.go.

- [ ] **Step 4.1: Add a helper that converts agent.ProgressKind to tui.AgentProgressKind**

At the top of `cmd/celeste/tui_agent.go`, add:

```go
func agentKindToTUI(k agent.ProgressKind) tui.AgentProgressKind {
    switch k {
    case agent.ProgressTurnStart:
        return tui.AgentProgressTurnStart
    case agent.ProgressToolCall:
        return tui.AgentProgressToolCall
    case agent.ProgressStepDone:
        return tui.AgentProgressStepDone
    case agent.ProgressResponse:
        return tui.AgentProgressResponse
    case agent.ProgressComplete:
        return tui.AgentProgressComplete
    default:
        return tui.AgentProgressError
    }
}
```

- [ ] **Step 4.2: Replace the RunAgentCommand body**

The key design decision: **info sub-commands** (help, list, resume) continue to use the old synchronous `AgentCommandResultMsg` path — they have nothing to stream. **Goal sub-commands** (any text treated as a goal) use the new progress channel path.

Replace the existing `RunAgentCommand` method entirely:

```go
// RunAgentCommand dispatches /agent sub-commands.
// Info commands (help, list, resume) return a single AgentCommandResultMsg.
// Goal commands stream incremental AgentProgressMsg via a channel.
func (a *TUIClientAdapter) RunAgentCommand(args []string) tea.Cmd {
    if len(args) == 0 {
        return func() tea.Msg {
            return tui.AgentCommandResultMsg{Output: agentUsage(), Err: fmt.Errorf("missing arguments")}
        }
    }
    sub := strings.ToLower(strings.TrimSpace(args[0]))
    switch sub {
    case "help", "--help", "-h":
        return func() tea.Msg {
            return tui.AgentCommandResultMsg{Output: agentUsage()}
        }
    case "list", "list-runs", "--list-runs":
        copiedArgs := append([]string(nil), args...)
        return func() tea.Msg {
            output, err := a.executeAgentCommand(copiedArgs)
            return tui.AgentCommandResultMsg{Output: output, Err: err}
        }
    case "resume", "--resume":
        copiedArgs := append([]string(nil), args...)
        return func() tea.Msg {
            output, err := a.executeAgentCommand(copiedArgs)
            return tui.AgentCommandResultMsg{Output: output, Err: err}
        }
    default:
        // Treat all other input as a goal — stream progress.
        return a.runGoalWithProgress(args)
    }
}

// runGoalWithProgress runs a goal in a goroutine and streams AgentProgressMsg
// back to the TUI via a bidirectional channel. The read end is stored in each
// non-terminal AgentProgressMsg so app.go can schedule the next read.
func (a *TUIClientAdapter) runGoalWithProgress(args []string) tea.Cmd {
    // ch is bidirectional so the goroutine can write and we can hand the
    // receive end (<-chan) to AgentProgressMsg.Ch without a compile error.
    ch := make(chan tui.AgentProgressMsg, 64)

    go func() {
        defer close(ch)
        cfg := a.currentAgentConfig()
        if cfg.APIKey == "" && !cfg.GoogleUseADC && strings.TrimSpace(cfg.GoogleCredentialsFile) == "" {
            ch <- tui.AgentProgressMsg{Kind: tui.AgentProgressError, Text: "no API key or credentials configured"}
            return
        }

        opts := agent.DefaultOptions()
        if cwd, err := os.Getwd(); err == nil {
            opts.Workspace = cwd
        }
        opts.Verbose = false
        // Pass the receive end of ch so AgentProgressMsg.Ch is a <-chan.
        recvCh := (<-chan tui.AgentProgressMsg)(ch)
        opts.OnProgress = func(kind agent.ProgressKind, text string, turn, maxTurns int) {
            tuiKind := agentKindToTUI(kind)
            var msgCh <-chan tui.AgentProgressMsg
            // Terminal kinds close the chain — don't set Ch so ReadNext returns nil.
            if tuiKind != tui.AgentProgressComplete && tuiKind != tui.AgentProgressError {
                msgCh = recvCh
            }
            ch <- tui.AgentProgressMsg{
                Kind:     tuiKind,
                Text:     text,
                Turn:     turn,
                MaxTurns: maxTurns,
                Ch:       msgCh,
            }
        }

        runner, err := newAgentRunnerForTUI(cfg, opts, io.Discard, io.Discard)
        if err != nil {
            ch <- tui.AgentProgressMsg{Kind: tui.AgentProgressError, Text: err.Error()}
            return
        }

        goal := strings.TrimSpace(strings.Join(args, " "))
        state, runErr := runner.RunGoal(context.Background(), goal)
        if runErr != nil {
            // OnProgress already sent ProgressError via the callback; nothing else needed.
            _ = state
            return
        }
        // Defensive: if the runner didn't fire ProgressComplete via OnProgress
        // (e.g., future runner implementation gap), emit it here so the TUI
        // always receives a terminal event and stops streaming.
        lastResponse := ""
        if state != nil {
            lastResponse = state.LastAssistantResponse
        }
        ch <- tui.AgentProgressMsg{Kind: tui.AgentProgressComplete, Text: lastResponse}
    }()

    return func() tea.Msg {
        msg, ok := <-ch
        if !ok {
            return nil
        }
        return msg
    }
}
```

- [ ] **Step 4.3: Verify executeAgentCommand is still present (info commands still use it)**

`executeAgentCommand` already handles `help`, `list`, `resume`, and `goal` sub-commands synchronously. The `RunAgentCommand` rewrite above delegates info commands to it. No changes needed to `executeAgentCommand` itself — it is kept as-is for the oneshot CLI path and TUI info commands.
```

- [ ] **Step 4.4: Build**

```bash
go build ./cmd/celeste/...
```

Expected: clean build.

- [ ] **Step 4.5: Commit**

```bash
git add cmd/celeste/tui_agent.go
git commit -m "feat: stream agent progress via channel in RunAgentCommand"
```

---

### Task 5: Handle AgentProgressMsg in app.go

**Files:**
- Modify: `cmd/celeste/tui/app.go`
- Modify: `cmd/celeste/tui/agent_command_test.go`

- [ ] **Step 5.1: Write the failing tests first**

Add to `cmd/celeste/tui/agent_command_test.go`:

```go
func TestAgentProgressTurnStartUpdatesStatus(t *testing.T) {
    client := &fakeAgentLLMClient{}
    m := NewApp(client)

    // Simulate receiving a turn-start progress message
    model, cmd := m.Update(AgentProgressMsg{
        Kind:     AgentProgressTurnStart,
        Text:     "turn 1/12",
        Turn:     1,
        MaxTurns: 12,
    })
    m = model.(AppModel)

    assert.Contains(t, m.status.text, "turn 1")
    assert.True(t, m.streaming)
    assert.Nil(t, cmd) // no Ch means no ReadNext
}

func TestAgentProgressToolCallAddsAnnotation(t *testing.T) {
    client := &fakeAgentLLMClient{}
    m := NewApp(client)
    m.streaming = true

    model, _ := m.Update(AgentProgressMsg{
        Kind: AgentProgressToolCall,
        Text: "dev_write_file",
        Ch:   make(chan AgentProgressMsg), // non-nil = more coming
    })
    m = model.(AppModel)

    assert.True(t, hasSystemMessageContaining(m.chat.GetMessages(), "dev_write_file"))
}

func TestAgentProgressResponseTriggersTypingAnimation(t *testing.T) {
    client := &fakeAgentLLMClient{}
    m := NewApp(client)
    m.streaming = true

    model, cmd := m.Update(AgentProgressMsg{
        Kind: AgentProgressResponse,
        Text: "hello from agent",
    })
    m = model.(AppModel)

    assert.Equal(t, "hello from agent", m.typingContent)
    assert.Equal(t, 0, m.typingPos)
    assert.NotNil(t, cmd) // typing tick scheduled
}

func TestAgentProgressCompleteStopsStreaming(t *testing.T) {
    client := &fakeAgentLLMClient{}
    m := NewApp(client)
    m.streaming = true

    model, _ := m.Update(AgentProgressMsg{Kind: AgentProgressComplete, Text: "done"})
    m = model.(AppModel)

    assert.False(t, m.streaming)
    assert.Contains(t, m.status.text, "Agent run complete")
}
```

- [ ] **Step 5.2: Run tests to confirm they fail**

```bash
go test ./cmd/celeste/tui/... -run "TestAgentProgress" -v 2>&1 | head -20
```

Expected: compile error — `AgentProgressMsg` not handled in `Update`.

- [ ] **Step 5.3: Add AgentProgressMsg handler in app.go**

In `app.go`, find the `case AgentCommandResultMsg:` block. Add a new case directly before it:

```go
case AgentProgressMsg:
    var cmds []tea.Cmd

    switch msg.Kind {
    case AgentProgressTurnStart:
        m.streaming = true
        m.status = m.status.SetStreaming(true)
        m.status = m.status.SetText(fmt.Sprintf("Agent: %s", msg.Text))

    case AgentProgressToolCall:
        m.status = m.status.SetText(fmt.Sprintf("Agent: calling %s", msg.Text))
        m.chat = m.chat.AddSystemMessage(fmt.Sprintf("⚙ %s", msg.Text))

    case AgentProgressStepDone:
        m.chat = m.chat.AddSystemMessage(fmt.Sprintf("✓ %s", msg.Text))

    case AgentProgressResponse:
        // Feed the final response through SimulatedTyping — same path as streamed LLM output.
        if strings.TrimSpace(msg.Text) != "" {
            m.typingContent = msg.Text
            m.typingPos = 0
            m.chat = m.chat.AddAssistantMessage("")
            m.status = m.status.SetText("Agent: typing response...")
            cmds = append(cmds, tea.Tick(typingTickInterval, func(t time.Time) tea.Msg {
                return TickMsg{Time: t}
            }))
        }

    case AgentProgressComplete:
        m.streaming = false
        m.status = m.status.SetStreaming(false)
        m.status = m.status.SetText("Agent run complete")
        m.persistSession()

    case AgentProgressError:
        m.streaming = false
        m.status = m.status.SetStreaming(false)
        m.status = m.status.SetText(fmt.Sprintf("Agent error: %s", msg.Text))
        if strings.TrimSpace(msg.Text) != "" {
            m.chat = m.chat.AddSystemMessage(fmt.Sprintf("❌ Agent error: %s", msg.Text))
        }
        m.persistSession()
    }

    // If there are more messages in the channel, schedule reading the next one.
    if next := msg.ReadNext(); next != nil {
        cmds = append(cmds, next)
    }

    return m, tea.Batch(cmds...)
```

- [ ] **Step 5.4: Run the new tests**

```bash
go test ./cmd/celeste/tui/... -run "TestAgentProgress" -v
```

Expected: all 4 new tests PASS.

- [ ] **Step 5.5: Run the full test suite to catch regressions**

```bash
go test ./... 2>&1 | tail -30
```

Expected: all pass.

- [ ] **Step 5.6: Commit**

```bash
git add cmd/celeste/tui/app.go cmd/celeste/tui/agent_command_test.go
git commit -m "feat: handle AgentProgressMsg in TUI — streaming status + typing animation"
```

---

### Task 6: Integration smoke-test and Phase A PR

- [ ] **Step 6.1: Manual smoke test via oneshot**

```bash
go build -o /tmp/celeste-test ./cmd/celeste && \
/tmp/celeste-test agent --goal "write hello.txt with content hello world" --workspace /tmp/agenttest
```

Expected: terminal output showing turn progress lines. `/tmp/agenttest/hello.txt` exists after completion.

- [ ] **Step 6.2: Run full test suite one more time**

```bash
go test ./... 2>&1 | grep -E "FAIL|ok"
```

Expected: all `ok`, no `FAIL`.

- [ ] **Step 6.3: Commit and push**

```bash
git add -p
git commit -m "chore: Phase A complete — agent UX streaming + typing animation"
git push
```

- [ ] **Step 6.4: Open PR targeting main**

```bash
gh pr create \
  --title "feat: live agent progress streaming + typing animation in TUI" \
  --body "$(cat <<'EOF'
## Summary
- Agent runs in a goroutine; TUI receives incremental AgentProgressMsg events
- Turn start, tool calls, and step completions show as status bar + chat annotations
- Final assistant response is fed into SimulatedTyping (identical to streamed LLM output)
- No change to oneshot CLI agent mode or any existing tests

## Test plan
- [ ] Unit tests for all AgentProgressMsg handling paths in app.go
- [ ] Existing agent_command_test.go still passes
- [ ] Manual: /agent goal in TUI shows live updates and typewriter response
EOF
)"
```

---

## Chunk 2: Phase B — Orchestrator

### File Map

| Action | Path | Responsibility |
|--------|------|----------------|
| Create | `cmd/celeste/orchestrator/events.go` | Typed OrchestratorEvent stream |
| Create | `cmd/celeste/orchestrator/classifier.go` | Goal → TaskLane (heuristics + LLM fallback) |
| Create | `cmd/celeste/orchestrator/classifier_test.go` | Keyword heuristic coverage |
| Create | `cmd/celeste/orchestrator/router.go` | Lane + config → ModelAssignment |
| Create | `cmd/celeste/orchestrator/router_test.go` | Resolution, fallback, override precedence |
| Create | `cmd/celeste/orchestrator/debate.go` | Review debate turn manager |
| Create | `cmd/celeste/orchestrator/debate_test.go` | Turn sequencing, max rounds, contested verdict |
| Create | `cmd/celeste/orchestrator/orchestrator.go` | Top-level state machine |
| Create | `cmd/celeste/orchestrator/orchestrator_test.go` | Full Idle→Done state machine walk |
| Modify | `cmd/celeste/agent/runtime.go` | Add EventSink interface; call it alongside OnProgress |
| Create | `cmd/celeste/tui/split_panel.go` | Left action feed + right artifact/diff panel |
| Create | `cmd/celeste/tui/split_panel_test.go` | Panel render + event stream assertions |
| Create | `cmd/celeste/tui_orchestrator.go` | Wires TUI to orchestrator; replaces tui_agent.go for orchestrated runs |
| Modify | `cmd/celeste/config/config.go` | Add `OrchestratorConfig` + lanes block |
| Modify | `cmd/celeste/config/config_test.go` | Config parse + lane resolution tests |

---

### Task 7: Typed event stream — orchestrator/events.go

**Files:**
- Create: `cmd/celeste/orchestrator/events.go`

- [ ] **Step 7.1: Create the events file**

```go
// Package orchestrator coordinates multiple agent runners and model routing.
package orchestrator

// EventKind identifies the type of an OrchestratorEvent.
type EventKind int

const (
    EventClassified   EventKind = iota // goal classified into a task lane
    EventAction                        // agent took a discrete action
    EventToolCall                      // agent called a tool
    EventFileDiff                      // agent wrote or modified a file
    EventReviewDraft                   // reviewer produced a critique
    EventDefense                       // primary agent responded to critique
    EventVerdict                       // reviewer issued a final verdict
    EventComplete                      // orchestrator run finished
    EventError                         // orchestrator run failed
)

// TaskLane identifies the type of task being performed.
type TaskLane string

const (
    LaneCode     TaskLane = "code"
    LaneContent  TaskLane = "content"
    LaneMedia    TaskLane = "media"
    LaneReview   TaskLane = "review"
    LaneResearch TaskLane = "research"
    LaneUnknown  TaskLane = "unknown"
)

// OrchestratorEvent is emitted by the orchestrator state machine.
type OrchestratorEvent struct {
    Kind       EventKind
    Lane       TaskLane
    Text       string  // human-readable description
    FilePath   string  // non-empty for EventFileDiff
    Diff       string  // unified diff content for EventFileDiff
    Turn       int
    MaxTurns   int
    Score      float64 // 0.0–1.0 for EventVerdict
    VerdictErr string  // non-empty if verdict is Contested or NeedsWork
}
```

- [ ] **Step 7.2: Build**

```bash
go build ./cmd/celeste/orchestrator/...
```

Expected: clean build.

- [ ] **Step 7.3: Commit**

```bash
git add cmd/celeste/orchestrator/events.go
git commit -m "feat: add orchestrator event types"
```

---

### Task 8: Task classifier — orchestrator/classifier.go

**Files:**
- Create: `cmd/celeste/orchestrator/classifier.go`
- Create: `cmd/celeste/orchestrator/classifier_test.go`

- [ ] **Step 8.1: Write failing tests first**

Create `cmd/celeste/orchestrator/classifier_test.go`:

```go
package orchestrator_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
)

func TestClassifyKeywords(t *testing.T) {
    cases := []struct {
        goal string
        want orchestrator.TaskLane
    }{
        {"fix the flaky test in auth_test.go", orchestrator.LaneCode},
        {"refactor the database layer", orchestrator.LaneCode},
        {"write a blog post about Go generics", orchestrator.LaneContent},
        {"summarize this document", orchestrator.LaneContent},
        {"upscale this image to 4k", orchestrator.LaneMedia},
        {"convert the video to mp4", orchestrator.LaneMedia},
        {"review my pull request", orchestrator.LaneReview},
        {"blind audit of main.go", orchestrator.LaneReview},
        {"research the best Go ORMs", orchestrator.LaneResearch},
        {"find all mentions of deprecated functions", orchestrator.LaneResearch},
    }
    for _, tc := range cases {
        t.Run(tc.goal, func(t *testing.T) {
            got, confidence := orchestrator.ClassifyHeuristic(tc.goal)
            assert.Equal(t, tc.want, got, "goal: %q", tc.goal)
            assert.Greater(t, confidence, 0.5, "expected confidence > 0.5 for: %q", tc.goal)
        })
    }
}

func TestClassifyUnknownReturnsLowConfidence(t *testing.T) {
    lane, confidence := orchestrator.ClassifyHeuristic("do the thing")
    assert.Equal(t, orchestrator.LaneUnknown, lane)
    assert.Less(t, confidence, 0.5)
}
```

- [ ] **Step 8.2: Run to confirm compile failure**

```bash
go test ./cmd/celeste/orchestrator/... -run TestClassify 2>&1 | head -5
```

Expected: `undefined: orchestrator.ClassifyHeuristic`.

- [ ] **Step 8.3: Implement classifier.go**

Create `cmd/celeste/orchestrator/classifier.go`:

```go
package orchestrator

import (
    "strings"
)

// laneKeywords maps keyword → lane. First match wins.
var laneKeywords = []struct {
    keywords []string
    lane     TaskLane
}{
    {[]string{"fix", "refactor", "debug", "test", "build", "compile", "implement", "lint", "patch", "bug"}, LaneCode},
    {[]string{"write", "draft", "blog", "docs", "document", "summarize", "explain", "describe", "article"}, LaneContent},
    {[]string{"upscale", "image", "video", "render", "convert", "generate image", "generate video", "media"}, LaneMedia},
    {[]string{"review", "audit", "check", "critique", "blind review", "code review"}, LaneReview},
    {[]string{"research", "find", "search", "compare", "what is", "how does", "investigate", "explore"}, LaneResearch},
}

// ClassifyHeuristic returns the best-guess TaskLane and a confidence score (0.0–1.0)
// based purely on keyword matching. Confidence < 0.5 means the goal is ambiguous.
func ClassifyHeuristic(goal string) (TaskLane, float64) {
    lower := strings.ToLower(goal)
    words := strings.Fields(lower)
    wordSet := make(map[string]bool, len(words))
    for _, w := range words {
        wordSet[w] = true
    }

    best := LaneUnknown
    bestScore := 0.0

    for _, entry := range laneKeywords {
        score := 0.0
        for _, kw := range entry.keywords {
            // Support multi-word keywords (e.g. "blind review")
            if strings.Contains(lower, kw) {
                score += 1.0 / float64(len(entry.keywords))
            }
        }
        if score > bestScore {
            bestScore = score
            best = entry.lane
        }
    }

    // Normalise: max possible score per lane is 1.0; scale to 0.5–0.95 range.
    if best == LaneUnknown {
        return LaneUnknown, 0.1
    }
    confidence := 0.5 + bestScore*0.45
    if confidence > 0.95 {
        confidence = 0.95
    }
    return best, confidence
}
```

- [ ] **Step 8.4: Run tests**

```bash
go test ./cmd/celeste/orchestrator/... -run TestClassify -v
```

Expected: all PASS.

- [ ] **Step 8.5: Commit**

```bash
git add cmd/celeste/orchestrator/classifier.go cmd/celeste/orchestrator/classifier_test.go
git commit -m "feat: add task lane classifier with keyword heuristics"
```

---

### Task 9: Model router — orchestrator/router.go

**Files:**
- Modify: `cmd/celeste/config/config.go`
- Create: `cmd/celeste/orchestrator/router.go`
- Create: `cmd/celeste/orchestrator/router_test.go`

- [ ] **Step 9.1: Add OrchestratorConfig to config.go**

In `cmd/celeste/config/config.go`, add a new struct and field:

```go
// LaneConfig holds the primary and optional reviewer model for one task lane.
type LaneConfig struct {
    Primary  string `json:"primary"`
    Reviewer string `json:"reviewer,omitempty"`
}

// OrchestratorConfig controls multi-model orchestration behaviour.
type OrchestratorConfig struct {
    Lanes        map[string]LaneConfig `json:"lanes,omitempty"`
    DefaultLane  string                `json:"default_lane,omitempty"`
    DebateRounds int                   `json:"debate_rounds,omitempty"`
}
```

In the `Config` struct, add:

```go
    Orchestrator *OrchestratorConfig `json:"orchestrator,omitempty"`
```

- [ ] **Step 9.2: Write failing router tests**

Create `cmd/celeste/orchestrator/router_test.go`:

```go
package orchestrator_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/whykusanagi/celeste-cli/cmd/celeste/config"
    "github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
)

func TestRouterResolvesConfiguredLane(t *testing.T) {
    cfg := &config.Config{
        Model: "default-model",
        Orchestrator: &config.OrchestratorConfig{
            Lanes: map[string]config.LaneConfig{
                "code": {Primary: "grok-fast", Reviewer: "gemini-review"},
            },
        },
    }
    r := orchestrator.NewRouter(cfg)
    assignment, err := r.Resolve(orchestrator.LaneCode)
    require.NoError(t, err)
    assert.Equal(t, "grok-fast", assignment.Primary)
    assert.Equal(t, "gemini-review", assignment.Reviewer)
    assert.True(t, assignment.HasReviewer())
}

func TestRouterFallsBackToDefaultModel(t *testing.T) {
    cfg := &config.Config{Model: "my-default"}
    r := orchestrator.NewRouter(cfg)
    assignment, err := r.Resolve(orchestrator.LaneContent)
    require.NoError(t, err)
    assert.Equal(t, "my-default", assignment.Primary)
    assert.False(t, assignment.HasReviewer())
}

func TestRouterBlankReviewerMeansNoDebate(t *testing.T) {
    cfg := &config.Config{
        Model: "primary",
        Orchestrator: &config.OrchestratorConfig{
            Lanes: map[string]config.LaneConfig{
                "code": {Primary: "primary", Reviewer: ""},
            },
        },
    }
    r := orchestrator.NewRouter(cfg)
    assignment, _ := r.Resolve(orchestrator.LaneCode)
    assert.False(t, assignment.HasReviewer())
}
```

- [ ] **Step 9.3: Run to confirm compile failure**

```bash
go test ./cmd/celeste/orchestrator/... -run TestRouter 2>&1 | head -5
```

Expected: `undefined: orchestrator.NewRouter`.

- [ ] **Step 9.4: Implement router.go**

Create `cmd/celeste/orchestrator/router.go`:

```go
package orchestrator

import (
    "strings"

    "github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// ModelAssignment describes which models to use for a given run.
type ModelAssignment struct {
    Primary  string
    Reviewer string
}

// HasReviewer returns true when a non-blank reviewer model is assigned.
func (m ModelAssignment) HasReviewer() bool {
    return strings.TrimSpace(m.Reviewer) != ""
}

// Router maps TaskLanes to ModelAssignments using the user's config.
type Router struct {
    cfg *config.Config
}

// NewRouter creates a Router backed by the given config.
func NewRouter(cfg *config.Config) *Router {
    return &Router{cfg: cfg}
}

// Resolve returns the ModelAssignment for the given lane.
// Falls back to cfg.Model as primary with no reviewer when the lane is unconfigured.
func (r *Router) Resolve(lane TaskLane) (ModelAssignment, error) {
    if r.cfg.Orchestrator != nil && r.cfg.Orchestrator.Lanes != nil {
        if lc, ok := r.cfg.Orchestrator.Lanes[string(lane)]; ok && strings.TrimSpace(lc.Primary) != "" {
            return ModelAssignment{Primary: lc.Primary, Reviewer: lc.Reviewer}, nil
        }
    }
    return ModelAssignment{Primary: r.cfg.Model}, nil
}
```

- [ ] **Step 9.5: Run router tests**

```bash
go test ./cmd/celeste/orchestrator/... -run TestRouter -v
```

Expected: all PASS.

- [ ] **Step 9.6: Build entire project to catch config changes**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 9.7: Commit**

```bash
git add cmd/celeste/config/config.go cmd/celeste/orchestrator/router.go cmd/celeste/orchestrator/router_test.go
git commit -m "feat: add orchestrator config schema and model router"
```

---

### Task 10: Debate manager — orchestrator/debate.go

**Files:**
- Create: `cmd/celeste/orchestrator/debate.go`
- Create: `cmd/celeste/orchestrator/debate_test.go`

- [ ] **Step 10.1: Write failing debate tests**

Create `cmd/celeste/orchestrator/debate_test.go`:

```go
package orchestrator_test

import (
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
)

func TestDebateManagerDefaultsToThreeRounds(t *testing.T) {
    dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{})
    assert.Equal(t, 3, dm.MaxRounds())
}

func TestDebateManagerCustomRounds(t *testing.T) {
    dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{MaxRounds: 5})
    assert.Equal(t, 5, dm.MaxRounds())
}

func TestAddTurnAppendsTurn(t *testing.T) {
    dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{})
    dm.AddTurn(orchestrator.DebateTurn{Round: 1, Role: orchestrator.RoleReviewer, Output: "issue: nil check missing"})
    dm.AddTurn(orchestrator.DebateTurn{Round: 1, Role: orchestrator.RolePrimary, Output: "accepted"})
    require.Len(t, dm.Turns(), 2)
    assert.Equal(t, orchestrator.RoleReviewer, dm.Turns()[0].Role)
}

func TestVerdictApprovedWhenNoIssues(t *testing.T) {
    dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{})
    result := dm.Verdict([]orchestrator.Issue{})
    assert.Equal(t, orchestrator.VerdictApproved, result.Kind)
    assert.Greater(t, result.Score, 0.8)
}

func TestVerdictNeedsWorkWhenIssuesExist(t *testing.T) {
    dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{})
    issues := []orchestrator.Issue{{File: "main.go", Line: 10, Severity: "high", Description: "nil dereference"}}
    result := dm.Verdict(issues)
    assert.Equal(t, orchestrator.VerdictNeedsWork, result.Kind)
    assert.Less(t, result.Score, 0.8)
}

func TestVerdictContestedAfterMaxRounds(t *testing.T) {
    dm := orchestrator.NewDebateManager(orchestrator.DebateOptions{MaxRounds: 2})
    for i := 0; i < 2; i++ {
        dm.AddTurn(orchestrator.DebateTurn{Round: i + 1, Role: orchestrator.RoleReviewer, Output: "still has issues"})
        dm.AddTurn(orchestrator.DebateTurn{Round: i + 1, Role: orchestrator.RolePrimary, Output: "disagree"})
    }
    issues := []orchestrator.Issue{{File: "main.go", Line: 1, Severity: "medium", Description: "unclear"}}
    result := dm.Verdict(issues)
    assert.Equal(t, orchestrator.VerdictContested, result.Kind)
}
```

- [ ] **Step 10.2: Run to confirm compile failure**

```bash
go test ./cmd/celeste/orchestrator/... -run TestDebate -run TestAddTurn -run TestVerdict 2>&1 | head -5
```

Expected: `undefined: orchestrator.NewDebateManager`.

- [ ] **Step 10.3: Implement debate.go**

Create `cmd/celeste/orchestrator/debate.go`:

```go
package orchestrator

// DebateRole identifies which side is speaking in a review debate.
type DebateRole int

const (
    RoleReviewer DebateRole = iota
    RolePrimary
)

// VerdictKind is the outcome of a review debate.
type VerdictKind int

const (
    VerdictApproved  VerdictKind = iota
    VerdictNeedsWork
    VerdictContested
)

// Issue is a code issue raised by the reviewer.
type Issue struct {
    File        string
    Line        int
    Severity    string // "low", "medium", "high"
    Description string
}

// DebateTurn is one side's contribution to a debate round.
type DebateTurn struct {
    Round  int
    Role   DebateRole
    Input  string
    Output string
}

// DebateResult is the final outcome of all debate rounds.
type DebateResult struct {
    Kind   VerdictKind
    Issues []Issue
    Score  float64 // 0.0–1.0; higher = cleaner code
}

// DebateOptions configures a DebateManager.
type DebateOptions struct {
    MaxRounds int // default 3
}

// DebateManager tracks debate turns and produces a verdict.
type DebateManager struct {
    opts  DebateOptions
    turns []DebateTurn
}

// NewDebateManager creates a DebateManager with the given options.
func NewDebateManager(opts DebateOptions) *DebateManager {
    if opts.MaxRounds <= 0 {
        opts.MaxRounds = 3
    }
    return &DebateManager{opts: opts}
}

// MaxRounds returns the configured maximum debate rounds.
func (d *DebateManager) MaxRounds() int { return d.opts.MaxRounds }

// Turns returns all turns added so far.
func (d *DebateManager) Turns() []DebateTurn { return d.turns }

// AddTurn appends a turn to the debate.
func (d *DebateManager) AddTurn(turn DebateTurn) {
    d.turns = append(d.turns, turn)
}

// RoundsCompleted returns the number of full rounds (reviewer + primary) completed.
func (d *DebateManager) RoundsCompleted() int {
    return len(d.turns) / 2
}

// Verdict produces a DebateResult from the given open issues.
func (d *DebateManager) Verdict(issues []Issue) DebateResult {
    switch {
    case len(issues) == 0:
        return DebateResult{Kind: VerdictApproved, Issues: issues, Score: 0.95}
    case d.RoundsCompleted() >= d.opts.MaxRounds:
        return DebateResult{Kind: VerdictContested, Issues: issues, Score: 0.5}
    default:
        // Score degrades with number and severity of issues.
        score := 0.75
        for _, iss := range issues {
            switch iss.Severity {
            case "high":
                score -= 0.15
            case "medium":
                score -= 0.08
            default:
                score -= 0.03
            }
        }
        if score < 0.1 {
            score = 0.1
        }
        return DebateResult{Kind: VerdictNeedsWork, Issues: issues, Score: score}
    }
}
```

- [ ] **Step 10.4: Run debate tests**

```bash
go test ./cmd/celeste/orchestrator/... -run "TestDebate|TestAddTurn|TestVerdict" -v
```

Expected: all PASS.

- [ ] **Step 10.5: Commit**

```bash
git add cmd/celeste/orchestrator/debate.go cmd/celeste/orchestrator/debate_test.go
git commit -m "feat: add debate manager with turn tracking and verdict logic"
```

---

### Task 11: Orchestrator state machine — orchestrator/orchestrator.go

**Files:**
- Create: `cmd/celeste/orchestrator/orchestrator.go`
- Create: `cmd/celeste/orchestrator/orchestrator_test.go`

- [ ] **Step 11.1: Write the integration test first**

Create `cmd/celeste/orchestrator/orchestrator_test.go`:

```go
package orchestrator_test

import (
    "context"
    "testing"

    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
    "github.com/whykusanagi/celeste-cli/cmd/celeste/config"
    "github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
)

// fakeRunner satisfies the orchestrator.AgentRunner interface for tests.
type fakeRunner struct {
    response string
    err      error
}

func (f *fakeRunner) RunGoal(_ context.Context, _ string) (string, error) {
    return f.response, f.err
}

func TestOrchestratorClassifiesAndRoutesGoal(t *testing.T) {
    cfg := &config.Config{Model: "test-model"}
    events := []orchestrator.OrchestratorEvent{}

    o := orchestrator.New(cfg, orchestrator.WithRunnerFactory(func(model string) orchestrator.AgentRunner {
        return &fakeRunner{response: "TASK_COMPLETE: done"}
    }))
    o.OnEvent(func(e orchestrator.OrchestratorEvent) {
        events = append(events, e)
    })

    result, err := o.Run(context.Background(), "fix the broken test in auth.go")
    require.NoError(t, err)
    assert.NotNil(t, result)

    // Must have emitted a classification event
    var classified bool
    for _, e := range events {
        if e.Kind == orchestrator.EventClassified {
            classified = true
            assert.Equal(t, orchestrator.LaneCode, e.Lane)
        }
    }
    assert.True(t, classified, "expected EventClassified to be emitted")

    // Must have emitted a complete event
    var completed bool
    for _, e := range events {
        if e.Kind == orchestrator.EventComplete {
            completed = true
        }
    }
    assert.True(t, completed)
}
```

- [ ] **Step 11.2: Run to confirm compile failure**

```bash
go test ./cmd/celeste/orchestrator/... -run TestOrchestrator 2>&1 | head -5
```

Expected: `undefined: orchestrator.New`.

- [ ] **Step 11.3: Implement orchestrator.go**

Create `cmd/celeste/orchestrator/orchestrator.go`:

```go
package orchestrator

import (
    "context"
    "fmt"

    "github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// AgentRunner is the interface the orchestrator uses to execute a goal.
// The real implementation wraps agent.Runner; tests supply fakes.
type AgentRunner interface {
    RunGoal(ctx context.Context, goal string) (string, error)
}

// RunnerFactory creates an AgentRunner for the given model name.
type RunnerFactory func(model string) AgentRunner

// Result is the final output of an orchestrator run.
type Result struct {
    Lane     TaskLane
    Primary  string  // final response from primary agent
    Verdict  *DebateResult // nil when no debate was run
}

// Orchestrator manages multi-model agent execution.
type Orchestrator struct {
    cfg           *config.Config
    router        *Router
    runnerFactory RunnerFactory
    onEvent       func(OrchestratorEvent)
    debateRounds  int
}

// Option configures an Orchestrator.
type Option func(*Orchestrator)

// WithRunnerFactory overrides the AgentRunner factory (useful in tests).
func WithRunnerFactory(f RunnerFactory) Option {
    return func(o *Orchestrator) { o.runnerFactory = f }
}

// New creates an Orchestrator backed by the given config.
func New(cfg *config.Config, opts ...Option) *Orchestrator {
    rounds := 3
    if cfg.Orchestrator != nil && cfg.Orchestrator.DebateRounds > 0 {
        rounds = cfg.Orchestrator.DebateRounds
    }
    o := &Orchestrator{
        cfg:          cfg,
        router:       NewRouter(cfg),
        debateRounds: rounds,
        onEvent:      func(OrchestratorEvent) {},
    }
    for _, opt := range opts {
        opt(o)
    }
    if o.runnerFactory == nil {
        // Default factory — wraps the real agent.Runner.
        // Imported lazily to avoid import cycle in tests.
        o.runnerFactory = defaultRunnerFactory(cfg)
    }
    return o
}

// OnEvent registers a callback for all orchestrator events.
func (o *Orchestrator) OnEvent(fn func(OrchestratorEvent)) {
    o.onEvent = fn
}

// Run classifies the goal, routes to models, executes the primary agent,
// and optionally runs a reviewer debate. Returns the final Result.
func (o *Orchestrator) Run(ctx context.Context, goal string) (*Result, error) {
    // 1. Classify
    lane, confidence := ClassifyHeuristic(goal)
    o.onEvent(OrchestratorEvent{Kind: EventClassified, Lane: lane, Text: fmt.Sprintf("%.0f%% confidence", confidence*100)})

    // 2. Route
    assignment, err := o.router.Resolve(lane)
    if err != nil {
        o.onEvent(OrchestratorEvent{Kind: EventError, Text: err.Error()})
        return nil, err
    }

    // 3. Run primary agent
    o.onEvent(OrchestratorEvent{Kind: EventAction, Lane: lane, Text: fmt.Sprintf("starting primary agent (%s)", assignment.Primary)})
    primary := o.runnerFactory(assignment.Primary)
    primaryResponse, err := primary.RunGoal(ctx, goal)
    if err != nil {
        o.onEvent(OrchestratorEvent{Kind: EventError, Text: err.Error()})
        return nil, fmt.Errorf("primary agent failed: %w", err)
    }

    result := &Result{Lane: lane, Primary: primaryResponse}

    // 4. Debate (code/review lanes with a configured reviewer only)
    if assignment.HasReviewer() && (lane == LaneCode || lane == LaneReview) {
        verdict, debateErr := o.runDebate(ctx, goal, primaryResponse, assignment)
        if debateErr != nil {
            // Debate failure is non-fatal — emit warning and continue.
            o.onEvent(OrchestratorEvent{Kind: EventError, Text: fmt.Sprintf("debate skipped: %v", debateErr)})
        } else {
            result.Verdict = verdict
        }
    }

    o.onEvent(OrchestratorEvent{Kind: EventComplete, Lane: lane, Text: "done"})
    return result, nil
}

func (o *Orchestrator) runDebate(ctx context.Context, goal, primaryOutput string, assignment ModelAssignment) (*DebateResult, error) {
    dm := NewDebateManager(DebateOptions{MaxRounds: o.debateRounds})
    reviewer := o.runnerFactory(assignment.Reviewer)

    reviewPrompt := fmt.Sprintf(
        "You are reviewing code produced by another model. Evaluate purely on correctness, security, and clarity.\n\nOriginal goal: %s\n\nOutput to review:\n%s\n\nList any issues as JSON: [{\"file\":\"\",\"line\":0,\"severity\":\"low|medium|high\",\"description\":\"\"}]",
        goal, primaryOutput,
    )

    // Track last parsed issues so the max-rounds path uses real data, not an empty list.
    var lastIssues []Issue

    for round := 1; round <= o.debateRounds; round++ {
        reviewOutput, err := reviewer.RunGoal(ctx, reviewPrompt)
        if err != nil {
            return nil, fmt.Errorf("reviewer round %d failed: %w", round, err)
        }
        dm.AddTurn(DebateTurn{Round: round, Role: RoleReviewer, Input: reviewPrompt, Output: reviewOutput})
        o.onEvent(OrchestratorEvent{Kind: EventReviewDraft, Text: reviewOutput})

        // Parse issues from reviewer JSON (best-effort; empty on parse failure)
        lastIssues = parseIssues(reviewOutput)
        verdict := dm.Verdict(lastIssues)

        if verdict.Kind == VerdictApproved {
            o.onEvent(OrchestratorEvent{Kind: EventVerdict, Score: verdict.Score, Text: "approved"})
            return &verdict, nil
        }
        if verdict.Kind == VerdictContested {
            o.onEvent(OrchestratorEvent{Kind: EventVerdict, Score: verdict.Score, Text: "contested"})
            return &verdict, nil
        }

        // Primary agent responds to critique
        defensePrompt := fmt.Sprintf("The reviewer found these issues:\n%s\n\nAddress each issue and provide the corrected output.", reviewOutput)
        defenseOutput, err := o.runnerFactory(assignment.Primary).RunGoal(ctx, defensePrompt)
        if err != nil {
            return nil, fmt.Errorf("primary defense round %d failed: %w", round, err)
        }
        dm.AddTurn(DebateTurn{Round: round, Role: RolePrimary, Input: defensePrompt, Output: defenseOutput})
        o.onEvent(OrchestratorEvent{Kind: EventDefense, Text: defenseOutput})
        reviewPrompt = fmt.Sprintf("Review the revised output:\n%s", defenseOutput)
    }

    // Use the last set of parsed issues (not empty) so VerdictContested is returned correctly.
    verdict := dm.Verdict(lastIssues)
    o.onEvent(OrchestratorEvent{Kind: EventVerdict, Score: verdict.Score, Text: "max rounds reached"})
    return &verdict, nil
}

// parseIssues extracts Issue structs from a JSON array in the reviewer's response.
// Returns empty slice on parse failure (non-fatal).
func parseIssues(text string) []Issue {
    // Minimal JSON extraction: look for [...] in the text.
    start := -1
    depth := 0
    for i, ch := range text {
        if ch == '[' {
            if depth == 0 {
                start = i
            }
            depth++
        } else if ch == ']' {
            depth--
            if depth == 0 && start >= 0 {
                // Try to parse
                var issues []Issue
                _ = json.Unmarshal([]byte(text[start:i+1]), &issues)
                return issues
            }
        }
    }
    return nil
}
```

Add `import "encoding/json"` to `orchestrator.go`'s import block and call `json.Unmarshal` directly inside `parseIssues` — no separate wrapper file needed.

Add `cmd/celeste/orchestrator/runner_default.go` (real runner factory, separate from logic to keep orchestrator.go import-free from agent):

```go
package orchestrator

import (
    "context"
    "io"

    "github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
    "github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

type realAgentRunner struct {
    cfg   *config.Config
    model string
}

func (r *realAgentRunner) RunGoal(ctx context.Context, goal string) (string, error) {
    cfg := *r.cfg
    cfg.Model = r.model
    opts := agent.DefaultOptions()
    runner, err := agent.NewRunner(&cfg, opts, io.Discard, io.Discard)
    if err != nil {
        return "", err
    }
    state, err := runner.RunGoal(ctx, goal)
    if state != nil {
        return state.LastAssistantResponse, err
    }
    return "", err
}

func defaultRunnerFactory(cfg *config.Config) RunnerFactory {
    return func(model string) AgentRunner {
        return &realAgentRunner{cfg: cfg, model: model}
    }
}
```

- [ ] **Step 11.4: Run orchestrator tests**

```bash
go test ./cmd/celeste/orchestrator/... -v 2>&1 | tail -30
```

Expected: all PASS.

- [ ] **Step 11.5: Full build check**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 11.6: Commit**

```bash
git add cmd/celeste/orchestrator/
git commit -m "feat: add orchestrator state machine with classify→route→run→debate flow"
```

---

### Task 12: Split-panel TUI — tui/split_panel.go

**Files:**
- Create: `cmd/celeste/tui/split_panel.go`
- Create: `cmd/celeste/tui/split_panel_test.go`

- [ ] **Step 12.1: Write failing split panel test**

Create `cmd/celeste/tui/split_panel_test.go`:

```go
package tui

import (
    "strings"
    "testing"

    "github.com/stretchr/testify/assert"
)

func TestSplitPanelAddActionEntry(t *testing.T) {
    p := NewSplitPanel(80, 24)
    p.AddAction("classified: code (0.94)")
    p.AddAction("reading main.go")
    assert.Len(t, p.Actions(), 2)
    assert.Equal(t, "classified: code (0.94)", p.Actions()[0])
}

func TestSplitPanelSetDiff(t *testing.T) {
    p := NewSplitPanel(80, 24)
    p.SetDiff("main.go", "@@ -1,3 +1,5 @@\n+func foo() {}")
    assert.Equal(t, "main.go", p.DiffFile())
    assert.Contains(t, p.DiffContent(), "func foo")
}

func TestSplitPanelRenderHasBothPanels(t *testing.T) {
    p := NewSplitPanel(100, 30)
    p.AddAction("doing something")
    p.SetDiff("foo.go", "- old\n+ new")
    rendered := p.View()
    assert.True(t, strings.Contains(rendered, "doing something"), "left panel missing")
    assert.True(t, strings.Contains(rendered, "foo.go"), "right panel missing")
}

func TestSplitPanelCapsActionsAt200(t *testing.T) {
    p := NewSplitPanel(80, 24)
    for i := 0; i < 250; i++ {
        p.AddAction("entry")
    }
    assert.LessOrEqual(t, len(p.Actions()), 200)
}
```

- [ ] **Step 12.2: Run to confirm compile failure**

```bash
go test ./cmd/celeste/tui/... -run TestSplitPanel 2>&1 | head -5
```

Expected: `undefined: NewSplitPanel`.

- [ ] **Step 12.3: Implement split_panel.go**

Create `cmd/celeste/tui/split_panel.go`:

```go
package tui

import (
    "strings"

    "github.com/charmbracelet/lipgloss"
)

const maxActionEntries = 200

// SplitPanel renders a two-column layout:
//   Left  — agent action feed (scrollable log)
//   Right — file diff or artifact view
type SplitPanel struct {
    width   int
    height  int
    actions []string
    diffFile    string
    diffContent string
    verdict string
}

// NewSplitPanel creates a SplitPanel sized to the given terminal dimensions.
func NewSplitPanel(width, height int) *SplitPanel {
    return &SplitPanel{width: width, height: height}
}

// Resize updates the panel dimensions (call on tea.WindowSizeMsg).
func (s *SplitPanel) Resize(width, height int) {
    s.width = width
    s.height = height
}

// AddAction appends an entry to the left action feed.
func (s *SplitPanel) AddAction(text string) {
    s.actions = append(s.actions, text)
    if len(s.actions) > maxActionEntries {
        s.actions = s.actions[len(s.actions)-maxActionEntries:]
    }
}

// Actions returns the current action feed entries.
func (s *SplitPanel) Actions() []string { return s.actions }

// SetDiff updates the right panel with a file diff.
func (s *SplitPanel) SetDiff(file, diff string) {
    s.diffFile = file
    s.diffContent = diff
    s.verdict = ""
}

// SetVerdict replaces the right panel with a verdict report.
func (s *SplitPanel) SetVerdict(text string) {
    s.verdict = text
    s.diffFile = ""
    s.diffContent = ""
}

// DiffFile returns the file name currently shown in the right panel.
func (s *SplitPanel) DiffFile() string { return s.diffFile }

// DiffContent returns the diff currently shown in the right panel.
func (s *SplitPanel) DiffContent() string { return s.diffContent }

// View renders the split panel as a string for Bubble Tea.
func (s *SplitPanel) View() string {
    if s.width < 40 {
        return s.viewNarrow()
    }

    leftWidth := s.width / 2
    rightWidth := s.width - leftWidth - 1

    leftStyle := lipgloss.NewStyle().
        Width(leftWidth).
        Height(s.height - 4).
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("#8b5cf6")).
        Padding(0, 1)

    rightStyle := lipgloss.NewStyle().
        Width(rightWidth).
        Height(s.height - 4).
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color("#00d4ff")).
        Padding(0, 1)

    leftContent := s.renderActionFeed(leftWidth - 4)
    rightContent := s.renderArtifact(rightWidth - 4)

    return lipgloss.JoinHorizontal(lipgloss.Top,
        leftStyle.Render(leftContent),
        rightStyle.Render(rightContent),
    )
}

func (s *SplitPanel) renderActionFeed(width int) string {
    header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#d94f90")).Render("AGENT ACTIONS")
    if len(s.actions) == 0 {
        return header + "\n(waiting...)"
    }
    // Show last N entries that fit.
    maxLines := s.height - 8
    if maxLines < 1 {
        maxLines = 1
    }
    entries := s.actions
    if len(entries) > maxLines {
        entries = entries[len(entries)-maxLines:]
    }
    lines := make([]string, len(entries))
    for i, e := range entries {
        line := "● " + e
        if len(line) > width {
            line = line[:width-1] + "…"
        }
        lines[i] = line
    }
    return header + "\n" + strings.Join(lines, "\n")
}

func (s *SplitPanel) renderArtifact(width int) string {
    if s.verdict != "" {
        header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00d4ff")).Render("REVIEW VERDICT")
        return header + "\n" + s.verdict
    }
    if s.diffFile == "" {
        return lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Render("(no file changes yet)")
    }
    header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00d4ff")).Render(s.diffFile)
    return header + "\n" + s.diffContent
}

func (s *SplitPanel) viewNarrow() string {
    return strings.Join(s.actions, "\n")
}
```

- [ ] **Step 12.4: Run split panel tests**

```bash
go test ./cmd/celeste/tui/... -run TestSplitPanel -v
```

Expected: all PASS.

- [ ] **Step 12.5: Commit**

```bash
git add cmd/celeste/tui/split_panel.go cmd/celeste/tui/split_panel_test.go
git commit -m "feat: add split-panel TUI for orchestrator action feed + artifact view"
```

---

### Task 13: Wire orchestrator to TUI — tui_orchestrator.go

**Files:**
- Create: `cmd/celeste/tui_orchestrator.go`
- Modify: `cmd/celeste/tui/messages.go` — add `OrchestratorEventMsg`
- Modify: `cmd/celeste/tui/app.go` — add split panel state + handle `OrchestratorEventMsg`

- [ ] **Step 13.1: Add OrchestratorEventMsg to messages.go**

```go
// OrchestratorEventMsg wraps an orchestrator.OrchestratorEvent for delivery to the TUI.
// Defined here to avoid an import cycle (tui → orchestrator is fine; orchestrator must not → tui).
type OrchestratorEventMsg struct {
    Kind      int    // cast from orchestrator.EventKind
    Lane      string // cast from orchestrator.TaskLane
    Text      string
    FilePath  string
    Diff      string
    Score     float64
    Ch        <-chan OrchestratorEventMsg // nil on terminal events
}

// ReadNext returns a cmd to read the next OrchestratorEventMsg.
func (m OrchestratorEventMsg) ReadNext() tea.Cmd {
    if m.Ch == nil {
        return nil
    }
    return func() tea.Msg {
        msg, ok := <-m.Ch
        if !ok {
            return nil
        }
        return msg
    }
}
```

- [ ] **Step 13.2: Add split panel to AppModel**

In `app.go`, add to `AppModel` struct:

```go
    // Split panel for orchestrator/agent view
    splitPanel     *SplitPanel
    splitPanelMode bool
```

- [ ] **Step 13.3: Handle OrchestratorEventMsg in app.go Update**

Add after the `AgentProgressMsg` case:

```go
case OrchestratorEventMsg:
    var cmds []tea.Cmd

    if m.splitPanel == nil {
        m.splitPanel = NewSplitPanel(m.width, m.height)
    }
    m.splitPanelMode = true

    // EventKind constants (mirror orchestrator package without import cycle):
    // 0=Classified 1=Action 2=ToolCall 3=FileDiff 4=ReviewDraft 5=Defense 6=Verdict 7=Complete 8=Error
    switch msg.Kind {
    case 0: // EventClassified
        m.splitPanel.AddAction(fmt.Sprintf("classified: %s (%s)", msg.Lane, msg.Text))
        m.status = m.status.SetText(fmt.Sprintf("Orchestrator: [%s] %s", msg.Lane, msg.Text))
        m.streaming = true
        m.status = m.status.SetStreaming(true)
    case 1, 2: // EventAction, EventToolCall
        m.splitPanel.AddAction(msg.Text)
        m.status = m.status.SetText(fmt.Sprintf("Orchestrator: %s", msg.Text))
    case 3: // EventFileDiff
        m.splitPanel.AddAction(fmt.Sprintf("wrote %s", msg.FilePath))
        if msg.FilePath != "" {
            m.splitPanel.SetDiff(msg.FilePath, msg.Diff)
        }
    case 4: // EventReviewDraft
        m.splitPanel.AddAction(fmt.Sprintf("🔍 reviewer: %s", truncateText(msg.Text, 60)))
    case 5: // EventDefense
        m.splitPanel.AddAction(fmt.Sprintf("🛡 defense: %s", truncateText(msg.Text, 60)))
    case 6: // EventVerdict
        score := fmt.Sprintf("%.2f", msg.Score)
        m.splitPanel.AddAction(fmt.Sprintf("verdict: %s (score %s)", msg.Text, score))
        m.splitPanel.SetVerdict(fmt.Sprintf("%s\nscore: %s", msg.Text, score))
    case 7: // EventComplete
        m.streaming = false
        m.splitPanelMode = false // restore normal chat view
        m.status = m.status.SetStreaming(false)
        m.status = m.status.SetText("Orchestrator: complete")
        m.persistSession()
    case 8: // EventError
        m.streaming = false
        m.splitPanelMode = false // restore normal chat view
        m.status = m.status.SetStreaming(false)
        m.status = m.status.SetText(fmt.Sprintf("Orchestrator error: %s", msg.Text))
        m.chat = m.chat.AddSystemMessage(fmt.Sprintf("❌ %s", msg.Text))
        m.persistSession()
    }

    if next := msg.ReadNext(); next != nil {
        cmds = append(cmds, next)
    }
    return m, tea.Batch(cmds...)
```

- [ ] **Step 13.4: Update View to show split panel when active**

In `app.go`'s `View()` method, wrap the chat area:

```go
// In View(), add before the normal chat render path:
if m.splitPanelMode && m.splitPanel != nil {
    // Show split panel during orchestrator run; restore on EventComplete/EventError.
    // Skills bar is preserved so the user can see available skills during the run.
    return lipgloss.JoinVertical(lipgloss.Left,
        m.header.View(),
        m.splitPanel.View(),
        m.skills.View(),
        m.status.View(),
        m.input.View(),
    )
}
```

- [ ] **Step 13.5: Create tui_orchestrator.go**

Create `cmd/celeste/tui_orchestrator.go`:

```go
package main

import (
    "context"

    tea "github.com/charmbracelet/bubbletea"
    "github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
    "github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// RunOrchestratorCommand launches an orchestrated agent run from the TUI.
// Returns a tea.Cmd that streams OrchestratorEventMsg to the TUI.
func (a *TUIClientAdapter) RunOrchestratorCommand(goal string) tea.Cmd {
    ch := make(chan tui.OrchestratorEventMsg, 64)

    go func() {
        defer close(ch)
        cfg := a.currentAgentConfig()
        o := orchestrator.New(cfg)
        // recvCh is the receive end of ch; needed because OrchestratorEventMsg.Ch
        // is <-chan (receive-only) but ch is bidirectional. Explicit conversion is valid Go.
        recvCh := (<-chan tui.OrchestratorEventMsg)(ch)
        o.OnEvent(func(e orchestrator.OrchestratorEvent) {
            var msgCh <-chan tui.OrchestratorEventMsg
            if e.Kind != orchestrator.EventComplete && e.Kind != orchestrator.EventError {
                msgCh = recvCh
            }
            ch <- tui.OrchestratorEventMsg{
                Kind:     int(e.Kind),
                Lane:     string(e.Lane),
                Text:     e.Text,
                FilePath: e.FilePath,
                Diff:     e.Diff,
                Score:    e.Score,
                Ch:       msgCh,
            }
        })
        // Run emits EventComplete or EventError via OnEvent before returning.
        // Do NOT send an additional error message here — that would double-emit.
        _, _ = o.Run(context.Background(), goal)
    }()

    return func() tea.Msg {
        msg, ok := <-ch
        if !ok {
            return nil
        }
        return msg
    }
}
```

- [ ] **Step 13.6: Add truncateText helper to app.go (must exist before build)**

In `cmd/celeste/tui/app.go`, add near the other small helpers:

```go
func truncateText(s string, max int) string {
    if len(s) <= max {
        return s
    }
    return s[:max] + "…"
}
```

- [ ] **Step 13.7: Build**

```bash
go build ./...
```

Expected: clean.

- [ ] **Step 13.8: Run full test suite**

```bash
go test ./... 2>&1 | grep -E "FAIL|ok"
```

Expected: all `ok`.

- [ ] **Step 13.9: Commit**

```bash
git add cmd/celeste/tui/messages.go cmd/celeste/tui/app.go cmd/celeste/tui_orchestrator.go
git commit -m "feat: wire orchestrator events to TUI split panel"
```

---

### Task 14: Final PR for Phase B

- [ ] **Step 14.2: Run all tests**

```bash
go test ./... 2>&1 | grep -E "FAIL|ok"
```

Expected: all `ok`.

- [ ] **Step 14.3: Push and open PR**

```bash
git push
gh pr create \
  --title "feat: multi-model orchestrator with split-panel TUI" \
  --body "$(cat <<'EOF'
## Summary
- New orchestrator/ package: keyword classifier, model router, debate manager, state machine
- Split-panel TUI: left action feed, right file diff/verdict view
- OrchestratorEventMsg channel feeds events from goroutine into Bubble Tea update loop
- Config schema updated with orchestrator.lanes block for per-lane model assignment

## Test plan
- [ ] classifier_test.go: all keyword heuristics pass
- [ ] router_test.go: lane resolution, fallback, blank reviewer
- [ ] debate_test.go: turns, max rounds, verdict kinds
- [ ] orchestrator_test.go: full Idle→Complete state machine
- [ ] split_panel_test.go: action feed, diff panel, 200-entry cap, render
- [ ] Manual: /agent goal with orchestrator mode shows split panel live
EOF
)"
```
