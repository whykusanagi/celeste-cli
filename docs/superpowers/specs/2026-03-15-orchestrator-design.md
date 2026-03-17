# Celeste CLI — Orchestrator Design Spec

**Date:** 2026-03-15
**Version:** 1.1
**Status:** Approved

---

## Overview

Add a multi-model orchestrator layer on top of the existing `agent.Runner`. The orchestrator classifies goals by task lane, routes them to the best-fit model per lane, runs a primary agent, and optionally initiates a review debate between two models (primary worker + independent reviewer). A new split-panel TUI surfaces agent actions on the left and file diffs / review verdicts on the right.

---

## Section 1: Architecture Overview

### New/Changed Files

```
cmd/celeste/
├── orchestrator/           ← NEW
│   ├── orchestrator.go     — top-level state machine, owns agent lifecycle
│   ├── classifier.go       — goal → task lane detection (LLM-assisted + heuristics)
│   ├── router.go           — lane + config → model assignment
│   ├── debate.go           — review debate turn manager
│   └── events.go           — typed event stream (OrchestratorEvent)
├── agent/                  ← mostly unchanged
│   └── runtime.go          — Runner now accepts an EventSink interface
├── tui/
│   ├── split_panel.go      ← NEW — left action feed, right diff/artifact view
│   └── app.go              ← updated — talks to orchestrator, not agent directly
└── tui_agent.go            ← replaced by tui_orchestrator.go
```

### Data Flow

```
User goal
    ↓
Orchestrator.Run(goal)
    ↓
Classifier → TaskLane (code/content/media/review/research)
    ↓
Router → ModelAssignment{primary, reviewer}
    ↓
PrimaryAgent.Run() ──→ OrchestratorEvents ──→ TUI left panel (action feed)
                   └──→ file diffs          ──→ TUI right panel
    ↓ (if lane == code or explicit review)
Debate.Run(primaryOutput) → turns between reviewer + primary → verdict
    ↓
OrchestratorResult → TUI renders final verdict block
```

The `OrchestratorEvent` stream is the single wire between the orchestrator and the TUI — no direct agent access from the UI layer.

---

## Section 2: Classifier & Router

### Task Classifier

Two-stage detection — fast heuristics first, LLM fallback only when ambiguous.

**Stage 1 — Heuristics** (zero latency, no API call):
```
keywords → lane
─────────────────────────────────────────────
fix, refactor, debug, test, build, compile    → code
write, draft, blog, docs, summarize, explain  → content
upscale, image, video, render, convert        → media
review, audit, check, critique, blind         → review
research, find, search, compare, what is      → research
```

**Stage 2 — LLM classifier** (only if heuristic confidence < threshold):
- Single-turn call to the cheapest configured model
- Returns `{lane, confidence, reasoning}`
- Result cached by goal hash to avoid re-classification on resume

### Router

Config-driven, no hardcoded models. Each lane reads from the user's config:

```json
// ~/.celeste/config.json (new "orchestrator" block)
{
  "orchestrator": {
    "lanes": {
      "code":     { "primary": "grok-4-1-fast",   "reviewer": "gemini-2.0-flash" },
      "content":  { "primary": "gpt-4o-mini",      "reviewer": "" },
      "media":    { "primary": "gpt-4o-mini",      "reviewer": "" },
      "review":   { "primary": "gemini-2.0-flash", "reviewer": "grok-4-1-fast" },
      "research": { "primary": "gpt-4o-mini",      "reviewer": "" }
    },
    "default_lane": "code",
    "debate_rounds": 3
  }
}
```

Reviewer blank = debate skipped for that lane. Router falls back to the default active config profile if a lane's primary is unconfigured.

---

## Section 3: Orchestrator State Machine & Debate Flow

### State Machine

```
Idle
  ↓ Run(goal)
Classifying  → emits: EventClassified{lane, model, confidence}
  ↓
PrimaryRunning → emits: EventAction{}, EventFileDiff{}, EventToolCall{}
  ↓ (primary completes)
  ├─ lane == media/content/research → Done (no debate)
  └─ lane == code/review → DebateStarting
       ↓
    ReviewerAnalyzing → emits: EventReviewDraft{critique}
       ↓
    PrimaryDefending  → emits: EventDefense{response}
       ↓
    ReviewerVerdicting → emits: EventVerdict{issues[], accepted[], final_score}
       ↓
    Done → emits: EventComplete{result, verdict}
```

Debate capped at **3 rounds** by default (configurable). If no consensus after max rounds, verdict is marked `contested` and both positions are shown.

### Debate Turn Manager (`debate.go`)

