// cmd/celeste/permissions/config.go
package permissions

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// PermissionConfig holds the persistent permission configuration.
// Stored at ~/.celeste/permissions.json.
type PermissionConfig struct {
	// Mode is the default permission mode (default, strict, trust).
	Mode PermissionMode `json:"mode"`

	// AlwaysAllow is a list of rules that are always permitted regardless of mode.
	AlwaysAllow []Rule `json:"always_allow,omitempty"`

	// AlwaysDeny is a list of rules that are always blocked regardless of mode.
	AlwaysDeny []Rule `json:"always_deny,omitempty"`

	// PatternRules are evaluated after alwaysDeny and alwaysAllow but before
	// mode fallthrough. They allow fine-grained control over specific tool/input
	// combinations.
	PatternRules []Rule `json:"pattern_rules,omitempty"`
}

// configJSON is the on-disk JSON representation. We use a separate struct
// to control serialization (e.g., Decision is stored as string, not int).
type configJSON struct {
	Mode         string     `json:"mode"`
	AlwaysAllow  []ruleJSON `json:"always_allow,omitempty"`
	AlwaysDeny   []ruleJSON `json:"always_deny,omitempty"`
	PatternRules []ruleJSON `json:"pattern_rules,omitempty"`
}

type ruleJSON struct {
	ToolPattern  string `json:"tool_pattern"`
	InputPattern string `json:"input_pattern,omitempty"`
	Decision     string `json:"decision"`
}

// DefaultConfig returns a PermissionConfig with sensible defaults:
//   - Mode: default (auto-allow reads, ask for writes)
//   - AlwaysDeny: sudo and su commands
//   - AlwaysAllow: read_file, list_files, search_files
func DefaultConfig() PermissionConfig {
	return PermissionConfig{
		Mode: ModeDefault,
		AlwaysAllow: []Rule{
			{ToolPattern: "read_file", Decision: Allow},
			{ToolPattern: "list_files", Decision: Allow},
			{ToolPattern: "search_files", Decision: Allow},
		},
		AlwaysDeny: []Rule{
			{ToolPattern: "bash(sudo *)", Decision: Deny},
			{ToolPattern: "bash(su *)", Decision: Deny},
		},
	}
}

// DefaultConfigPath returns the default path for the permissions config file.
func DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".celeste", "permissions.json")
}

// LoadConfig reads a PermissionConfig from disk. If the file does not exist,
// it returns DefaultConfig() without error. If the file exists but contains
// invalid JSON or an unrecognized mode, it returns an error.
func LoadConfig(path string) (*PermissionConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := DefaultConfig()
			return &cfg, nil
		}
		return nil, fmt.Errorf("read permissions config: %w", err)
	}

	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse permissions config: %w", err)
	}

	// Validate and parse mode
	mode := ModeDefault
	if raw.Mode != "" {
		parsed, err := ParsePermissionMode(raw.Mode)
		if err != nil {
			return nil, fmt.Errorf("permissions config: %w", err)
		}
		mode = parsed
	}

	cfg := &PermissionConfig{
		Mode:         mode,
		AlwaysAllow:  convertRulesFromJSON(raw.AlwaysAllow, Allow),
		AlwaysDeny:   convertRulesFromJSON(raw.AlwaysDeny, Deny),
		PatternRules: convertRulesFromJSON(raw.PatternRules, Ask),
	}

	return cfg, nil
}

// SaveConfig writes a PermissionConfig to disk as formatted JSON.
// Parent directories are created if they don't exist.
func SaveConfig(path string, config *PermissionConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}

	raw := configJSON{
		Mode:         config.Mode.String(),
		AlwaysAllow:  convertRulesToJSON(config.AlwaysAllow),
		AlwaysDeny:   convertRulesToJSON(config.AlwaysDeny),
		PatternRules: convertRulesToJSON(config.PatternRules),
	}

	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal permissions config: %w", err)
	}

	// Append newline for POSIX compliance
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write permissions config: %w", err)
	}

	return nil
}

// convertRulesFromJSON converts JSON rule representations to Rule structs.
// defaultDecision is used when the rule's decision field is empty.
func convertRulesFromJSON(jsonRules []ruleJSON, defaultDecision Decision) []Rule {
	if len(jsonRules) == 0 {
		return nil
	}

	rules := make([]Rule, len(jsonRules))
	for i, jr := range jsonRules {
		decision := defaultDecision
		switch jr.Decision {
		case "allow":
			decision = Allow
		case "deny":
			decision = Deny
		case "ask":
			decision = Ask
		}

		rules[i] = Rule{
			ToolPattern:  jr.ToolPattern,
			InputPattern: jr.InputPattern,
			Decision:     decision,
		}
	}
	return rules
}

// convertRulesToJSON converts Rule structs to JSON representations.
func convertRulesToJSON(rules []Rule) []ruleJSON {
	if len(rules) == 0 {
		return nil
	}

	jsonRules := make([]ruleJSON, len(rules))
	for i, r := range rules {
		jsonRules[i] = ruleJSON{
			ToolPattern:  r.ToolPattern,
			InputPattern: r.InputPattern,
			Decision:     r.Decision.String(),
		}
	}
	return jsonRules
}
