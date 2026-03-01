# Celeste CLI Feature Buildout Plan (Claw-Style Expansion + Stability)

## Summary
This plan defines how to build the next CLI improvements and a "Celeste-flavored claw-style" mode without breaking current OpenAI, xAI, or Vertex behavior.

Branch: `codex/celeste-claw-buildout-plan`  
Planning date: `2026-03-01`

## Goals
1. Improve maintainability and testability of the CLI core.
2. Make behavior deterministic where output order currently depends on map iteration.
3. Remove known technical debt that increases break risk (deprecated IPFS client, stale docs, stubs).
4. Introduce a Celeste-flavored claw-style runtime mode as an additive feature.
5. Include unit tests and validation criteria for every feature before merge.

## Non-Goals
1. No change to backend selection contract in `cmd/celeste/llm/interface.go`.
2. No auth flow redesign for xAI Collections or Vertex ADC/service account.
3. No user-facing command removals in this phase.

## Compatibility Guardrails
1. Keep `LLMBackend` interface unchanged.
2. Keep xAI backend request shape and collections behavior unchanged.
3. Keep Vertex initialization/auth bootstrap unchanged.
4. Treat new claw-style runtime as opt-in profile/mode, not default replacement.

## Workstreams

### Workstream A: CLI Entrypoint Decomposition
Scope:
1. Split `cmd/celeste/main.go` into smaller command-runner modules.
2. Move command dispatch to a `run(args []string) (exitCode int, err error)` flow.
3. Reduce scattered `os.Exit` calls to one top-level exit point.

Implementation checklist:
1. Create `cmd/celeste/app_run.go` for argument parsing and dispatch.
2. Move command handlers into focused files (`run_chat.go`, `run_config.go`, etc.).
3. Preserve current command names and behavior.

Unit tests:
1. Table-driven tests for `run()` dispatch decisions and exit codes.
2. Tests for unknown command fallback behavior.
3. Tests for `-config` flag extraction with both forms (`-config name`, `-config=name`).

Validation:
1. `go test ./cmd/celeste -v`
2. `go run ./cmd/celeste --version`
3. Manual smoke: `chat`, `providers`, `skills`, `collections`, `session`.

Acceptance criteria:
1. `main.go` reduced to bootstrap-only flow.
2. Behavior parity confirmed by existing and new tests.

### Workstream B: Deterministic Ordering and Output Stability
Scope:
1. Sort provider lists before returning from registry helpers.
2. Sort skill/tool definitions before passing to model and UI paths.
3. Replace bubble sort in model sorting with stable standard-library sort.

Implementation checklist:
1. Add sorted key extraction in provider and skills registries.
2. Apply `sort.SliceStable` for model ordering logic.
3. Verify command outputs are deterministic across runs.

Unit tests:
1. Registry ordering tests for provider list and tool-capable provider list.
2. Skills registry test ensuring deterministic ordering of `GetAllSkills` and tool definitions.
3. Model sorting test for stable ordering when capabilities are equal.

Validation:
1. Re-run relevant package tests three consecutive times to confirm stable output.
2. Snapshot-like command output assertions in command tests.

Acceptance criteria:
1. No map iteration ordering leaks in user-facing output or tool payload ordering.

### Workstream C: Skills UI/Model Cleanup
Scope:
1. Resolve duplicate/stub skills model path in TUI.
2. Ensure one authoritative flow for runtime skill definitions.

Implementation checklist:
1. Decide between implementing `SkillsModel` or removing the stub path.
2. Keep existing `SkillsBrowserModel` behavior for skill browsing.
3. Ensure any config/status panel uses live `llmClient.GetSkills()` data.

Unit tests:
1. TUI tests for skills panel rendering mode selection.
2. Tests ensuring skills count and display reflect runtime registry, not stubs.

Validation:
1. `go test ./cmd/celeste/tui -v`
2. Manual TUI check: skills view reflects loaded skills.

Acceptance criteria:
1. No TODO/stub skills panel path in active runtime flow.

### Workstream D: IPFS Client Migration (Deprecated Dependency Removal)
Scope:
1. Replace deprecated `go-ipfs-http-client` dependency with maintained alternative.
2. Keep existing `ipfs` skill interface and arguments stable.

