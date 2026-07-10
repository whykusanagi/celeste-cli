package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
)

// PermissionRequest describes a pending tool invocation that requires user approval.
// It is passed to PromptFunc when the permission checker returns Ask.
type PermissionRequest struct {
	ToolName     string
	InputSummary string // short human-readable summary of what the tool will do
	RiskLevel    string // "read", "write", or "destructive"
}

// PermissionResponse carries the user's decision from an interactive prompt.
type PermissionResponse struct {
	Decision string // "allow_once", "always_allow", "deny", "always_deny"
	Pattern  string // rule pattern for "always" decisions
}

// PromptFunc is a blocking callback invoked when a tool execution requires
// interactive approval. It runs in the tool-execution goroutine (off the Bubble
// Tea Update loop), so it may safely block until the user responds.
// Returning a zero-value PermissionResponse (empty Decision) is treated as deny.
type PromptFunc func(req PermissionRequest) PermissionResponse

// AskOption is one selectable choice presented by the ask tool.
type AskOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// AskRequest is a structured question the model asks the user mid-turn.
type AskRequest struct {
	Question    string      `json:"question"`
	Options     []AskOption `json:"options"`
	MultiSelect bool        `json:"multi_select"`
}

// AskResponse carries the user's selection.
type AskResponse struct {
	Selected  []string // chosen option labels
	Cancelled bool
}

// AskFunc is a blocking callback that presents an AskRequest and returns the
// user's answer. Installed only in interactive (TUI) mode; nil means headless,
// in which case Ask returns an error rather than deadlocking.
type AskFunc func(ctx context.Context, req AskRequest) (AskResponse, error)

// classifyRiskLevel returns "read", "write", or "destructive" for a tool.
// Called after the checker returns Ask (so the tool is not read-only and not
// in an always-allow list). We apply simple heuristics on the tool name.
func classifyRiskLevel(toolName string) string {
	lower := strings.ToLower(toolName)
	switch {
	case strings.Contains(lower, "delete") ||
		strings.Contains(lower, "remove") ||
		strings.Contains(lower, "drop") ||
		strings.Contains(lower, "destroy") ||
		strings.Contains(lower, "truncate") ||
		lower == "bash":
		return "destructive"
	default:
		return "write"
	}
}

// inputSummary produces a short (<80 char) human-readable summary of the tool input.
func inputSummary(input map[string]any) string {
	if len(input) == 0 {
		return "(no args)"
	}
	// Try priority keys first
	for _, key := range []string{"command", "path", "content", "pattern", "query"} {
		if v, ok := input[key]; ok {
			if s, ok := v.(string); ok {
				if len(s) > 60 {
					s = s[:57] + "..."
				}
				return s
			}
		}
	}
	// Fallback: JSON-encode with truncation
	b, err := json.Marshal(input)
	if err != nil {
		return "(args)"
	}
	s := string(b)
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

// toolInfoAdapter wraps a Tool to satisfy the permissions.ToolInfo interface.
// Tool has Name() while ToolInfo expects ToolName().
type toolInfoAdapter struct {
	tool Tool
}

func (a *toolInfoAdapter) ToolName() string { return a.tool.Name() }
func (a *toolInfoAdapter) IsReadOnly() bool { return a.tool.IsReadOnly() }

// HookResult is the outcome of a pre/post tool hook.
type HookResult struct {
	Decision string // "approve" or "block"
	Output   string
}

// HookRunner is an interface for running pre/post tool hooks.
// This avoids a circular dependency between tools and hooks packages.
type HookRunner interface {
	RunPreToolUse(toolName string, input map[string]any) (*HookResult, error)
	RunPostToolUse(toolName string, input map[string]any) (*HookResult, error)
}

// Registry manages the collection of available tools and their mode associations.
type Registry struct {
	mu       sync.RWMutex
	tools    map[string]Tool
	modes    map[string][]RuntimeMode // tool name -> allowed modes (nil = all modes)
	checker  *permissions.Checker     // optional, nil = allow all
	hooks    HookRunner               // optional, nil = no hooks
	promptFn PromptFunc               // optional; nil = deny on Ask
	askFn    AskFunc                  // optional; nil = Ask returns an error (headless)
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]Tool),
		modes: make(map[string][]RuntimeMode),
	}
}

// Register adds a tool that is available in all modes.
func (r *Registry) Register(tool Tool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
	r.modes[tool.Name()] = nil // nil means all modes
}

