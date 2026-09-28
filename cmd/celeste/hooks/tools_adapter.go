package hooks

import (
	"context"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// ToolHooks adapts r for tools.Registry.SetHookRunner. It is nil when no
// PreToolUse or PostToolUse hook is loaded, so the registry skips hook work.
func (r *Runner) ToolHooks() tools.HookRunner {
	if !r.Has(EventPreToolUse) && !r.Has(EventPostToolUse) {
		return nil
	}
	return toolHooks{r}
}

type toolHooks struct{ r *Runner }

func (t toolHooks) PreToolUse(ctx context.Context, name string, input map[string]any) tools.PreToolHookResult {
	o := t.r.PreToolUse(ctx, name, input)
	return tools.PreToolHookResult{Decision: string(o.Decision), Reason: o.Reason, AdditionalContext: o.AdditionalContext, UpdatedInput: o.UpdatedInput}
}

func (t toolHooks) PostToolUse(ctx context.Context, name string, input map[string]any, res tools.ToolResult) string {
	return t.r.PostToolUse(ctx, name, input, ToolResponse{Content: res.Content, Error: res.Error}).AdditionalContext
}
