package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/shellrun"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/permissions"
)

// PermissionRequest describes a pending tool invocation that requires user approval.
// It is passed to PromptFunc when the permission checker returns Ask.
type PermissionRequest struct {
	ToolName     string
	InputSummary string // short human-readable summary of what the tool will do
	RiskLevel    string // "read", "write", or "destructive"
	// Forced is set when a PreToolUse hook or an AskAdvisor forced this
	// ask over a policy Allow: a prompt must ask the user, never answer
	// from a remembered "always" of its own.
	Forced bool
	// Context is the asking call's context (loop.PromptGate sets it): its
	// Done and values tell an interactive prompt which run asked and
	// whether that run has ended (2.0 F2e). nil: unknown.
	Context context.Context
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

// PreToolHookResult is the combined verdict of the PreToolUse hooks.
type PreToolHookResult struct {
	Decision          string // "allow", "deny" or "ask"
	Reason            string
	AdditionalContext string
	UpdatedInput      map[string]any // nil = unchanged
}

// HookRunner runs tool hooks (2.0 F0). It is an interface so tools does
// not import hooks; hooks.Runner.ToolHooks returns one.
type HookRunner interface {
	PreToolUse(ctx context.Context, toolName string, input map[string]any) PreToolHookResult
	PostToolUse(ctx context.Context, toolName string, input map[string]any, result ToolResult) (additionalContext string)
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

	// Dynamic tool discovery (opt-in via SetDiscoveryMode).
	hidden        map[string]bool // tools hidden from the prompt until activated
	activated     map[string]bool // tools re-activated this session by find_tools
	discoveryMode bool            // when false, hidden/activated are ignored

	// overwritten lists names Register/RegisterWithModes replaced with a
	// different tool (builtins never should; TestBuiltinNamesAreUnique).
	overwritten []string
}

// NewRegistry creates a new empty tool registry.
func NewRegistry() *Registry {
	return &Registry{
		tools:     make(map[string]Tool),
		modes:     make(map[string][]RuntimeMode),
		hidden:    make(map[string]bool),
		activated: make(map[string]bool),
	}
}

// Register adds a tool that is available in all modes. It is for builtins,
// whose names the code controls; an existing tool of that name is replaced.
// External tools (MCP servers, JSON custom tools) use Add.
func (r *Registry) Register(tool Tool) {
	r.RegisterWithModes(tool)
}

// RegisterWithModes adds a tool that is only available in the specified
// modes (none: all modes). Like Register, it replaces a tool of that name.
func (r *Registry) RegisterWithModes(tool Tool, modes ...RuntimeMode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.tools[tool.Name()]; ok && !sameTool(old, tool) {
		r.overwritten = append(r.overwritten, tool.Name())
	}
	r.put(tool, modes)
}

// ErrToolNameTaken is returned by Add when another tool holds the name.
var ErrToolNameTaken = errors.New("tool name already registered")

// Add registers an external tool (an MCP server's, a JSON custom tool)
// without ever replacing another (2.0 W4): a name held by a different tool
// is ErrToolNameTaken, wrapped with the name. Re-adding the very same tool
// (a reconnect) is a no-op success. Modes as RegisterWithModes.
func (r *Registry) Add(tool Tool, modes ...RuntimeMode) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if old, ok := r.tools[tool.Name()]; ok && !sameTool(old, tool) {
		return fmt.Errorf("%w: %s", ErrToolNameTaken, tool.Name())
	}
	r.put(tool, modes)
	return nil
}

// put stores tool; the caller holds r.mu.
func (r *Registry) put(tool Tool, modes []RuntimeMode) {
	if len(modes) == 0 {
		modes = nil // nil means all modes
	}
	r.tools[tool.Name()] = tool
	r.modes[tool.Name()] = modes
}

// Overwritten lists the names Register or RegisterWithModes replaced with a
// different tool, in order; builtins must never do that.
func (r *Registry) Overwritten() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.overwritten)
}

// sameTool reports whether a and b are the same tool. == on two values of
// a type that is not comparable (a struct holding a slice or map) panics;
// such values count as different.
func sameTool(a, b Tool) (same bool) {
	defer func() {
		if recover() != nil {
			same = false
		}
	}()
	return a == b
}

// Unregister removes a tool by name. No-op if the tool is not present.
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tools, name)
	delete(r.modes, name)
}

