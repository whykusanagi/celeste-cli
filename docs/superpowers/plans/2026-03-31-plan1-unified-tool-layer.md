# Plan 1: Unified Tool Abstraction Layer

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `skills/` and `agent/dev_skills.go` with a single `tools/` package that provides one `Tool` interface for all runtime modes.

**Architecture:** New `tools/` package under `cmd/celeste/tools/` with a `Tool` interface, `Registry` for tool management, and `builtin/` sub-package for all implementations. The existing `skills.Registry`, `skills.Executor`, `skills.Skill`, and `agent/dev_skills.go` are replaced entirely. All consumers (`main.go`, `agent/runtime.go`, `tui/app.go`, `orchestrator/`) are updated to use the new interface.

**Tech Stack:** Go 1.26, standard library, existing dependencies (go-ethereum, go-qrcode, etc.)

**Prerequisite Plans:** None — this is the foundation.

---

## Codebase Context

**Current tool systems being replaced:**

1. **`cmd/celeste/skills/`** — 15 Go files, ~6000 lines
   - `registry.go`: `Skill` struct (Name, Description, Parameters map), `Registry` with `skills map[string]Skill`, `handlers map[string]SkillHandler`, `GetToolDefinitions()` returns `[]map[string]interface{}`
   - `executor.go`: `Executor` wraps registry, `Execute(ctx, name, argsJSON)` returns `*ExecutionResult`
   - `builtin.go`: `RegisterBuiltinSkills(registry, configLoader)` registers 23 skills + handlers
   - `validation.go`: `ValidateSkillDefinition(skill)` checks name, params, required fields
   - `crypto*.go`: 4 crypto skills (IPFS, Alchemy, Blockmon, WalletSecurity)
   - `test_helpers.go`: `MockConfigLoader` with per-skill config getters

2. **`cmd/celeste/agent/dev_skills.go`** — 687 lines
   - `RegisterDevSkills(registry, workspace)` adds 6 dev tools: `dev_list_files`, `dev_read_file`, `dev_write_file`, `dev_patch_file`, `dev_search_files`, `dev_run_command`
   - `RegisterReadOnlyDevSkills(registry, workspace)` for reviewer agents (excludes write/run)
   - Path traversal prevention, command output truncation (12KB), file read limits (200KB)

**Key consumers that will be updated (in later plans):**

- `cmd/celeste/main.go`: `TUIClientAdapter` calls `client.GetSkills()` and `client.ExecuteSkill()`
- `cmd/celeste/llm/client.go`: `Client` holds `registry *skills.Registry`, calls `registry.GetToolDefinitions()` and `registry.Execute()`
- `cmd/celeste/agent/runtime.go`: `Runner` holds `registry *skills.Registry`, calls `r.client.ExecuteSkill()`
- `cmd/celeste/tui/app.go`: `SkillCallBatchMsg`, `SkillResultMsg`, `SkillCallRequest` types

**Module path:** `github.com/whykusanagi/celeste-cli`

---

## File Structure

```
cmd/celeste/tools/                    # NEW package
├── tool.go                           # Tool interface, ToolResult, ProgressEvent, InterruptBehavior
├── registry.go                       # Registry with mode filtering, custom tool loading
├── registry_test.go                  # Registry unit tests
├── builtin/                          # NEW sub-package
│   ├── base.go                       # BaseTool helper struct (reduces boilerplate)
│   ├── bash.go                       # Shell execution (from dev_run_command)
│   ├── read_file.go                  # File reading (from dev_read_file)
│   ├── write_file.go                 # File creation (from dev_write_file)
│   ├── patch_file.go                 # Surgical edits (from dev_patch_file)
│   ├── list_files.go                 # Directory listing (from dev_list_files)
│   ├── search.go                     # Text search (from dev_search_files)
│   ├── weather.go                    # Weather skill
│   ├── tarot.go                      # Tarot skill
│   ├── crypto.go                     # Crypto skills (IPFS, Alchemy, Blockmon, WalletSecurity)
│   ├── media.go                      # Image gen/upscale (Venice)
│   ├── currency.go                   # Currency conversion
│   ├── encoding.go                   # Base64 encode/decode
│   ├── hash.go                       # Hash generation
│   ├── uuid.go                       # UUID generation
│   ├── password.go                   # Password generation
│   ├── reminder.go                   # Reminders
│   ├── notes.go                      # Notes
│   ├── qrcode.go                     # QR code generation
│   ├── twitch.go                     # Twitch live check
│   ├── youtube.go                    # YouTube videos
│   ├── collections.go                # xAI Collections
│   ├── register.go                   # RegisterAll() wires all built-in tools
│   ├── bash_test.go                  # Dev tool tests
│   ├── read_file_test.go
│   └── register_test.go              # All tools registered correctly
```

**Modified files:**
- `cmd/celeste/llm/client.go` — swap `skills.Registry` for `tools.Registry`
- `cmd/celeste/main.go` — swap skill init for tool init
- `cmd/celeste/agent/runtime.go` — swap skill execution for tool execution

