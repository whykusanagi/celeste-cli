// cmd/celeste/permissions/rules_test.go
package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchRule_ExactToolName(t *testing.T) {
	rule := Rule{ToolPattern: "read_file", Decision: Allow}
	assert.True(t, MatchRule(rule, "read_file", nil))
	assert.False(t, MatchRule(rule, "write_file", nil))
	assert.False(t, MatchRule(rule, "READ_FILE", nil))
}

func TestMatchRule_WildcardTool(t *testing.T) {
	rule := Rule{ToolPattern: "*", Decision: Deny}
	assert.True(t, MatchRule(rule, "read_file", nil))
	assert.True(t, MatchRule(rule, "bash", nil))
	assert.True(t, MatchRule(rule, "anything", nil))
}

func TestMatchRule_ToolWithArgumentGlob(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		toolName string
		input    map[string]any
		want     bool
	}{
		{
			name:     "bash git star matches git status",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": "git status"},
			want:     true,
		},
		{
			name:     "bash git star matches git commit with flags",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": "git commit -m 'hello'"},
			want:     true,
		},
		{
			name:     "bash git star does not match ls",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": "ls -la"},
			want:     false,
		},
		{
			name:     "bash rm -rf star matches rm -rf /tmp",
			pattern:  "bash(rm -rf *)",
			toolName: "bash",
			input:    map[string]any{"command": "rm -rf /tmp/foo"},
			want:     true,
		},
		{
			name:     "bash rm -rf star does not match rm file",
			pattern:  "bash(rm -rf *)",
			toolName: "bash",
			input:    map[string]any{"command": "rm file.txt"},
			want:     false,
		},
		{
			name:     "bash sudo star matches sudo apt",
			pattern:  "bash(sudo *)",
			toolName: "bash",
			input:    map[string]any{"command": "sudo apt install vim"},
			want:     true,
		},
		{
			name:     "wrong tool name does not match",
			pattern:  "bash(git *)",
			toolName: "read_file",
			input:    map[string]any{"command": "git status"},
			want:     false,
		},
		{
			name:     "no input returns false for argument glob",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    nil,
			want:     false,
		},
		{
			name:     "empty command returns false",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": ""},
			want:     false,
		},
		{
			name:     "non-string command returns false",
			pattern:  "bash(git *)",
			toolName: "bash",
			input:    map[string]any{"command": 42},
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := Rule{ToolPattern: tt.pattern, Decision: Allow}
			assert.Equal(t, tt.want, MatchRule(rule, tt.toolName, tt.input))
		})
	}
}

func TestMatchRule_InputPattern(t *testing.T) {
	tests := []struct {
		name         string
		toolPattern  string
		inputPattern string
		toolName     string
		input        map[string]any
		want         bool
	}{
		{
			name:         "input pattern matches path field",
			toolPattern:  "read_file",
			inputPattern: "*/secret*",
			toolName:     "read_file",
			input:        map[string]any{"path": "/etc/secret.key"},
			want:         true,
		},
		{
			name:         "input pattern does not match",
			toolPattern:  "read_file",
			inputPattern: "*/secret*",
			toolName:     "read_file",
			input:        map[string]any{"path": "/etc/hosts"},
			want:         false,
		},
		{
			name:         "tool pattern fails so input pattern skipped",
			toolPattern:  "write_file",
			inputPattern: "*secret*",
			toolName:     "read_file",
			input:        map[string]any{"path": "/etc/secret.key"},
			want:         false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := Rule{ToolPattern: tt.toolPattern, InputPattern: tt.inputPattern, Decision: Allow}
			assert.Equal(t, tt.want, MatchRule(rule, tt.toolName, tt.input))
		})
	}
}

func TestParseToolPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		wantTool string
		wantArg  string
	}{
		{"bash(git *)", "bash", "git *"},
		{"read_file", "read_file", ""},
		{"*", "*", ""},
		{"bash(sudo *)", "bash", "sudo *"},
		{"bash(rm -rf *)", "bash", "rm -rf *"},
		{"bash()", "bash", ""},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			tool, arg := ParseToolPattern(tt.pattern)
			assert.Equal(t, tt.wantTool, tool)
			assert.Equal(t, tt.wantArg, arg)
		})
	}
}

func TestExtractFirstStringArg(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  string
	}{
		{
			name:  "command key",
			input: map[string]any{"command": "git status"},
			want:  "git status",
		},
		{
			name:  "path key when no command",
			input: map[string]any{"path": "/etc/hosts"},
			want:  "/etc/hosts",
		},
		{
			name:  "command takes priority over path",
			input: map[string]any{"command": "ls", "path": "/tmp"},
			want:  "ls",
		},
		{
			name:  "nil input",
			input: nil,
			want:  "",
		},
		{
			name:  "empty map",
			input: map[string]any{},
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ExtractFirstStringArg(tt.input))
		})
	}
}