// Retain removes every tool keep rejects and returns how many it removed.
// A nil keep removes nothing. A typed subagent (2.0 W4e) trims its own
// registry with it; the tools' hidden/activated marks go with them.
func (r *Registry) Retain(keep func(Tool) bool) int {
	if keep == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for name, t := range r.tools {
		if keep(t) {
			continue
		}
		delete(r.tools, name)
		delete(r.modes, name)
		delete(r.hidden, name)
		delete(r.activated, name)
		n++
	}
	return n
}

// UnregisterByPrefix removes every tool whose name starts with prefix and
// returns the count removed. Used to drop all tools an MCP server contributed.
func (r *Registry) UnregisterByPrefix(prefix string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for name := range r.tools {
		if strings.HasPrefix(name, prefix) {
			delete(r.tools, name)
			delete(r.modes, name)
			n++
		}
	}
	return n
}

// SetDiscoveryMode toggles dynamic tool discovery. When off (default), the
// hidden/activated maps are ignored and every registered tool is offered.
func (r *Registry) SetDiscoveryMode(on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.discoveryMode = on
}

// DiscoveryMode reports whether dynamic tool discovery is on.
func (r *Registry) DiscoveryMode() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.discoveryMode
}

// SetHidden marks a tool as hidden-until-activated. Only takes effect while
// discovery mode is on. find_tools itself must never be hidden.
func (r *Registry) SetHidden(name string, hidden bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if hidden {
		r.hidden[name] = true
	} else {
		delete(r.hidden, name)
	}
}

// Activate re-exposes hidden tools for the rest of the session (called by
// find_tools after a BM25 match).
func (r *Registry) Activate(names ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range names {
		r.activated[n] = true
	}
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
		if r.discoveryMode && r.hidden[name] && !r.activated[name] {
			continue // hidden until find_tools activates it
		}
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

type execTimeoutKey struct{}

// WithExecTimeout asks the registry to bound the tool's execution with
// timeout, starting only once the tool is cleared to run. A deadline set on
// ctx directly also covers the time spent waiting on the user's approval, so
// a slow answer hands the tool an already-expired context (#172).
func WithExecTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, execTimeoutKey{}, timeout)
}

type promptKey struct{}

// WithPrompt makes fn answer this call's permission Ask instead of the
// registry's prompt. loop.Loop uses it so each run brings its own Gate,
// whose lifetime differs by mode (2.0 F2). A nil fn means no one to ask,
// the same as WithoutPrompt: it never falls back to the registry's prompt.
func WithPrompt(ctx context.Context, fn PromptFunc) context.Context {
	if fn == nil {
		return WithoutPrompt(ctx)
	}
	return context.WithValue(ctx, promptKey{}, fn)
}

// AdvisedCall is a call the permission policy allows, shown to an
// AskAdvisor.
type AdvisedCall struct {
	Name     string
	Input    map[string]any
	ReadOnly bool
}

// AskAdvisor may turn a call the policy allows into an Ask (jev_gate, 2.0
// W3). It can never allow a call or deny one outright: Deny stays Deny,
// and an Ask it adds goes to the prompt, or is denied headless. reason is
// shown with the prompt and in a headless denial.
type AskAdvisor func(ctx context.Context, call AdvisedCall) (ask bool, reason string)

type advisorKey struct{}

// WithAskAdvisor makes a consult this call's allowed decision. loop.Loop
// sets it from Loop.Advisor.
func WithAskAdvisor(ctx context.Context, a AskAdvisor) context.Context {
	return context.WithValue(ctx, advisorKey{}, a)
}

// noPrompt marks a call with no one to ask (see WithoutPrompt).
type noPrompt struct{}

// WithoutPrompt makes this call's permission Ask deny as headless ("no
// prompt is configured"), ignoring the registry's own prompt. loop.Loop uses
// it for a run without a Gate. It shares WithPrompt's key, so the innermost
// of the two wins (a subagent's run inside a gated parent, or the reverse).
func WithoutPrompt(ctx context.Context) context.Context {
	return context.WithValue(ctx, promptKey{}, noPrompt{})
}

// Execute runs a tool by name with input validation.
func (r *Registry) Execute(ctx context.Context, name string, input map[string]any) (ToolResult, error) {
	return r.ExecuteWithProgress(ctx, name, input, nil)
}