```go
type DebateTurn struct {
    Round     int
    Role      DebateRole   // Reviewer | Primary
    Config    *config.Config
    Input     string       // previous turn's output
    Output    string       // this turn's response
}

type DebateResult struct {
    Turns    []DebateTurn
    Verdict  Verdict       // Approved | NeedsWork | Contested
    Issues   []Issue       // {file, line, severity, description}
    Score    float64       // 0.0–1.0
}
```

Reviewer prompt is injected with explicit blindness instruction:
> *"You are reviewing code produced by another model. You do not know which model wrote it. Evaluate purely on correctness, security, and clarity."*

---

## Section 4: TUI Split Panel

### Layout

```
╔══════════════════════════════════════════════════════════════════════════╗
║ ✨ Celeste CLI ─────── [code] grok-4-1-fast → gemini-review ✓ ──── 4.1K ║
╠═══════════════════════════════╦════════════════════════════════════════════╣
║  AGENT ACTIONS                ║  main.go                                  ║
║ ─────────────────────────────  ║ ─────────────────────────────────────────  ║
║ ● classified: code (0.94)     ║  @@ -142,7 +142,12 @@                     ║
║ ● reading main.go             ║  - func NewRunner(cfg *config.Config) {    ║
║ ● reading runtime.go          ║  + func NewRunner(                         ║
║ ● writing fix to runtime.go   ║  +   cfg *config.Config,                   ║
║                               ║  +   opts Options,                         ║
║  REVIEW DEBATE  ─────────────  ║  +   out io.Writer,                       ║
║ 🔍 gemini: 3 issues found     ║  + ) {                                     ║
║ 🛡  grok: accepted 2, pushed 1 ║                                            ║
║ ✅ verdict: approved (0.87)   ║  [tab to cycle files]                      ║
╠═══════════════════════════════╩════════════════════════════════════════════╣
║  ❯ Type a message or /agent <goal>...                                      ║
╚══════════════════════════════════════════════════════════════════════════╝
```

### Panel Behavior

**Left panel — Action Feed:**
- Streams `EventAction`, `EventToolCall`, `EventReviewDraft`, `EventDefense`, `EventVerdict` as they arrive
- Scrollable, capped at last 200 entries
- Review debate section visually separated with a divider
- Color coding: `●` blue = agent action, `🔍` yellow = reviewer, `🛡` cyan = defense, `✅`/`❌` = verdict

**Right panel — Artifact View:**
- Default: unified diff of most recently written file
- Tab cycles through all modified files in current run
- Switches to review report (structured issues list) when verdict arrives
- Falls back to plain text summary when no file diffs exist (e.g., research lane)

**Status bar:**
- Shows: `[lane] primary-model → reviewer-model status  token-count`
- Updates in real time as phase transitions occur
- Displays `[review] no reviewer configured` if debate is skipped

---

## Section 5: Error Handling & Testing

### Error Handling

| Phase | Failure | Recovery |
|---|---|---|
| Classification | LLM call fails | Fall back to heuristics; if still ambiguous, prompt user to pick lane |
| Primary agent | Max turns / blocked | Emit `EventError`, surface in left panel, offer `/agent resume <id>` |
| Reviewer init | Reviewer model unconfigured | Skip debate, warn in status bar |
| Debate round | Reviewer API error | Mark round as skipped, continue to verdict with partial data |
| Verdict | No consensus after max rounds | Mark `Contested`, show both positions, let user decide |

Errors never crash the orchestrator — they emit `EventError` and transition to a degraded-but-functional state. A full failure checkpoints what it has and surfaces a resume command.

### Testing Strategy

**Unit tests:**
- `classifier_test.go` — keyword heuristics coverage, confidence thresholds
- `router_test.go` — lane → config resolution, missing lane fallback, override precedence
- `debate_test.go` — turn sequencing, max rounds cap, contested verdict path
- `events_test.go` — event stream ordering and field correctness

**Integration tests (extend existing `test/` fixture pattern):**
- `orchestrator_integration_test.go` — mock LLM backends, full Idle→Done state machine walk
- Debate flow: mock reviewer returns issues → mock primary accepts/rejects → assert verdict shape
- Split panel: inject event stream → assert left/right panel render output

**What we explicitly don't test:**
- Actual LLM output quality (eval harness territory, already in `agent/eval.go`)
- Real API calls in CI (all mocked at the backend interface boundary)

---

## Section 6: Agent UX — Live Progress & Typing Animation

### Problem

The current `/agent` TUI flow is fully synchronous: `RunAgentCommand` blocks until the entire agent run completes (discarding all output with `io.Discard`), then delivers one `AgentCommandResultMsg` that dumps the final summary as a static system message. The TUI shows a frozen spinner for the full duration, then a wall of text.