// RegisterWithModes adds a tool that is only available in the specified modes.
func (r *Registry) RegisterWithModes(tool Tool, modes ...RuntimeMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[tool.Name()] = tool
	r.modes[tool.Name()] = modes
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

// GetTools returns tools available for the given mode, sorted by name.
func (r *Registry) GetTools(mode RuntimeMode) []Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []Tool
	for name, t := range r.tools {
		modes := r.modes[name]
		if modes == nil {
			// nil means available in all modes
			result = append(result, t)
			continue
		}
		if slices.Contains(modes, mode) {
			result = append(result, t)
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
	return r.toolsToDefinitions(tools)
}

// GetToolDefinitionsForMode returns tools for the given mode in OpenAI function-calling format.
func (r *Registry) GetToolDefinitionsForMode(mode RuntimeMode) []map[string]any {
	tools := r.GetTools(mode)
	return r.toolsToDefinitions(tools)
}

func (r *Registry) toolsToDefinitions(tools []Tool) []map[string]any {
	defs := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		var params map[string]any
		if t.Parameters() != nil {
			_ = json.Unmarshal(t.Parameters(), &params)
		}
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		def := map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  params,
			},
		}
		defs = append(defs, def)
	}
	return defs
}

// Execute runs a tool by name with input validation.
func (r *Registry) Execute(ctx context.Context, name string, input map[string]any) (ToolResult, error) {
	return r.ExecuteWithProgress(ctx, name, input, nil)
}

// ExecuteWithProgress runs a tool by name with input validation and a progress channel.
func (r *Registry) ExecuteWithProgress(ctx context.Context, name string, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return ToolResult{}, fmt.Errorf("tool '%s' not found", name)
	}
	if err := tool.ValidateInput(input); err != nil {
		return ToolResult{Content: err.Error(), Error: true}, nil
	}

	// Permission check
	r.mu.RLock()
	checker := r.checker
	prompt := r.promptFn
	r.mu.RUnlock()

	if checker != nil {
		result := checker.Check(&toolInfoAdapter{tool: tool}, input)
		switch result.Decision {
		case permissions.Deny:
			return ToolResult{
				Content: fmt.Sprintf("Permission denied: %s", result.Reason),
				Error:   true,
			}, nil
		case permissions.Ask:
			// Hard permission gate: invoke the prompt callback if configured.
			// If no prompt is configured (headless / non-TUI), deny by default
			// so the gate cannot be silently bypassed.
			if prompt == nil {
				return ToolResult{
					Content: fmt.Sprintf("Permission denied: interactive approval required for %q but no prompt is configured", name),
					Error:   true,
				}, nil
			}
			// Build the request and block for the user's response.
			// This call runs inside a tea.Cmd goroutine (off the Bubble Tea
			// Update loop), so blocking here is safe.
			req := PermissionRequest{
				ToolName:     name,
				InputSummary: inputSummary(input),
				RiskLevel:    classifyRiskLevel(name),
			}
			resp := prompt(req)
			switch resp.Decision {
			case "allow_once":
				// Proceed; no rule persisted.
			case "always_allow":
				// Persist an allow rule for future invocations.
				pattern := resp.Pattern
				if pattern == "" {
					pattern = name
				}
				_ = checker.AddPersistentAllow(permissions.Rule{
					ToolPattern: pattern,
					Decision:    permissions.Allow,
				})
			case "deny", "always_deny":
				if resp.Decision == "always_deny" {
					pattern := resp.Pattern
					if pattern == "" {
						pattern = name
					}
					_ = checker.AddPersistentDeny(permissions.Rule{
						ToolPattern: pattern,
						Decision:    permissions.Deny,
					})
				}
				return ToolResult{
					Content: fmt.Sprintf("Permission denied: user denied execution of %q", name),
					Error:   true,
				}, nil
			default:
				// Empty or unknown decision → deny (safe default).
				return ToolResult{
					Content: fmt.Sprintf("Permission denied: no decision received for %q", name),
					Error:   true,
				}, nil
			}
		}
	}

	// Pre-tool hook check
	if r.hooks != nil {
		hookResult, hookErr := r.hooks.RunPreToolUse(name, input)
		if hookErr != nil {
			return ToolResult{Content: fmt.Sprintf("Hook error: %s", hookErr.Error()), Error: true}, nil
		}
		if hookResult != nil && hookResult.Decision == "block" {
			msg := "Blocked by pre-tool hook"
			if hookResult.Output != "" {
				msg = fmt.Sprintf("Blocked by pre-tool hook: %s", hookResult.Output)
			}
			return ToolResult{Content: msg, Error: true}, nil
		}
	}

	result, err := tool.Execute(ctx, input, progress)

	// Post-tool hook (fire-and-forget, does not block result)
	if r.hooks != nil && err == nil {
		_, hookErr := r.hooks.RunPostToolUse(name, input)
		if hookErr != nil {
			fmt.Fprintf(os.Stderr, "Post-tool hook failed for %q: %v\n", name, hookErr)
		}
	}

	return result, err
}