**Deleted files (after migration complete):**
- `cmd/celeste/skills/` — entire package
- `cmd/celeste/agent/dev_skills.go`

---

### Task 1: Core Tool Interface

**Files:**
- Create: `cmd/celeste/tools/tool.go`
- Test: `cmd/celeste/tools/tool_test.go`

- [ ] **Step 1: Write the test for Tool interface types**

```go
// cmd/celeste/tools/tool_test.go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockTool implements Tool for testing
type mockTool struct {
	name             string
	description      string
	params           json.RawMessage
	concurrencySafe  bool
	readOnly         bool
	interruptBehavior InterruptBehavior
	executeFunc      func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error)
}

func (m *mockTool) Name() string                                    { return m.name }
func (m *mockTool) Description() string                             { return m.description }
func (m *mockTool) Parameters() json.RawMessage                     { return m.params }
func (m *mockTool) IsConcurrencySafe(input map[string]any) bool     { return m.concurrencySafe }
func (m *mockTool) IsReadOnly() bool                                { return m.readOnly }
func (m *mockTool) ValidateInput(input map[string]any) error        { return nil }
func (m *mockTool) InterruptBehavior() InterruptBehavior            { return m.interruptBehavior }
func (m *mockTool) Execute(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, input, progress)
	}
	return ToolResult{Content: "ok"}, nil
}

func TestToolResult(t *testing.T) {
	r := ToolResult{Content: "hello", Error: false, Metadata: map[string]any{"key": "val"}}
	assert.Equal(t, "hello", r.Content)
	assert.False(t, r.Error)
	assert.Equal(t, "val", r.Metadata["key"])
}

func TestProgressEvent(t *testing.T) {
	p := ProgressEvent{ToolName: "bash", Message: "running", Percent: 0.5}
	assert.Equal(t, "bash", p.ToolName)
	assert.Equal(t, 0.5, p.Percent)
}

func TestInterruptBehavior(t *testing.T) {
	assert.Equal(t, InterruptBehavior(0), InterruptCancel)
	assert.Equal(t, InterruptBehavior(1), InterruptBlock)
}

func TestMockToolImplementsInterface(t *testing.T) {
	var _ Tool = &mockTool{}
	tool := &mockTool{
		name:        "test",
		description: "test tool",
		params:      json.RawMessage(`{"type":"object","properties":{}}`),
		readOnly:    true,
	}
	assert.Equal(t, "test", tool.Name())
	assert.True(t, tool.IsReadOnly())

	result, err := tool.Execute(context.Background(), nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "ok", result.Content)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/celeste/tools/ -v -run TestToolResult`
Expected: FAIL — package doesn't exist yet

- [ ] **Step 3: Write the Tool interface**

```go
// cmd/celeste/tools/tool.go
package tools

import (
	"context"
	"encoding/json"
)

// Tool is the unified interface for all tool implementations.
// Every tool in the system — built-in skills, dev tools, MCP tools,
// and custom user tools — implements this interface.
type Tool interface {
	// Name returns the tool's unique identifier (e.g., "bash", "read_file", "get_weather").
	Name() string

	// Description returns a human-readable description for the LLM.
	Description() string

	// Parameters returns the JSON Schema for the tool's input parameters.
	Parameters() json.RawMessage

	// IsConcurrencySafe returns true if this tool can safely run in parallel
	// with other tools. The input is provided so tools can make input-dependent
	// decisions (e.g., read_file is safe, but write_file is not).
	IsConcurrencySafe(input map[string]any) bool

	// IsReadOnly returns true if the tool only reads state (never mutates).
	IsReadOnly() bool

	// ValidateInput checks tool-specific input constraints before execution.
	// Return nil if valid. Called before Execute.
	ValidateInput(input map[string]any) error

	// Execute runs the tool with the given input. Progress events should be
	// sent to the progress channel during long-running operations. The channel
	// may be nil if the caller doesn't need progress updates.
	Execute(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error)

	// InterruptBehavior declares how this tool responds to user interrupts (Ctrl+C).
	InterruptBehavior() InterruptBehavior
}

// InterruptBehavior controls how a tool handles user interrupts.
type InterruptBehavior int

const (
	// InterruptCancel means Ctrl+C aborts the tool immediately.
	InterruptCancel InterruptBehavior = iota
	// InterruptBlock means Ctrl+C is blocked until the tool completes.
	InterruptBlock
)

// ToolResult is the output of a tool execution.
type ToolResult struct {
	Content  string         `json:"content"`
	Error    bool           `json:"error,omitempty"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ProgressEvent is emitted during tool execution for real-time TUI updates.
type ProgressEvent struct {
	ToolName string  `json:"tool_name"`
	Message  string  `json:"message"`
	Percent  float64 `json:"percent"` // -1 for indeterminate
}

// RuntimeMode determines which tools are available.
type RuntimeMode int