### Required Behavior

The agent must feel like a live tool:
- The TUI updates in real-time as each turn, tool call, step completion, and final response arrives
- Each incoming text chunk is fed through `SimulatedTyping` (already implemented in `tui/streaming.go`) so it typewriter-renders into the chat, identical to how streamed LLM responses behave
- The status bar shows what the agent is currently doing (e.g., "Agent: planning…", "Agent: calling dev_write_file", "Agent: turn 3/12")

### Design

**New TUI message types** (`tui/messages.go`):
```go
// AgentProgressMsg is sent incrementally during an agent run.
type AgentProgressMsg struct {
    RunID   string
    Kind    AgentProgressKind  // KindTurnStart | KindToolCall | KindStepDone | KindResponse | KindComplete | KindError
    Text    string             // Human-readable status line or response chunk
    Turn    int
    MaxTurns int
}
type AgentProgressKind int
const (
    KindTurnStart  AgentProgressKind = iota
    KindToolCall
    KindStepDone
    KindResponse    // final assistant response text — feed into SimulatedTyping
    KindComplete
    KindError
)
```

**Runner progress callback** (`agent/runtime.go`):
```go
// Options gains an optional progress sink.
type Options struct {
    // ... existing fields ...
    OnProgress func(kind ProgressKind, text string, turn int)
}
```
The runner calls `OnProgress` at each significant event instead of writing to `out io.Writer`. `tui_agent.go` supplies this callback, which emits `AgentProgressMsg` back to the Bubble Tea program via `program.Send(msg)`.

**TUI handling** (`tui/app.go`):
```go
case AgentProgressMsg:
    switch msg.Kind {
    case KindTurnStart:
        m.status = m.status.SetText(fmt.Sprintf("Agent: turn %d/%d", msg.Turn, msg.MaxTurns))
        m.status = m.status.SetStreaming(true)
    case KindToolCall:
        m.status = m.status.SetText(fmt.Sprintf("Agent: %s", msg.Text))
        m.chat = m.chat.AddSystemMessage("⚙ " + msg.Text)
    case KindStepDone:
        m.chat = m.chat.AddSystemMessage("✓ " + msg.Text)
    case KindResponse:
        // Feed into SimulatedTyping — renders identically to streamed LLM output
        m.typingContent = msg.Text
        m.typingPos = 0
        m.chat = m.chat.StartAssistantMessage()
        return m, TypingTickCmd()
    case KindComplete, KindError:
        m.streaming = false
        m.status = m.status.SetStreaming(false)
    }
```

**Oneshot / non-TUI mode**: The existing `out io.Writer` path in `agent.Runner` is preserved for `celeste agent --goal` CLI mode (plain text to stdout). The `OnProgress` callback is only wired when launching from the TUI.

### What this replaces

`tui_agent.go:RunAgentCommand` currently uses `io.Discard` for both stdout and stderr. After this change it instead:
1. Calls `tea.Program.Send` for progress events (non-blocking via goroutine)
2. Returns immediately from `RunAgentCommand` (the run happens in background)
3. Sends `AgentProgressMsg{Kind: KindComplete}` or `KindError` when the run finishes

---

## Summary: What Gets Built

**Phase A — Agent UX (prerequisite, smaller scope):**
1. **`tui/messages.go`** — add `AgentProgressMsg` + `AgentProgressKind`
2. **`agent/runtime.go`** — add `OnProgress` callback to `Options`; emit progress at turn start, tool calls, step done, final response
3. **`tui_agent.go`** — wire `OnProgress` to send `AgentProgressMsg` via `tea.Program.Send`; run agent in goroutine; return immediately from `RunAgentCommand`
4. **`tui/app.go`** — handle `AgentProgressMsg`: status bar updates, tool call annotations, and `KindResponse` fed into existing `SimulatedTyping` path

**Phase B — Orchestrator:**
5. **`orchestrator/`** — classifier, router, state machine, debate manager, event stream (5 files)
6. **`agent/runtime.go`** — add `EventSink` interface so runner also pushes typed orchestrator events
7. **`tui/split_panel.go`** — new split view, backward-compatible with existing chat mode
8. **`tui_orchestrator.go`** — replaces `tui_agent.go`, wires TUI to orchestrator
9. **Config schema** — new `orchestrator.lanes` block, `celeste config --set-lane` commands
10. **Tests** — unit + integration coverage for all new packages

**What stays unchanged:** `agent.Runner` core loop, all LLM backends, existing chat/TUI mode, checkpoint format, eval harness.
