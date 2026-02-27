# OpenAI Skill Compatibility Hardening Plan (xAI/Vertex Safe)

## Summary
This document captures the tool-calling reliability issues found in Celeste CLI and the implementation strategy to fix them without breaking xAI or Vertex functionality.

Version target: `1.5.5`  
Branch: `codex/openai-skill-hardening`

## Findings (Severity + References)

### P0: Tool definitions not sent from TUI chat path
- TUI chat used `m.skills.GetDefinitions()` (skills panel stub) which always returned an empty list.
- Result: providers received no user skill definitions, so function calling was effectively disabled in normal chat.
- References:
  - `cmd/celeste/tui/app.go` (skills panel stub and send path)

### P1: Only first tool call executed
- Streaming handler captured all tool calls but only dispatched the first call in a turn.
- Result: multi-tool turns from OpenAI-compatible providers were partially executed.
- References:
  - `cmd/celeste/main.go` tool call dispatch logic
  - `cmd/celeste/tui/app.go` skill result continuation flow

### P1: Missing validation for disk-loaded custom skills
- Custom skills loaded from `~/.celeste/skills/*.json` only validated `name`.
- Result: malformed schemas could be forwarded to provider APIs and fail requests.
- References:
  - `cmd/celeste/skills/registry.go`

### P2: Serialization and argument parsing errors were masked
- Tool parameter marshal errors were ignored.
- Tool argument JSON parse errors silently fell back to `{}`.
- Result: lower observability and misleading downstream errors.
- References:
  - `cmd/celeste/llm/backend_openai.go`
  - `cmd/celeste/llm/backend_xai.go`
  - `cmd/celeste/main.go`

## Root Cause Analysis
1. The TUI skills panel implementation is a rendering stub and not a source of runtime tool definitions.
2. Tool dispatch logic assumed one tool call per turn despite providers returning arrays.
3. Skill schema trust boundary was too permissive for filesystem-loaded JSON.
4. Error handling favored silent fallback over explicit fail-fast or structured reporting.

## Provider Compatibility Constraints

### OpenAI
- Must receive complete `tools` definitions for function calling.
- Must receive assistant `tool_calls` message followed by one `tool` response per `tool_call_id`.

### xAI
- Native backend and Collections (`collection_ids`) request shape must remain unchanged.
- Tool conversion hardening should only skip invalid user-defined function tools, not built-in xAI behavior.

### Vertex (Google backend)
- Native auth/bootstrap flow must remain unchanged.
- Schema conversion must handle both `required` list representations (`[]string` and `[]interface{}`).

## Phased Implementation Checklist

### Phase 0: Release Baseline
- [x] Create branch `codex/openai-skill-hardening`
- [x] Bump version to `1.5.5`
- [x] Add changelog entry for this hardening release

### Phase 1: Functional Compatibility Fixes
- [ ] Use `llmClient.GetSkills()` as the runtime source of tool definitions in TUI request paths.
- [ ] Add batch tool-call message and execute all calls in sequence per assistant turn.
- [ ] Append all tool results first, then send one continuation request to the LLM.
- [ ] Preserve NSFW/provider gating semantics.

### Phase 2: Validation and Error Hardening
- [ ] Add skill schema validator for disk-loaded custom skills.
- [ ] Reject invalid custom skill files during load with clear warnings.
- [ ] Stop ignoring tool schema marshal failures in OpenAI/xAI backends; skip invalid tools with logging.
- [ ] Convert tool argument parse failures into explicit tool-error messages.
- [ ] Make Google schema conversion tolerant to `required` representation differences.

### Test and Verification
- [ ] Skills validation tests (valid + invalid custom skill files)
- [ ] OpenAI tool conversion tests (success + skip-on-marshal-error)
- [ ] xAI tool conversion tests (success + skip-on-marshal-error)
- [ ] Google schema conversion tests (`required` parsing variants)
- [ ] TUI multi-tool sequencing tests
- [ ] Package test runs:
  - `go test ./cmd/celeste/skills -v`
  - `go test ./cmd/celeste/llm -v`
  - `go test ./cmd/celeste/providers -v`
  - `go test ./cmd/celeste/tui -v`

## Acceptance Criteria
1. Branch exists as `codex/openai-skill-hardening`.
2. Version reports `1.5.5`.
3. Non-NSFW chat sends tools when skills are enabled.
4. All tool calls in a turn are executed and replied before continuation.
5. Invalid custom skills are filtered and do not break model requests.
6. xAI and Vertex paths remain functional and covered by regression tests.

## Rollback Plan
1. If provider regressions appear, rollback to previous tag (`1.5.4`) in production distribution.
2. Temporarily disable Phase 2 validation gates (if needed) behind conservative fallback to preserve availability while debugging.
3. Re-run provider-specific tests (`llm`, `providers`) before re-enabling.
4. Publish a patch changelog entry documenting rollback cause and restored behavior.