const (
	ModeChat RuntimeMode = iota
	ModeClaw
	ModeAgent
	ModeOrchestrator
)
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/celeste/tools/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/tool.go cmd/celeste/tools/tool_test.go
git commit -m "feat(tools): add unified Tool interface and core types"
```

---

### Task 2: BaseTool Helper

**Files:**
- Create: `cmd/celeste/tools/builtin/base.go`
- Test: `cmd/celeste/tools/builtin/base_test.go`

- [ ] **Step 1: Write the test**

```go
// cmd/celeste/tools/builtin/base_test.go
package builtin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestBaseTool(t *testing.T) {
	bt := &BaseTool{
		ToolName:        "test_tool",
		ToolDescription: "A test tool",
		ToolParameters:  json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}}}`),
		ReadOnly:        true,
		ConcurrencySafe: true,
	}

	assert.Equal(t, "test_tool", bt.Name())
	assert.Equal(t, "A test tool", bt.Description())
	assert.True(t, bt.IsReadOnly())
	assert.True(t, bt.IsConcurrencySafe(nil))
	assert.Equal(t, tools.InterruptCancel, bt.InterruptBehavior())
	require.NoError(t, bt.ValidateInput(nil))
}

func TestBaseToolRequiredFields(t *testing.T) {
	bt := &BaseTool{
		ToolName:       "req_test",
		ToolDescription: "Test required",
		ToolParameters: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}`),
		RequiredFields: []string{"name"},
	}

	err := bt.ValidateInput(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name")

	err = bt.ValidateInput(map[string]any{"name": "alice"})
	assert.NoError(t, err)
}

