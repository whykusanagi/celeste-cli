# Autonomous Agent Mode Buildout (Celeste)

## Objective
Shift Celeste from primarily one-shot chat/tool calls into a reusable autonomous execution CLI for coding, content creation, and development workflows, while preserving OpenAI/xAI/Vertex compatibility paths.

## Scope Delivered In This Iteration
1. New `celeste agent` command surface for autonomous runs.
2. Autonomous multi-turn loop with explicit completion controls.
3. Checkpoint persistence and resume/list workflows.
4. Agent-oriented development tools (workspace file/search/command skills).
5. Eval harness for repeatable scenario testing.

## Why This Matters
These additions establish the minimum viable agent substrate:
- Goal -> iterate -> tool use -> continue -> completion signal.
- Long-horizon continuation through checkpoint state.
- Workspace-aware tooling suitable for software/content tasks.
- Regression-friendly eval loop to measure behavior over time.

## Implemented Architecture

### 1) Command & UX Layer
- Added `agent` command wiring through command dispatcher.
- Added usage surfaces for:
  - `--goal`, `--goal-file`
  - `--resume`, `--list-runs`
  - `--eval`
  - control knobs: max turns/tool calls/no-tool turns, timeouts, completion marker.

### 2) Runtime Orchestration
New package: `cmd/celeste/agent`
- `Runner` loop sends full conversation + tools each turn.
- Executes all returned tool calls sequentially.
- Appends one tool result per `tool_call_id`.
- Continues until:
  - explicit completion marker (default `TASK_COMPLETE:`), or
  - configured no-progress stop, or
  - max-turn safety stop.

### 3) Long-Horizon Memory / Checkpointing
- Persistent checkpoints in `~/.celeste/agent/runs/*.json`.
- Stores:
  - run id, goal, status, turn counters
  - full message history
  - per-step trace records
  - completion/error metadata
- Supports run listing and resume.

### 4) Richer Tool Environment (Agent-specific)
Added agent-only dev skills:
- `dev_list_files`
- `dev_read_file`
- `dev_write_file`
- `dev_search_files`
- `dev_run_command`

Constraints:
- File tools enforce workspace path boundaries.
- Search excludes `.git` directory.
- output/size caps prevent runaway payloads.

### 5) Eval Harness
- Eval input supports either:
  - `{ "cases": [...] }`
  - or direct array `[...]`
- Per-case status and pass/fail scoring based on:
  - completion status
  - required / forbidden output text assertions

## Validation Added
Unit tests in `cmd/celeste/agent`:
1. Checkpoint save/load/list.
2. Workspace traversal guard.
3. Dev skills read/write/search/command flow.
4. Eval file parsing and case scoring.

Also updated dispatcher tests for command routing to `agent`.

## Compatibility Notes
- Existing chat/TUI flows are unchanged.
- `LLMBackend` interface unchanged.
- xAI collections and Vertex auth paths untouched.
- Agent mode is additive and opt-in through `celeste agent`.

## Remaining Gaps to Reach OpenClaw/PicoClaw Parity
1. Planner depth:
   - explicit plan state machine (plan -> execute -> verify -> revise).
2. Structured artifact management:
   - explicit task files, checkpoints by milestone, auto-diff summaries.
3. Multi-run memory:
   - retrieval over prior runs (semantic store or indexed snapshots).
4. Stronger evals:
   - deterministic fixture repos + golden outputs + grading rubric.
5. Safety controls:
   - command allowlists/denylists, approval gates for high-risk operations.

## Next Increment (Proposed)
1. Add planner state with explicit `PlanStep[]` and completion criteria per step.
2. Add `agent verify` stage using tests/lint/build checks before completion.
3. Add run artifact bundle export (`plan`, `actions`, `diff`, `validation`).
4. Add benchmark suite for coding/content tasks with CI gating.