// ExecuteWithProgress runs a tool by name with input validation and a progress channel.
//
// Order: validate → deny-only permission pass on the model's input (a hard
// denial never reaches a hook process) → PreToolUse hooks → re-validate a
// rewritten input → full permission gate on the final input (a hook's ask
// forces the prompt; a hook can never skip one) → execute → PostToolUse.
func (r *Registry) ExecuteWithProgress(ctx context.Context, name string, input map[string]any, progress chan<- ProgressEvent) (ToolResult, error) {
	tool, ok := r.Get(name)
	if !ok {
		return ToolResult{}, fmt.Errorf("tool '%s' not found", name)
	}
	if err := tool.ValidateInput(input); err != nil {
		return ToolResult{Content: err.Error(), Error: true}, nil
	}

	r.mu.RLock()
	checker := r.checker
	prompt := r.promptFn
	hooks := r.hooks
	r.mu.RUnlock()

	switch v := ctx.Value(promptKey{}).(type) {
	case PromptFunc:
		prompt = v // never nil: WithPrompt stores nil as noPrompt
	case noPrompt:
		prompt = nil
	}

	hookCtx := ctx // hooks never inherit the tool's execution timeout
	var hookContext []string
	forceAsk := false
	if hooks != nil {
		if checker != nil {
			if res := checker.Check(&toolInfoAdapter{tool: tool}, input); res.Decision == permissions.Deny {
				return ToolResult{Content: fmt.Sprintf("Permission denied: %s", res.Reason), Error: true}, nil
			}
		}
		pre := hooks.PreToolUse(hookCtx, name, input)
		if pre.AdditionalContext != "" {
			hookContext = append(hookContext, pre.AdditionalContext)
		}
		switch pre.Decision {
		case "deny":
			msg := "Blocked by pre-tool hook"
			if pre.Reason != "" {
				msg += ": " + pre.Reason
			}
			return withHookContext(ToolResult{Content: msg, Error: true}, hookContext), nil
		case "ask":
			forceAsk = true
		}
		if pre.UpdatedInput != nil {
			if err := tool.ValidateInput(pre.UpdatedInput); err != nil {
				return ToolResult{Content: "Hook rewrote the input into an invalid one: " + err.Error(), Error: true}, nil
			}
			input = pre.UpdatedInput
		}
	}

	advice := ""
	if a, _ := ctx.Value(advisorKey{}).(AskAdvisor); a != nil && !forceAsk && allowed(tool, input, checker) {
		if ask, why := a(hookCtx, AdvisedCall{Name: name, Input: input, ReadOnly: tool.IsReadOnly()}); ask {
			forceAsk, advice = true, why
		}
	}

	if denied, blocked := r.checkPermission(tool, name, input, checker, prompt, forceAsk, advice); blocked {
		return withHookContext(denied, hookContext), nil
	}

	// Approved: start the execution timeout now.
	if timeout, ok := ctx.Value(execTimeoutKey{}).(time.Duration); ok && timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// A Go error from the tool itself is returned as-is: PostToolUse is not
	// run and the pre-hook's additionalContext is dropped, same as 1.x,
	// which had no post-execution hook step on this path either.
	result, err := tool.Execute(ctx, input, progress)
	if err != nil {
		return result, err
	}
	if hooks != nil {
		if c := hooks.PostToolUse(hookCtx, name, input, result); c != "" {
			hookContext = append(hookContext, c)
		}
	}
	return withHookContext(result, hookContext), nil
}

// allowed reports the policy's decision for the call is Allow.
func allowed(tool Tool, input map[string]any, checker *permissions.Checker) bool {
	return checker == nil || checker.Check(&toolInfoAdapter{tool: tool}, input).Decision == permissions.Allow
}