func TestBaseToolExecuteNotImplemented(t *testing.T) {
	bt := &BaseTool{ToolName: "noop"}
	_, err := bt.Execute(context.Background(), nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not implemented")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/celeste/tools/builtin/ -v`
Expected: FAIL

- [ ] **Step 3: Write BaseTool**

```go
// cmd/celeste/tools/builtin/base.go
package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// BaseTool provides default implementations for the Tool interface.
// Embed this in concrete tools to reduce boilerplate.
type BaseTool struct {
	ToolName        string
	ToolDescription string
	ToolParameters  json.RawMessage
	ReadOnly        bool
	ConcurrencySafe bool
	Interrupt       tools.InterruptBehavior
	RequiredFields  []string
}

func (b *BaseTool) Name() string                                { return b.ToolName }
func (b *BaseTool) Description() string                         { return b.ToolDescription }
func (b *BaseTool) Parameters() json.RawMessage                 { return b.ToolParameters }
func (b *BaseTool) IsConcurrencySafe(input map[string]any) bool { return b.ConcurrencySafe }
func (b *BaseTool) IsReadOnly() bool                            { return b.ReadOnly }
func (b *BaseTool) InterruptBehavior() tools.InterruptBehavior  { return b.Interrupt }

func (b *BaseTool) ValidateInput(input map[string]any) error {
	for _, field := range b.RequiredFields {
		val, ok := input[field]
		if !ok || val == nil || val == "" {
			return fmt.Errorf("required field '%s' is missing", field)
		}
	}
	return nil
}

func (b *BaseTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{}, fmt.Errorf("Execute not implemented for tool '%s'", b.ToolName)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/celeste/tools/builtin/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/builtin/
git commit -m "feat(tools): add BaseTool helper for reducing boilerplate"
```

---

### Task 3: Tool Registry

**Files:**
- Create: `cmd/celeste/tools/registry.go`
- Test: `cmd/celeste/tools/registry_test.go`

- [ ] **Step 1: Write the test**

```go
// cmd/celeste/tools/registry_test.go
package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	tool := &mockTool{name: "test", description: "a test"}
	r.Register(tool)

	got, ok := r.Get("test")
	require.True(t, ok)
	assert.Equal(t, "test", got.Name())
}

func TestRegistryGetMissing(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("nonexistent")
	assert.False(t, ok)
}

func TestRegistryGetAll(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{name: "b"})
	r.Register(&mockTool{name: "a"})

	all := r.GetAll()
	require.Len(t, all, 2)
	// Sorted by name
	assert.Equal(t, "a", all[0].Name())
	assert.Equal(t, "b", all[1].Name())
}

func TestRegistryGetToolDefinitions(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{
		name:        "weather",
		description: "get weather",
		params:      json.RawMessage(`{"type":"object","properties":{"zip":{"type":"string"}}}`),
	})

	defs := r.GetToolDefinitions()
	require.Len(t, defs, 1)
	assert.Equal(t, "function", defs[0]["type"])
	fn := defs[0]["function"].(map[string]any)
	assert.Equal(t, "weather", fn["name"])
}

func TestRegistryGetTools_ModeFiltering(t *testing.T) {
	r := NewRegistry()
	// Dev tool
	r.RegisterWithModes(&mockTool{name: "bash"}, ModeAgent, ModeClaw, ModeChat)
	// Skill-only tool
	r.RegisterWithModes(&mockTool{name: "tarot"}, ModeChat, ModeClaw)

	agentTools := r.GetTools(ModeAgent)
	assert.Len(t, agentTools, 1)
	assert.Equal(t, "bash", agentTools[0].Name())

	chatTools := r.GetTools(ModeChat)
	assert.Len(t, chatTools, 2)
}

func TestRegistryExecute(t *testing.T) {
	r := NewRegistry()
	r.Register(&mockTool{
		name: "echo",
		executeFunc: func(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
			return ToolResult{Content: input["text"].(string)}, nil
		},
	})

	result, err := r.Execute(context.Background(), "echo", map[string]any{"text": "hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello", result.Content)
}

func TestRegistryExecuteMissing(t *testing.T) {
	r := NewRegistry()
	_, err := r.Execute(context.Background(), "nope", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestRegistryCount(t *testing.T) {
	r := NewRegistry()
	assert.Equal(t, 0, r.Count())
	r.Register(&mockTool{name: "a"})
	assert.Equal(t, 1, r.Count())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/celeste/tools/ -v -run TestRegistry`
Expected: FAIL

- [ ] **Step 3: Write the Registry**

```go
// cmd/celeste/tools/registry.go
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Registry manages tool definitions, mode filtering, and execution.
type Registry struct {
	mu        sync.RWMutex
	tools     map[string]Tool
	toolModes map[string][]RuntimeMode // which modes each tool is available in
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:     make(map[string]Tool),
		toolModes: make(map[string][]RuntimeMode),
	}
}

// Register adds a tool available in all modes.
func (r *Registry) Register(tool Tool) {
	r.RegisterWithModes(tool, ModeChat, ModeClaw, ModeAgent, ModeOrchestrator)
}

// RegisterWithModes adds a tool available only in specific modes.
func (r *Registry) RegisterWithModes(tool Tool, modes ...RuntimeMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
	r.toolModes[tool.Name()] = modes
}

// Get returns a tool by name.
func (r *Registry) Get(name string) (Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// GetAll returns all registered tools sorted by name.
func (r *Registry) GetAll() []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]Tool, 0, len(r.tools))
	for _, t := range r.tools {
		result = append(result, t)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})
	return result
}

// GetTools returns tools available in the specified runtime mode, sorted by name.
func (r *Registry) GetTools(mode RuntimeMode) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Tool
	for name, tool := range r.tools {
		modes := r.toolModes[name]
		for _, m := range modes {
			if m == mode {
				result = append(result, tool)
				break
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name() < result[j].Name()
	})
	return result
}

// GetToolDefinitions returns all tools in OpenAI function-calling format.
func (r *Registry) GetToolDefinitions() []map[string]any {
	tools := r.GetAll()
	defs := make([]map[string]any, len(tools))
	for i, tool := range tools {
		var params any
		if err := json.Unmarshal(tool.Parameters(), &params); err != nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		defs[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name(),
				"description": tool.Description(),
				"parameters":  params,
			},
		}
	}
	return defs
}

// GetToolDefinitionsForMode returns tools in OpenAI format filtered by mode.
func (r *Registry) GetToolDefinitionsForMode(mode RuntimeMode) []map[string]any {
	tools := r.GetTools(mode)
	defs := make([]map[string]any, len(tools))
	for i, tool := range tools {
		var params any
		if err := json.Unmarshal(tool.Parameters(), &params); err != nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		defs[i] = map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        tool.Name(),
				"description": tool.Description(),
				"parameters":  params,
			},
		}
	}
	return defs
}

// Execute runs a tool by name with the given input.
func (r *Registry) Execute(ctx context.Context, name string, input map[string]any) (ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return ToolResult{}, fmt.Errorf("tool not found: %s", name)
	}
	if err := tool.ValidateInput(input); err != nil {
		return ToolResult{Content: fmt.Sprintf("validation error: %v", err), Error: true}, err
	}
	return tool.Execute(ctx, input, nil)
}

// ExecuteWithProgress runs a tool and sends progress events to the channel.
func (r *Registry) ExecuteWithProgress(ctx context.Context, name string, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return ToolResult{}, fmt.Errorf("tool not found: %s", name)
	}
	if err := tool.ValidateInput(input); err != nil {
		return ToolResult{Content: fmt.Sprintf("validation error: %v", err), Error: true}, err
	}
	return tool.Execute(ctx, input, progress)
}

// Count returns the number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// LoadCustomTools loads tool definitions from JSON files in the given directory.
// This preserves compatibility with ~/.celeste/skills/*.json custom skills.
func (r *Registry) LoadCustomTools(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create tools directory: %w", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return fmt.Errorf("failed to list tool files: %w", err)
	}
	for _, file := range files {
		if err := r.loadCustomToolFile(file); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to load tool %s: %v\n", file, err)
		}
	}
	return nil
}

// customToolDef matches the existing skills.json format for backwards compatibility.
type customToolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func (r *Registry) loadCustomToolFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var def customToolDef
	if err := json.Unmarshal(data, &def); err != nil {
		return err
	}
	if def.Name == "" {
		return fmt.Errorf("tool name is required")
	}
	params, _ := json.Marshal(def.Parameters)
	// Custom tools are registered but have no handler — they're defined for
	// the LLM to see but execution returns an error until a handler is attached.
	r.Register(&customTool{
		name:        def.Name,
		description: def.Description,
		params:      params,
	})
	return nil
}

// customTool is a tool loaded from a JSON definition file.
type customTool struct {
	name        string
	description string
	params      json.RawMessage
}

func (c *customTool) Name() string                                    { return c.name }
func (c *customTool) Description() string                             { return c.description }
func (c *customTool) Parameters() json.RawMessage                     { return c.params }
func (c *customTool) IsConcurrencySafe(input map[string]any) bool     { return false }
func (c *customTool) IsReadOnly() bool                                { return false }
func (c *customTool) ValidateInput(input map[string]any) error        { return nil }
func (c *customTool) InterruptBehavior() InterruptBehavior            { return InterruptCancel }
func (c *customTool) Execute(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
	return ToolResult{}, fmt.Errorf("custom tool '%s' has no handler", c.name)
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./cmd/celeste/tools/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/registry.go cmd/celeste/tools/registry_test.go
git commit -m "feat(tools): add Registry with mode filtering and custom tool loading"
```

---

### Task 4: Dev Tools Migration (bash, read_file, write_file, patch_file, list_files, search)

**Files:**
- Create: `cmd/celeste/tools/builtin/bash.go`, `read_file.go`, `write_file.go`, `patch_file.go`, `list_files.go`, `search.go`
- Test: `cmd/celeste/tools/builtin/bash_test.go`, `read_file_test.go`

This task migrates the 6 dev tools from `agent/dev_skills.go` into individual `tools/builtin/` files. Each tool implements the `Tool` interface. The business logic (path traversal prevention, output truncation, etc.) is preserved exactly from the original.

- [ ] **Step 1: Write test for bash tool**

```go
// cmd/celeste/tools/builtin/bash_test.go
package builtin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBashTool_Name(t *testing.T) {
	tool := NewBashTool("/tmp")
	assert.Equal(t, "bash", tool.Name())
	assert.False(t, tool.IsReadOnly())
	assert.False(t, tool.IsConcurrencySafe(nil))
}

func TestBashTool_Execute(t *testing.T) {
	tool := NewBashTool(t.TempDir())
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "echo hello",
	}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "hello")
	assert.False(t, result.Error)
}

func TestBashTool_BlocksSudo(t *testing.T) {
	tool := NewBashTool(t.TempDir())
	result, err := tool.Execute(context.Background(), map[string]any{
		"command": "sudo rm -rf /",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "blocked")
}

func TestBashTool_RequiresCommand(t *testing.T) {
	tool := NewBashTool(t.TempDir())
	err := tool.ValidateInput(map[string]any{})
	assert.Error(t, err)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/celeste/tools/builtin/ -v -run TestBashTool`
Expected: FAIL

- [ ] **Step 3: Write bash.go**

Migrate the `dev_run_command` handler from `agent/dev_skills.go` lines 518-687 into a `BashTool` struct. Preserve:
- sudo/su blocking
- Output truncation at 12KB
- Configurable timeout (default 45s)
- Workspace-relative execution

```go
// cmd/celeste/tools/builtin/bash.go
package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

const maxCommandOutput = 12 * 1024 // 12KB

type BashTool struct {
	BaseTool
	workspace string
}

func NewBashTool(workspace string) *BashTool {
	return &BashTool{
		BaseTool: BaseTool{
			ToolName:        "bash",
			ToolDescription: "Execute a shell command in the workspace directory. Use for running tests, builds, git operations, and other CLI tasks.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"command": {
						"type": "string",
						"description": "The shell command to execute"
					},
					"timeout": {
						"type": "integer",
						"description": "Timeout in seconds (default 45)"
					}
				},
				"required": ["command"]
			}`),
			ReadOnly:        false,
			ConcurrencySafe: false,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"command"},
		},
		workspace: workspace,
	}
}