Implementation checklist:
1. Introduce adapter layer so skill handler contract remains unchanged.
2. Port operations: upload, download, pin, unpin, list pins.
3. Preserve provider-specific auth behavior (Infura/Pinata/custom node).

Unit tests:
1. Adapter tests with mocked IPFS API for each operation.
2. Auth-header construction tests for Infura and Pinata.
3. Error mapping tests (validation, connection, file errors).

Validation:
1. `go test ./cmd/celeste/skills -v`
2. Manual smoke with local IPFS node (if available).

Acceptance criteria:
1. No `nolint:staticcheck` markers for deprecated IPFS client usage remain.
2. Existing `ipfs` skill function signature remains backward compatible.

### Workstream E: Documentation Sync and Accuracy Pass
Scope:
1. Align FUTURE_WORK and provider docs with actual implemented state.
2. Remove outdated TODO claims and stale "needs testing" statements where tests exist.

Implementation checklist:
1. Update `docs/FUTURE_WORK.md` to reflect current command support.
2. Update `docs/LLM_PROVIDERS.md` status sections based on current backend/test reality.
3. Add explicit "validated by" test command references.

Unit tests:
1. N/A (docs-only), but add doc-lint check if introduced.

Validation:
1. Manual doc review against current code paths and test files.
2. Optional markdown lint run if tooling is available.

Acceptance criteria:
1. No documented TODO contradicts existing code behavior.

### Workstream F: Celeste-Flavored Claw-Style Mode
Scope:
1. Add an opt-in runtime profile that supports a claw-style interaction loop while reusing Celeste core.
2. Keep current mode intact and default.

Implementation checklist:
1. Define `mode` abstraction (`classic`, `claw`) at app configuration layer.
2. Implement claw loop orchestration as a separate module on top of existing LLM/skills client.
3. Keep persona, routing hints, and tool gating configurable per mode.
4. Add profile presets (`celeste-classic`, `celeste-claw`) without removing existing config behavior.

Unit tests:
1. Mode selection tests from config and CLI flags.
2. Claw-loop orchestration tests for:
   1. single-step response
   2. tool call cycle
   3. multi-tool cycle
   4. max-iteration safety stop
3. Regression tests ensuring `classic` mode behavior is unchanged.

Validation:
1. `go test ./cmd/celeste/...` (excluding environment-restricted suites when needed).
2. Manual runs in both modes with OpenAI, xAI, and Vertex configs.
3. Verify collections still function in xAI mode and Vertex auth still boots correctly.

Acceptance criteria:
1. New mode is opt-in and feature-flagged/profile-gated.
2. Classic mode output and command behavior remain unchanged.
3. Tool-calling passes in both modes for supported providers.

## Cross-Provider Validation Matrix
For each merged workstream, run:
1. `go test ./cmd/celeste/llm -v`
2. `go test ./cmd/celeste/providers -v`
3. `go test ./cmd/celeste/tui -v`
4. `go test ./cmd/celeste/skills -v`
5. `go test ./cmd/celeste/commands -v`

Provider checks:
1. OpenAI: tool calls and multi-tool completion.
2. xAI: backend selection, collections pass-through unchanged.
3. Vertex: backend selection, required-schema parsing, auth bootstrap unchanged.

## Delivery Phases

### Phase 1 (Low Risk Hardening)
1. Workstream B (deterministic ordering)
2. Workstream E (docs sync)
3. Workstream C (skills stub cleanup)

Gate:
1. All affected package tests pass.
2. No behavior regressions in chat/tool loop.

### Phase 2 (Structural Refactor)
1. Workstream A (entrypoint decomposition)

Gate:
1. Command parity tests pass.
2. Manual smoke on top-level commands pass.

### Phase 3 (Dependency Migration)
1. Workstream D (IPFS migration)

Gate:
1. Skills tests pass including IPFS adapter coverage.
2. Manual IPFS smoke (if environment supports it).

### Phase 4 (New Mode Introduction)
1. Workstream F (claw-style mode)

Gate:
1. New mode tests pass.
2. Classic-mode regression tests pass.
3. Cross-provider validation matrix passes.

## Rollback Plan
1. Keep each phase in separate commits for selective revert.
2. If provider regression appears, disable new mode/profile path behind feature flag.
3. Revert only the offending workstream commit(s) and rerun provider/tui/skills suites.
4. Document rollback reason and recovery steps in `CHANGELOG.md`.

