# Future Work & TODO List

This document tracks planned features, incomplete integrations, and improvement opportunities for Celeste CLI.

---

## 📋 Priority 1: Code Cleanup (Immediate)

### ✅ COMPLETED
- [x] Remove dead files: `scaffolding.go`, `ui.go`, `animation.go` (-911 lines)
- [x] Fix mermaid diagrams in README
- [x] Fix image display size

### ✅ COMPLETED (Recent)
- [x] `/providers` command family implemented and wired in command layer
- [x] `ListProviders()` and `GetToolCallingProviders()` are actively used
- [x] Skills package has schema validation tests
- [x] TUI has tool-call batch flow tests

---

## 🔌 Priority 2: Provider Integrations

### Tier 1: Limited Support (Short-term)

#### DigitalOcean Gradient
- **Status**: LIMITED - Function calling disabled
- **Issue**: Agent API requires cloud-hosted functions, not local execution
- **Location**: `providers/registry.go` lines 120-130
- **Tasks**:
  - [ ] Update documentation to clarify limitations
  - [ ] Add provider notes to README
  - [ ] Consider removing if not actively used

#### ElevenLabs
- **Status**: UNTESTED - Voice AI focused
- **Issue**: Function calling support unknown
- **Location**: `providers/registry.go` lines 132-142
- **Tasks**:
  - [ ] Test with real API key
  - [ ] Document function calling capability
  - [ ] Update registry or remove if unsupported

#### Anthropic Claude (OpenAI Compatibility)
- **Status**: TESTING - Uses OpenAI SDK compatibility layer
- **Issue**: Native API differs, not fully tested
- **Location**: `providers/registry.go` lines 70-80
- **Note**: "OpenAI SDK compatibility is for testing only. Native API recommended."
- **Tasks**:
  - [ ] Implement native Anthropic API support
  - [ ] Test tool calling with Messages API
  - [ ] Document as experimental until native support added

### Tier 2: Enterprise Providers (Long-term)

#### AWS Bedrock
- **Status**: NOT IMPLEMENTED
- **Complexity**: Requires AWS SDK, IAM roles, region configuration
- **Use Case**: Enterprise customers with existing AWS infrastructure
- **Tasks**:
  - [ ] Evaluate user demand
  - [ ] Research Bedrock function calling capabilities
  - [ ] Implement if enterprise users request

#### Azure OpenAI
- **Status**: NOT IMPLEMENTED
- **Complexity**: Different auth model (Azure AD), enterprise-focused
- **Use Case**: Enterprise customers with Azure subscriptions
- **Tasks**:
  - [ ] Evaluate user demand
  - [ ] Research Azure OpenAI API differences
  - [ ] Implement if enterprise users request

#### GCP Model Garden
- **Status**: NOT NEEDED - Vertex AI covers Google's offerings
- **Note**: Redundant with existing Gemini/Vertex support
- **Decision**: DO NOT IMPLEMENT

---

## ✨ Priority 3: Feature Enhancements

### CLI Commands

#### Provider UX Refinement
- **Status**: PARTIALLY COMPLETE
- **Current**: `/providers`, `/providers --tools`, `/providers info <name>`, `/providers current`
- **Next Tasks**:
  - [ ] Improve provider output ordering and consistency
  - [ ] Add richer provider capability metadata in output
  - [ ] Add provider health diagnostics command output

#### Autonomous Agent Mode (Claw-style foundation)
- **Status**: PHASE 1 COMPLETE (runtime loop + checkpoints + eval harness)
- **Current**:
  - `celeste agent` command supports autonomous multi-turn execution
  - Run checkpointing + resume/list flows implemented
  - Agent-focused development skills added (`dev_*` file/search/command tools)
  - Basic eval harness for scenario JSON files implemented
- **Next Tasks**:
  - [ ] Add explicit planner state machine (plan -> execute -> verify)
  - [ ] Add stronger completion/verifier stage (tests/lint/build gating)
  - [ ] Add artifact bundles per run (plan/actions/diff/validation)
  - [ ] Add benchmark suites for coding/content tasks with CI scoring

#### Model Step Validation (Venice.ai)
- **Purpose**: Enforce model-specific step limits for image generation
- **Issue**: Some models have lower limits (e.g., wai-Illustrious max is 30)
- **Location**: `venice/media.go` line 70
- **Tasks**:
  - [ ] Create model capabilities map
  - [ ] Add validation in `GenerateImage()`
  - [ ] Return clear error messages for invalid steps

---

## 🧪 Priority 4: Testing & Quality

### Provider Testing
- [ ] Create integration test suite for all providers
- [ ] Add mock server for offline testing
- [ ] Test function calling on all supported providers
- [ ] Document which models support parallel function calling

### Code Coverage
- [ ] Increase test coverage above 40% threshold
- [x] Add unit tests for skills package
- [x] Add integration-style tests for TUI tool-call interactions
- [ ] Add deterministic-ordering regression tests for provider/skill output surfaces

---

## 📚 Priority 5: Documentation

### Provider Documentation
- [x] Maintain `docs/LLM_PROVIDERS.md` as provider compatibility source
- [ ] Add troubleshooting guides for common provider issues
- [ ] Keep provider matrix synchronized with integration test outcomes

### Architecture Documentation
- [ ] Create detailed function call flow diagram
- [ ] Document TUI component lifecycle
- [ ] Add contribution guide for new skills
- [ ] Document provider integration requirements

---

## 🔍 Functions to Monitor (Keep for Now)

These functions are currently unused but may be useful for future features:

- `HasHandler()` in `skills/registry.go` - Useful for debugging/validation
- `Count()` in `skills/registry.go` - Useful for stats/metrics
- `GetBestToolModel()` in `providers/models.go` - Useful for auto-selection

**Decision**: Keep these small utility functions until clear they won't be needed.

---

## 📊 Codebase Statistics

### Current State
- **Total Go files**: 21 active (24 original - 3 removed)
- **Lines of code**: ~6,500 (after removing 911 lines of dead code)
- **Active skills**: 18 built-in + user-defined
- **Supported providers**: 9 (3 fully tested, 6 experimental/limited)

### Post-Cleanup Goals
- **Target test coverage**: 60%+
- **Target providers fully tested**: 5+ (add Gemini, Anthropic)
- **Documentation completeness**: 90%+

---

## 🚀 How to Contribute

When working on items from this list:

1. **Check Status**: Ensure item isn't already in progress
2. **Create Issue**: Reference this TODO item
3. **Branch**: Use `feature/provider-xyz` or `fix/cleanup-xyz`
4. **Test**: Add tests for new providers/features
5. **Document**: Update relevant docs and this file
6. **PR**: Reference TODO item in PR description

---

## 📝 Notes

- This document is maintained as part of the codebase cleanup initiative
- Dead code audit performed: 2025-12-06
- Last updated: 2026-03-01
- See `CLAUDE.md` for development guidelines
- See `ROADMAP.md` for strategic direction

---

**Legend**:
- ✅ COMPLETED - Task finished
- 🔄 IN PROGRESS - Currently being worked on
- [ ] TODO - Not started yet