func (b *BashTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	command, _ := input["command"].(string)

	// Block sudo/su
	trimmed := strings.TrimSpace(command)
	if strings.HasPrefix(trimmed, "sudo ") || strings.HasPrefix(trimmed, "su ") ||
		strings.HasPrefix(trimmed, "sudo\t") || strings.HasPrefix(trimmed, "su\t") {
		return tools.ToolResult{
			Content: "Error: sudo/su commands are blocked for safety",
			Error:   true,
		}, nil
	}

	timeout := 45 * time.Second
	if t, ok := input["timeout"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, "sh", "-c", command)
	cmd.Dir = b.workspace

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: "bash",
			Message:  fmt.Sprintf("running: %s", truncateStr(command, 80)),
			Percent:  -1,
		}
	}

	err := cmd.Run()

	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	// Truncate output
	if len(output) > maxCommandOutput {
		output = output[:maxCommandOutput] + "\n... (output truncated)"
	}

	if err != nil {
		return tools.ToolResult{
			Content: fmt.Sprintf("Command failed: %v\n%s", err, output),
			Error:   true,
		}, nil
	}

	return tools.ToolResult{Content: output}, nil
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
```

- [ ] **Step 4: Write remaining dev tools (read_file, write_file, patch_file, list_files, search)**

Each file follows the same pattern as bash.go — migrate the handler logic from `agent/dev_skills.go` into a struct that embeds `BaseTool`. The key safety features to preserve:

- `read_file.go`: Max 200KB file reads, line range support, path traversal check
- `write_file.go`: Path traversal check, append mode support
- `patch_file.go`: old_string/new_string replacement, path traversal check
- `list_files.go`: Max 1000 entries, recursive option, `IsReadOnly=true`, `ConcurrencySafe=true`
- `search.go`: Regex pattern search, max results cap, `IsReadOnly=true`, `ConcurrencySafe=true`

Each tool constructor takes `workspace string` for path resolution.

- [ ] **Step 5: Write read_file_test.go**

```go
// cmd/celeste/tools/builtin/read_file_test.go
package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFileTool(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("line1\nline2\nline3\n"), 0644)
	require.NoError(t, err)

	tool := NewReadFileTool(dir)
	assert.Equal(t, "read_file", tool.Name())
	assert.True(t, tool.IsReadOnly())
	assert.True(t, tool.IsConcurrencySafe(nil))

	result, err := tool.Execute(context.Background(), map[string]any{
		"path": "test.txt",
	}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "line1")
	assert.Contains(t, result.Content, "line3")
}