// checkPermission applies the permission gate. forceAsk (a PreToolUse hook
// said "ask", or an AskAdvisor did, giving advice) turns Allow into Ask;
// Deny always stays Deny. It returns the denial result and true when the
// call must not run.
func (r *Registry) checkPermission(tool Tool, name string, input map[string]any, checker *permissions.Checker, prompt PromptFunc, forceAsk bool, advice string) (ToolResult, bool) {
	decision, reason := permissions.Allow, ""
	if checker != nil {
		res := checker.Check(&toolInfoAdapter{tool: tool}, input)
		decision, reason = res.Decision, res.Reason
	}
	if decision == permissions.Deny {
		return ToolResult{Content: fmt.Sprintf("Permission denied: %s", reason), Error: true}, true
	}
	if decision != permissions.Ask && !forceAsk {
		return ToolResult{}, false
	}
	// Hard gate: with no prompt configured (headless), deny so the gate
	// can't be bypassed silently.
	if prompt == nil {
		if advice != "" {
			return ToolResult{
				Content: fmt.Sprintf("Permission denied: %s; interactive approval required for %q but no prompt is configured", advice, name),
				Error:   true,
			}, true
		}
		return ToolResult{
			Content: fmt.Sprintf("Permission denied: interactive approval required for %q but no prompt is configured", name),
			Error:   true,
		}, true
	}
	summary := inputSummary(input)
	if advice != "" {
		summary = "[" + advice + "] " + summary
	}
	// Runs in the tool-execution goroutine (off the Bubble Tea Update loop),
	// so blocking on the answer is safe.
	resp := prompt(PermissionRequest{ToolName: name, InputSummary: summary, RiskLevel: classifyRiskLevel(name), Forced: forceAsk})
	pattern := resp.Pattern
	if pattern == "" {
		pattern = name
	}
	switch resp.Decision {
	case "allow_once":
		return ToolResult{}, false
	case "always_allow":
		if checker != nil {
			_ = checker.AddPersistentAllow(permissions.Rule{ToolPattern: pattern, Decision: permissions.Allow})
		}
		return ToolResult{}, false
	case "deny", "always_deny":
		if resp.Decision == "always_deny" && checker != nil {
			_ = checker.AddPersistentDeny(permissions.Rule{ToolPattern: pattern, Decision: permissions.Deny})
		}
		return ToolResult{Content: fmt.Sprintf("Permission denied: user denied execution of %q", name), Error: true}, true
	default:
		// Empty or unknown decision → deny (safe default).
		return ToolResult{Content: fmt.Sprintf("Permission denied: no decision received for %q", name), Error: true}, true
	}
}

// withHookContext puts hooks' additionalContext in front of what the model
// sees. Result capping keeps the head of a large result, so prepending means
// the context survives the cap.
func withHookContext(res ToolResult, contexts []string) ToolResult {
	if len(contexts) == 0 {
		return res
	}
	res.Content = "<hook-context>\n" + strings.Join(contexts, "\n") + "\n</hook-context>\n\n" + res.Content
	return res
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

// Prompt returns the prompt SetPromptFunc installed, or nil. The chat's
// loop Gate reads it at ask time (2.0 F2d), so a prompt installed after the
// Gate was built (runChatTUI, tests) still answers.
func (r *Registry) Prompt() PromptFunc {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.promptFn
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

// customToolTimeout bounds one custom tool command; the caller's context
// may end it sooner.
const customToolTimeout = 2 * time.Minute

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

	// The command is user-authored (trusted, so no denylist), but the
	// model chooses when it runs: it still gets its own process group,
	// a timeout, a bounded pipe wait and an output cap.
	res := shellrun.Run(ctx, shellrun.Options{Command: c.command, Stdin: data, Timeout: customToolTimeout})
	var failure string
	switch {
	case res.Err != nil:
		failure = res.Err.Error()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		failure = "the caller's deadline ended it; the command and everything it started were killed"
	case res.TimedOut:
		failure = fmt.Sprintf("timed out after %s; the command and everything it started were killed", customToolTimeout)
	case ctx.Err() != nil:
		failure = "cancelled; the command and everything it started were killed"
	case res.ExitCode != 0:
		failure = fmt.Sprintf("exit status %d", res.ExitCode)
	}
	output := res.Output
	if res.Truncated {
		output += fmt.Sprintf("\n[output truncated at %d bytes]", shellrun.DefaultMaxOutput)
	}
	if failure != "" {
		return ToolResult{Content: fmt.Sprintf("Command '%s' failed: %s\nOutput:\n%s", c.command, failure, output), Error: true}, nil
	}
	return ToolResult{Content: output}, nil
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

	var taken []error
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

		// Add, not Register: a custom tool never replaces a builtin or
		// another custom tool (2.0 W4); the rest of the directory loads.
		if err := r.Add(&customToolWrapper{
			name:        def.Name,
			description: def.Description,
			params:      def.Parameters,
			command:     def.Command,
		}); err != nil {
			taken = append(taken, fmt.Errorf("custom tool file %s not loaded: %w", path, err))
		}
	}
	return errors.Join(taken...)
}
