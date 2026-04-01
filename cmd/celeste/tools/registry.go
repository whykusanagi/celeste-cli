package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
)

// toolInfoAdapter wraps a Tool to satisfy the permissions.ToolInfo interface.
// Tool has Name() while ToolInfo expects ToolName().
type toolInfoAdapter struct {
	tool Tool
}

func (a *toolInfoAdapter) ToolName() string { return a.tool.Name() }
func (a *toolInfoAdapter) IsReadOnly() bool { return a.tool.IsReadOnly() }

// Registry manages the collection of available tools and their mode associations.
type Registry struct {
	mu      sync.RWMutex
	tools   map[string]Tool
	modes   map[string][]RuntimeMode // tool name -> allowed modes (nil = all modes)
	checker *permissions.Checker     // optional, nil = allow all
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
	if r.checker != nil {
		result := r.checker.Check(&toolInfoAdapter{tool: tool}, input)
		switch result.Decision {
		case permissions.Deny:
			return ToolResult{
				Content: fmt.Sprintf("Permission denied: %s", result.Reason),
				Error:   true,
			}, nil
		case permissions.Ask:
			// For now, treat Ask as Allow (TUI prompt comes in Plan 6)
			// TODO: Plan 6 will add interactive permission prompts
		}
	}

	return tool.Execute(ctx, input, progress)
}

// SetPermissionChecker sets the permission checker used to gate tool execution.
// If checker is nil, all tools are allowed (default behavior).
func (r *Registry) SetPermissionChecker(checker *permissions.Checker) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checker = checker
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
	return ToolResult{Content: "custom tool execution not yet implemented"}, nil
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