func TestReadFileTool_PathTraversal(t *testing.T) {
	tool := NewReadFileTool(t.TempDir())
	result, err := tool.Execute(context.Background(), map[string]any{
		"path": "../../etc/passwd",
	}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "outside workspace")
}
```

- [ ] **Step 6: Run all tests**

Run: `go test ./cmd/celeste/tools/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add cmd/celeste/tools/builtin/
git commit -m "feat(tools): migrate dev tools to unified tool interface"
```

---

### Task 5: Skill Tools Migration

**Files:**
- Create: `cmd/celeste/tools/builtin/weather.go`, `tarot.go`, `crypto.go`, `media.go`, `currency.go`, `encoding.go`, `hash.go`, `uuid.go`, `password.go`, `reminder.go`, `notes.go`, `qrcode.go`, `twitch.go`, `youtube.go`, `collections.go`
- Create: `cmd/celeste/tools/builtin/register.go`
- Test: `cmd/celeste/tools/builtin/register_test.go`

This task migrates all 23 skills from `skills/builtin.go` and `skills/crypto*.go` into individual tool files under `tools/builtin/`. Each tool implements the `Tool` interface by embedding `BaseTool` and delegating execution to the existing handler logic.

**ConfigLoader dependency:** The existing `skills.ConfigLoader` interface is preserved. Skill tools that need config (weather, tarot, crypto, etc.) take a `ConfigLoader` in their constructor.

- [ ] **Step 1: Write register.go that wires everything**

```go
// cmd/celeste/tools/builtin/register.go
package builtin

import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// ConfigLoader provides skill-specific configuration.
// This interface matches the existing skills.ConfigLoader for backwards compatibility.
type ConfigLoader interface {
	GetWeatherConfig() (WeatherConfig, error)
	GetTarotConfig() (TarotConfig, error)
	GetVeniceConfig() (VeniceConfig, error)
	GetTwitchConfig() (TwitchConfig, error)
	GetYouTubeConfig() (YouTubeConfig, error)
	GetIPFSConfig() (IPFSConfig, error)
	GetAlchemyConfig() (AlchemyConfig, error)
	GetBlockmonConfig() (BlockmonConfig, error)
	GetWalletSecurityConfig() (WalletSecuritySettingsConfig, error)
}

