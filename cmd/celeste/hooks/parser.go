// Package hooks provides pre/post tool-use hook parsing and execution.
package hooks

import (
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
)

// Hook represents a parsed hook definition ready for execution.
type Hook struct {
	Event   string // "PreToolUse", "PostToolUse", "PreCommit"
	Tool    string // tool name or "*"
	Command string // shell command with {{variables}}
	Timeout int    // seconds, default 30
}

// defaultHookTimeout is the default timeout in seconds.
const defaultHookTimeout = 30

// ParseFromGrimoire converts grimoire HookEntry items into Hook structs.
func ParseFromGrimoire(g *grimoire.Grimoire) []Hook {
	if g == nil {
		return nil
	}

	hooks := make([]Hook, 0, len(g.Hooks))
	for _, entry := range g.Hooks {
		tool := entry.ToolName
		if tool == "" {
			tool = "*"
		}
		hooks = append(hooks, Hook{
			Event:   entry.Phase,
			Tool:    tool,
			Command: entry.Command,
			Timeout: defaultHookTimeout,
		})
	}
	return hooks
}
