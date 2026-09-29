// cmd/celeste/permissions/checker.go
package permissions

import (
	"fmt"
	"os"
	"sync"
)

// ToolInfo is the minimal interface the Checker needs from a tool.
// This avoids importing the tools package (preventing circular dependencies).
// The tools.Tool interface satisfies this via its Name() and IsReadOnly() methods.
type ToolInfo interface {
	ToolName() string
	IsReadOnly() bool
}

// Checker evaluates whether a tool execution should be allowed, denied, or
// requires user approval. It implements a 6-step evaluation chain:
//
//  0. protected hook/trust files — best-effort Deny for shell commands
//  1. alwaysDeny rules — if any match, return Deny immediately
//  2. alwaysAllow rules — if any match, return Allow immediately
//  3. IsReadOnly check — in default mode, read-only tools are auto-allowed
//  4. patternRules — if any match, return the rule's decision
//  5. Mode fallthrough — default asks for writes, strict asks for all, trust allows all
type Checker struct {
	mu           sync.RWMutex
	alwaysDeny   []Rule
	alwaysAllow  []Rule
	patternRules []Rule
	mode         PermissionMode
	configPath   string   // path to persist rule additions; empty = no persistence
	protected    []string // spellings of hook/trust files tools may not modify (protected.go)
}

// NewChecker creates a Checker from a PermissionConfig.
func NewChecker(config PermissionConfig) *Checker {
	mode := config.Mode
	if !mode.Valid() {
		mode = ModeDefault
	}
	home, _ := os.UserHomeDir()

	return &Checker{
		alwaysDeny:   config.AlwaysDeny,
		alwaysAllow:  config.AlwaysAllow,
		patternRules: config.PatternRules,
		mode:         mode,
		protected:    protectedFragments(home),
	}
}

// SetConfigPath sets the file path used to persist rule additions from
// interactive prompts (always_allow / always_deny decisions).
// If unset, AddPersistentAllow and AddPersistentDeny still update the
// in-memory rule lists but skip disk writes.
func (c *Checker) SetConfigPath(path string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.configPath = path
}

// AddPersistentAllow appends an always-allow rule and, if a config path is
// set, adds it to the config on disk (see persistRule).
// The in-memory mutation happens under the lock; disk I/O happens after
// releasing it so that concurrent Check() calls are not blocked.
func (c *Checker) AddPersistentAllow(rule Rule) error {
	return c.addPersistent(rule, Allow)
}

// AddPersistentDeny appends an always-deny rule and, if a config path is
// set, adds it to the config on disk (see persistRule).
func (c *Checker) AddPersistentDeny(rule Rule) error {
	return c.addPersistent(rule, Deny)
}

func (c *Checker) addPersistent(rule Rule, d Decision) error {
	c.mu.Lock()
	rule.Decision = d
	if d == Deny {
		c.alwaysDeny = append(c.alwaysDeny, rule)
	} else {
		c.alwaysAllow = append(c.alwaysAllow, rule)
	}
	cfg, path := c.snapshotConfigLocked()
	c.mu.Unlock()
	if path == "" {
		return nil
	}
	return persistRule(path, rule, cfg)
}

// persistMu serializes the load-modify-save in persistRule within the
// process.
var persistMu sync.Mutex

// persistRule adds rule to the config at path. Several checkers persist to
// one file (the chat's, each /agent run's and /orchestrate lane's), so it
// re-reads the file and adds the rule there, keeping rules the others saved
// since this checker loaded it, rather than writing this checker's snapshot
// over them. A file it cannot read is replaced by fallback, this checker's
// rules, as before.
func persistRule(path string, rule Rule, fallback PermissionConfig) error {
	persistMu.Lock()
	defer persistMu.Unlock()
	cfg, err := LoadConfig(path)
	if err != nil {
		return SaveConfig(path, &fallback)
	}
	list := &cfg.AlwaysAllow
	if rule.Decision == Deny {
		list = &cfg.AlwaysDeny
	}
	for _, r := range *list {
		if r.ToolPattern == rule.ToolPattern && r.InputPattern == rule.InputPattern {
			return nil
		}
	}
	*list = append(*list, rule)
	return SaveConfig(path, cfg)
}

// snapshotConfigLocked returns a copy of the current PermissionConfig and the
// configPath. Must be called with mu held.
func (c *Checker) snapshotConfigLocked() (PermissionConfig, string) {
	cfg := PermissionConfig{
		Mode:         c.mode,
		AlwaysAllow:  append([]Rule(nil), c.alwaysAllow...),
		AlwaysDeny:   append([]Rule(nil), c.alwaysDeny...),
		PatternRules: append([]Rule(nil), c.patternRules...),
	}
	return cfg, c.configPath
}

// Check evaluates whether the given tool invocation is permitted.
//
// The tool parameter provides tool metadata (name, read-only status).
// The input parameter contains the tool's input arguments.
//
// If tool is nil, it is treated as a non-read-only tool with an empty name.
func (c *Checker) Check(tool ToolInfo, input map[string]any) CheckResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	toolName := ""
	readOnly := false
	if tool != nil {
		toolName = tool.ToolName()
		readOnly = tool.IsReadOnly()
	}

	// Step 0: best-effort defence in depth. A model-authored shell command
	// naming the hook/trust files is refused before it reaches a shell (2.0
	// F0 fix round 1). This is a literal substring match on unparsed text --
	// it can't catch every spelling a shell would still expand to the same
	// file (`..`, quoting, indirection) -- and it applies only to bash's
	// `command` argument, never to another tool's arguments or file
	// contents (a doc that merely mentions one of these paths must not be
	// denied). It is not the trust boundary: the real enforcement for file
	// tools is tools/builtin's resolvePath.
	if !readOnly {
		if cmd, ok := input["command"].(string); ok {
			if frag, hit := commandNamesProtectedFile(cmd, c.protected); hit {
				return CheckResult{
					Decision: Deny,
					Reason:   fmt.Sprintf("%s is protected: hooks and hook trust are changed by you, not by tools", frag),
				}
			}
		}
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
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.mode
}