// RegisterAll registers all built-in tools in the registry.
// workspace is the working directory for dev tools (can be empty for chat-only).
// configLoader provides skill-specific configuration.
func RegisterAll(registry *tools.Registry, workspace string, configLoader ConfigLoader) {
	// Dev tools — available in Agent, Claw, Chat modes
	if workspace != "" {
		registry.RegisterWithModes(NewBashTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewReadFileTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewWriteFileTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewPatchFileTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewListFilesTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
		registry.RegisterWithModes(NewSearchTool(workspace), tools.ModeAgent, tools.ModeClaw, tools.ModeChat)
	}

	// Skill tools — available in Chat and Claw modes
	if configLoader != nil {
		registry.RegisterWithModes(NewWeatherTool(configLoader), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewTarotTool(configLoader), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewCurrencyTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewBase64EncodeTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewBase64DecodeTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewHashTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewUUIDTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewPasswordTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewReminderSetTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewReminderListTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewNoteSaveTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewNoteGetTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewNoteListTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewQRCodeTool(), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewTwitchTool(configLoader), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewYouTubeTool(configLoader), tools.ModeChat, tools.ModeClaw)
		registry.RegisterWithModes(NewUpscaleImageTool(configLoader), tools.ModeChat, tools.ModeClaw)
		// Crypto tools
		RegisterCryptoTools(registry, configLoader)
	}
}

// RegisterReadOnlyDevTools registers only read-only dev tools (for reviewer agents).
func RegisterReadOnlyDevTools(registry *tools.Registry, workspace string) {
	registry.RegisterWithModes(NewReadFileTool(workspace), tools.ModeAgent)
	registry.RegisterWithModes(NewListFilesTool(workspace), tools.ModeAgent)
	registry.RegisterWithModes(NewSearchTool(workspace), tools.ModeAgent)
}

// RegisterCryptoTools registers crypto/blockchain tools.
func RegisterCryptoTools(registry *tools.Registry, configLoader ConfigLoader) {
	registry.RegisterWithModes(NewIPFSTool(configLoader), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewAlchemyTool(configLoader), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewBlockmonTool(configLoader), tools.ModeChat, tools.ModeClaw)
	registry.RegisterWithModes(NewWalletSecurityTool(configLoader), tools.ModeChat, tools.ModeClaw)
}
```

- [ ] **Step 2: Write register_test.go**

```go
// cmd/celeste/tools/builtin/register_test.go
package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestRegisterAll(t *testing.T) {
	registry := tools.NewRegistry()
	// Use nil configLoader to skip config-dependent tools for this test
	RegisterAll(registry, t.TempDir(), nil)

	// Should have 6 dev tools
	agentTools := registry.GetTools(tools.ModeAgent)
	assert.Equal(t, 6, len(agentTools))

	// Dev tools should be accessible by name
	bash, ok := registry.Get("bash")
	assert.True(t, ok)
	assert.Equal(t, "bash", bash.Name())

	readFile, ok := registry.Get("read_file")
	assert.True(t, ok)
	assert.True(t, readFile.IsReadOnly())
}
```

- [ ] **Step 3: Migrate each skill tool**

Each skill tool file follows this pattern (using weather as an example):

```go
// cmd/celeste/tools/builtin/weather.go
package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

type WeatherConfig struct {
	DefaultZipCode string `json:"default_zip_code"`
}

type WeatherTool struct {
	BaseTool
	configLoader ConfigLoader
}

func NewWeatherTool(configLoader ConfigLoader) *WeatherTool {
	return &WeatherTool{
		BaseTool: BaseTool{
			ToolName:        "get_weather",
			ToolDescription: "Get current weather for a US zip code",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"zip": {"type": "string", "description": "US zip code (5 digits)"}
				},
				"required": ["zip"]
			}`),
			ReadOnly:        true,
			ConcurrencySafe: true,
			RequiredFields:  []string{"zip"},
		},
		configLoader: configLoader,
	}
}

func (w *WeatherTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	// Migrate handler logic from skills/builtin.go WeatherHandler
	// ... (exact handler code from existing builtin.go)
	zip, _ := input["zip"].(string)
	_ = zip // placeholder — actual impl copies from existing handler
	return tools.ToolResult{Content: fmt.Sprintf("Weather for %s: ...", zip)}, nil
}
```

Each remaining skill tool follows this exact same pattern. The handler body is copied verbatim from `skills/builtin.go` — only the function signature wrapper changes.

The config type definitions (`WeatherConfig`, `TarotConfig`, `VeniceConfig`, etc.) are moved from `skills/builtin.go` to the respective tool files.

- [ ] **Step 4: Run all tests**

Run: `go test ./cmd/celeste/tools/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add cmd/celeste/tools/builtin/
git commit -m "feat(tools): migrate all 23 skills to unified tool interface"
```

---

### Task 6: Wire New Tool System Into LLM Client

**Files:**
- Modify: `cmd/celeste/llm/client.go`

- [ ] **Step 1: Update imports and fields**

In `cmd/celeste/llm/client.go`, replace:
```go
import "github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
```
with:
```go
import "github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
```

Replace field:
```go
registry *skills.Registry
```
with:
```go
registry *tools.Registry
```

- [ ] **Step 2: Update NewClient signature**

```go
func NewClient(config *Config, registry *tools.Registry) *Client {
```

- [ ] **Step 3: Update GetSkills**

Replace `GetSkills()` to delegate to `registry.GetToolDefinitions()`:
```go
func (c *Client) GetSkills() []tui.SkillDefinition {
	if c.registry == nil {
		return nil
	}
	toolDefs := c.registry.GetToolDefinitions()
	result := make([]tui.SkillDefinition, len(toolDefs))
	for i, def := range toolDefs {
		fn := def["function"].(map[string]any)
		result[i] = tui.SkillDefinition{
			Name:        fn["name"].(string),
			Description: fn["description"].(string),
			Parameters:  fn["parameters"],
		}
	}
	return result
}
```

- [ ] **Step 4: Update ExecuteSkill**

```go
func (c *Client) ExecuteSkill(ctx context.Context, name string, argsJSON string) (*tools.ToolResult, error) {
	var args map[string]any
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return nil, fmt.Errorf("failed to parse arguments: %w", err)
		}
	} else {
		args = make(map[string]any)
	}
	result, err := c.registry.Execute(ctx, name, args)
	return &result, err
}
```

- [ ] **Step 5: Run existing tests**

Run: `go test ./cmd/celeste/llm/ -v`
Expected: PASS (or fix compilation errors)

- [ ] **Step 6: Commit**

```bash
git add cmd/celeste/llm/client.go
git commit -m "refactor(llm): wire unified tool registry into LLM client"
```

---

### Task 7: Wire New Tool System Into main.go

**Files:**
- Modify: `cmd/celeste/main.go`

- [ ] **Step 1: Update imports**

Replace `skills` import with `tools` and `tools/builtin`:
```go
import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/builtin"
)
```

- [ ] **Step 2: Update runChatTUI**

In `runChatTUI()` (lines 188-371), replace skill initialization:
```go
// OLD:
// registry := skills.NewRegistry()
// registry.LoadSkills()
// skills.RegisterBuiltinSkills(registry, configLoader)

// NEW:
registry := tools.NewRegistry()
configLoader := config.NewConfigLoader(cfg)
builtin.RegisterAll(registry, "", configLoader) // empty workspace for chat mode
if err := registry.LoadCustomTools(filepath.Join(homeDir, ".celeste", "skills")); err != nil {
    fmt.Fprintf(os.Stderr, "Warning: failed to load custom tools: %v\n", err)
}
```

- [ ] **Step 3: Update TUIClientAdapter**

Change `registry *skills.Registry` to `registry *tools.Registry` and update `ExecuteSkill` to use `tools.ToolResult`.

- [ ] **Step 4: Update agent command initialization**

In `runAgentCommand()`, use `builtin.RegisterAll(registry, workspace, configLoader)` instead of `skills.RegisterBuiltinSkills` + `agent.RegisterDevSkills`.

- [ ] **Step 5: Build and test**

Run: `go build ./cmd/celeste/`
Expected: Compiles successfully

- [ ] **Step 6: Commit**

```bash
git add cmd/celeste/main.go
git commit -m "refactor(main): switch from skills to unified tools package"
```

---

### Task 8: Wire New Tool System Into Agent Runtime

**Files:**
- Modify: `cmd/celeste/agent/runtime.go`

- [ ] **Step 1: Update Runner to use tools.Registry**

Replace `registry *skills.Registry` with `registry *tools.Registry`. Update `NewRunner()` signature.

- [ ] **Step 2: Update executeToolCall**

The existing `executeToolCall` calls `r.client.ExecuteSkill()`. Update it to use the new `tools.ToolResult` type returned by the updated LLM client.

- [ ] **Step 3: Remove dev_skills.go**

Delete `cmd/celeste/agent/dev_skills.go` — its functionality is now in `tools/builtin/`.

- [ ] **Step 4: Update dev_skills_test.go**

Update test imports to use `tools/builtin` instead. Or delete the test file if the tests have been ported to `tools/builtin/bash_test.go` etc.

- [ ] **Step 5: Build and run existing agent tests**

Run: `go test ./cmd/celeste/agent/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/celeste/agent/ cmd/celeste/tools/
git commit -m "refactor(agent): remove dev_skills, use unified tools package"
```

---

### Task 9: Delete Old Skills Package

**Files:**
- Delete: `cmd/celeste/skills/` (entire directory)

- [ ] **Step 1: Verify no remaining imports**

Run: `grep -r '"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"' cmd/`
Expected: No results

- [ ] **Step 2: Delete the package**

```bash
rm -rf cmd/celeste/skills/
```

- [ ] **Step 3: Build and run all tests**

Run: `go build ./cmd/celeste/ && go test ./cmd/celeste/... -v`
Expected: Compiles and all tests pass

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "refactor: remove old skills package — replaced by tools/"
```

---

### Task 10: Final Verification

- [ ] **Step 1: Run full test suite**

Run: `go test -race ./cmd/celeste/... -v`
Expected: All tests pass

- [ ] **Step 2: Run linter**

Run: `go vet ./cmd/celeste/...`
Expected: No issues

- [ ] **Step 3: Verify build for all platforms**

Run: `GOOS=linux GOARCH=amd64 go build ./cmd/celeste/ && GOOS=darwin GOARCH=arm64 go build ./cmd/celeste/`
Expected: Both compile

- [ ] **Step 4: Commit any remaining fixes**

```bash
git add -A
git commit -m "chore: plan 1 complete — unified tool layer verified"
```
