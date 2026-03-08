# Celeste Agent Productization Plan (Phases 5-10)

Date: 2026-03-06  
Owner: `codex/celeste-phase5-product-core`

## Goal
Evolve Celeste from one-shot chat + tools into a production-ready autonomous assistant for development and content workflows, while preserving OpenAI/xAI/Vertex compatibility.

## Phase 5: Runtime Product Core (Lifecycle + Live Events)
### Scope
- First-class run lifecycle controls in TUI/CLI surfaces (`start`, `list`, `show`, `resume`, `stop`).
- Live runtime events (planning/turns/tools/verification/status transitions).
- Single-active-run safety in TUI process.

### Deliverables
- `agent.RunEvent` model and event sink hook in runtime.
- TUI streaming event panel/messages via `/agent`.
- `/agent show <run-id>` and `/agent stop [run-id]` support.

### Validation
- Unit: event emission metadata and event conversion.
- Unit: lifecycle command parsing/behavior (list/show/stop/goal/resume).
- TUI: event polling, terminal event handling, and status updates.

### Acceptance
- User can observe progress during run, not just final summary.
- User can inspect prior runs and stop active runs safely.

## Phase 6: Safety + Execution Controls
### Scope
- Workspace jail and path validation for dev skills.
- Command policy guardrails and explicit risky-action approvals.
- Tool-level limits (timeout, output cap, retry budget).

### Validation
- Path traversal rejection tests.
- Policy enforcement tests for disallowed commands.
- Timeout/output truncation behavior tests.

### Acceptance
- Unsafe actions are blocked or require explicit approval.

## Phase 7: Planner/Executor/Verifier v2
### Scope
- Step dependencies and retry semantics.
- No-progress recovery policy and blocker reporting.
- Strong completion gating tied to verification outcomes.

### Validation
- Scenario tests for loop stalls, flaky verification, partial completion.
- Regression metrics against phase-3 benchmark scaffold.

### Acceptance
- Reduced false-complete and no-progress failure rate.

## Phase 8: Memory + Checkpoint Continuity
### Scope
- Project memory store for decisions, constraints, and previous fixes.
- Retrieval-integrated prompts for resumed/new runs.
- Memory-aware checkpoint resume.

### Validation
- Persistence/retrieval unit tests.
- Resume continuity integration tests.

### Acceptance
- Resumed runs preserve context and avoid repeated mistakes.

## Phase 9: Provider Compatibility + Eval Gate
### Scope
- Provider matrix eval harness (OpenAI/xAI/Vertex) in CI.
- Tool-calling and schema behavior parity checks.
- Regression thresholds for merge/release gating.

### Validation
- CI matrix runs pass on designated model set.
- Golden fixtures for multi-tool turns and serialization errors.

### Acceptance
- Provider regressions block release automatically.

## Phase 10: Productization + Release
### Scope
- Onboarding UX for agent mode profiles.
- TUI run dashboard polish and operational diagnostics.
- Release automation (packaging, notes, rollback playbook).

### Validation
- Smoke tests on release binaries.
- Upgrade/migration tests for config/session continuity.

### Acceptance
- Repeatable release process with documented rollback.

## Execution Order
1. Phase 5
2. Phase 6
3. Phase 7
4. Phase 8
5. Phase 9
6. Phase 10

## Current Status
- Phase 5: IN PROGRESS on `codex/celeste-phase5-product-core`.
- Phase 6: IN PROGRESS (command policy + workspace symlink guard baseline).
- Phase 7: IN PROGRESS (blocker-aware stop + verification retry ceiling baseline).
- Phase 8: IN PROGRESS (project memory store + recall/writeback baseline).
- Phase 9: IN PROGRESS (provider matrix gate + CI/release enforcement baseline).
- Phase 10: Planned.
