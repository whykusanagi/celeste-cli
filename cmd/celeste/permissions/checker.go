// cmd/celeste/permissions/checker.go
package permissions

import "fmt"

// ToolInfo is the minimal interface the Checker needs from a tool.
// This avoids importing the tools package (preventing circular dependencies).
// The tools.Tool interface satisfies this via its Name() and IsReadOnly() methods.
type ToolInfo interface {
	ToolName() string
	IsReadOnly() bool
}

// Checker evaluates whether a tool execution should be allowed, denied, or
// requires user approval. It implements a 5-step evaluation chain:
//
//  1. alwaysDeny rules — if any match, return Deny immediately
//  2. alwaysAllow rules — if any match, return Allow immediately
//  3. IsReadOnly check — in default mode, read-only tools are auto-allowed
//  4. patternRules — if any match, return the rule's decision
//  5. Mode fallthrough — default asks for writes, strict asks for all, trust allows all
type Checker struct {
	alwaysDeny   []Rule
	alwaysAllow  []Rule
	patternRules []Rule
	mode         PermissionMode
}

// NewChecker creates a Checker from a PermissionConfig.
func NewChecker(config PermissionConfig) *Checker {
	mode := config.Mode
	if !mode.Valid() {
		mode = ModeDefault
	}

	return &Checker{
		alwaysDeny:   config.AlwaysDeny,
		alwaysAllow:  config.AlwaysAllow,
		patternRules: config.PatternRules,
		mode:         mode,
	}
}

// Check evaluates whether the given tool invocation is permitted.
//
// The tool parameter provides tool metadata (name, read-only status).
// The input parameter contains the tool's input arguments.
//
// If tool is nil, it is treated as a non-read-only tool with an empty name.
func (c *Checker) Check(tool ToolInfo, input map[string]any) CheckResult {
	toolName := ""
	readOnly := false
	if tool != nil {
		toolName = tool.ToolName()
		readOnly = tool.IsReadOnly()
	}

	// Step 1: alwaysDeny rules (highest priority)
	for i := range c.alwaysDeny {
		if MatchRule(c.alwaysDeny[i], toolName, input) {
			return CheckResult{
				Decision:    Deny,
				MatchedRule: &c.alwaysDeny[i],
				Reason:      fmt.Sprintf("blocked by always-deny rule: %s", c.alwaysDeny[i].ToolPattern),
			}
		}
	}

	// Step 2: alwaysAllow rules
	for i := range c.alwaysAllow {
		if MatchRule(c.alwaysAllow[i], toolName, input) {
			return CheckResult{
				Decision:    Allow,
				MatchedRule: &c.alwaysAllow[i],
				Reason:      fmt.Sprintf("permitted by always-allow rule: %s", c.alwaysAllow[i].ToolPattern),
			}
		}
	}

	// Step 3: IsReadOnly check (only in default mode)
	if c.mode == ModeDefault && readOnly {
		return CheckResult{
			Decision: Allow,
			Reason:   fmt.Sprintf("read-only tool %q auto-allowed in default mode", toolName),
		}
	}

	// Step 4: Pattern rules
	for i := range c.patternRules {
		if MatchRule(c.patternRules[i], toolName, input) {
			return CheckResult{
				Decision:    c.patternRules[i].Decision,
				MatchedRule: &c.patternRules[i],
				Reason:      fmt.Sprintf("matched pattern rule: %s", c.patternRules[i].ToolPattern),
			}
		}
	}

	// Step 5: Mode fallthrough
	switch c.mode {
	case ModeTrust:
		return CheckResult{
			Decision: Allow,
			Reason:   fmt.Sprintf("trust mode: auto-allowing %q", toolName),
		}
	case ModeStrict:
		return CheckResult{
			Decision: Ask,
			Reason:   fmt.Sprintf("strict mode: asking for %q", toolName),
		}
	default: // ModeDefault
		if readOnly {
			return CheckResult{
				Decision: Allow,
				Reason:   fmt.Sprintf("read-only tool %q auto-allowed in default mode", toolName),
			}
		}
		return CheckResult{
			Decision: Ask,
			Reason:   fmt.Sprintf("default mode: asking for non-read-only tool %q", toolName),
		}
	}
}

// Mode returns the current permission mode.
func (c *Checker) Mode() PermissionMode {
	return c.mode
}