// SetPermissionChecker sets the permission checker used to gate tool execution.
// If checker is nil, all tools are allowed (default behavior).
func (r *Registry) SetPermissionChecker(checker *permissions.Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checker = checker
}

// SetHookRunner sets the hook runner used for pre/post tool hooks.
// If runner is nil, no hooks are executed (default behavior).
func (r *Registry) SetHookRunner(runner HookRunner) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hooks = runner
}

// SetPromptFunc configures the interactive permission prompt callback.
// When the permission checker returns Ask for a tool, the registry calls fn
// to get the user's decision. fn runs in the tool-execution goroutine (off
// the Bubble Tea Update loop), so it may safely block.
// If fn is nil (the default), any Ask decision is treated as Deny — the gate
// cannot be silently bypassed in headless or non-TUI contexts.
func (r *Registry) SetPromptFunc(fn PromptFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.promptFn = fn
}

// SetAskFunc installs the interactive ask callback (TUI-only).
func (r *Registry) SetAskFunc(fn AskFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.askFn = fn
}

// Ask presents a structured question to the user and blocks for the answer.
// Returns an error when no callback is installed (headless / one-shot / serve),
// so the caller can degrade gracefully instead of deadlocking.
func (r *Registry) Ask(ctx context.Context, req AskRequest) (AskResponse, error) {
	r.mu.RLock()
	fn := r.askFn
	r.mu.RUnlock()
	if fn == nil {
		return AskResponse{}, fmt.Errorf("interactive input unavailable in this context")
	}
	return fn(ctx, req)
}

// Count returns the number of registered tools.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// customToolWrapper wraps a JSON-defined custom tool.
type customToolWrapper struct {
	name        string
	description string
	params      json.RawMessage
	command     string
}

func (c *customToolWrapper) Name() string                                { return c.name }
func (c *customToolWrapper) Description() string                         { return c.description }
func (c *customToolWrapper) Parameters() json.RawMessage                 { return c.params }
func (c *customToolWrapper) IsConcurrencySafe(input map[string]any) bool { return false }
func (c *customToolWrapper) IsReadOnly() bool                            { return false }
func (c *customToolWrapper) ValidateInput(input map[string]any) error    { return nil }
func (c *customToolWrapper) InterruptBehavior() InterruptBehavior        { return InterruptCancel }
func (c *customToolWrapper) Execute(ctx context.Context, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
	if c.command == "" {
		return ToolResult{Content: "Custom tool schema loaded, but no 'command' field defined. Add 'command' (shell-executable) to ~/.celeste/skills/" + c.name + ".json for execution support."}, nil
	}

	data, err := json.Marshal(input)
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Failed to marshal input: %v", err), Error: true}, nil
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", c.command)
	cmd.Stdin = bytes.NewBuffer(data)

	output, err := cmd.Output()
	if err != nil {
		return ToolResult{Content: fmt.Sprintf("Command '%s' failed: %v\nOutput:\n%s", c.command, err, string(output)), Error: true}, nil
	}

	return ToolResult{Content: string(output)}, nil
}

// LoadCustomTools loads JSON tool definitions from a directory.
// This provides backwards compatibility with ~/.celeste/skills/*.json files.
func (r *Registry) LoadCustomTools(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // directory doesn't exist, nothing to load
		}
		return fmt.Errorf("reading custom tools directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading custom tool file %s: %w", path, err)
		}

		var def struct {
			Name        string          `json:"name"`
			Description string          `json:"description"`
			Parameters  json.RawMessage `json:"parameters"`
			Command     string          `json:"command"`
		}
		if err := json.Unmarshal(data, &def); err != nil {
			return fmt.Errorf("parsing custom tool file %s: %w", path, err)
		}

		if def.Name == "" {
			continue
		}

		r.Register(&customToolWrapper{
			name:        def.Name,
			description: def.Description,
			params:      def.Parameters,
			command:     def.Command,
		})
	}
	return nil
}
