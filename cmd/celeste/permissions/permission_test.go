// cmd/celeste/permissions/permission_test.go
package permissions

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDecision_String(t *testing.T) {
	tests := []struct {
		d    Decision
		want string
	}{
		{Allow, "allow"},
		{Deny, "deny"},
		{Ask, "ask"},
		{Decision(99), "unknown"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.d.String())
	}
}

func TestPermissionMode_String(t *testing.T) {
	tests := []struct {
		m    PermissionMode
		want string
	}{
		{ModeDefault, "default"},
		{ModeStrict, "strict"},
		{ModeTrust, "trust"},
		{PermissionMode("bogus"), "bogus"},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.want, tt.m.String())
	}
}

func TestPermissionMode_Valid(t *testing.T) {
	assert.True(t, ModeDefault.Valid())
	assert.True(t, ModeStrict.Valid())
	assert.True(t, ModeTrust.Valid())
	assert.False(t, PermissionMode("yolo").Valid())
}

func TestParsePermissionMode(t *testing.T) {
	tests := []struct {
		input string
		want  PermissionMode
		ok    bool
	}{
		{"default", ModeDefault, true},
		{"strict", ModeStrict, true},
		{"trust", ModeTrust, true},
		{"DEFAULT", ModeDefault, true},
		{"Trust", ModeTrust, true},
		{"invalid", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		got, err := ParsePermissionMode(tt.input)
		if tt.ok {
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		} else {
			assert.Error(t, err)
		}
	}
}

func TestRule_Fields(t *testing.T) {
	r := Rule{
		ToolPattern:  "bash(git *)",
		InputPattern: "",
		Decision:     Allow,
	}
	assert.Equal(t, "bash(git *)", r.ToolPattern)
	assert.Equal(t, Allow, r.Decision)
	assert.Empty(t, r.InputPattern)
}

func TestCheckResult_IsAllowed(t *testing.T) {
	assert.True(t, CheckResult{Decision: Allow}.IsAllowed())
	assert.False(t, CheckResult{Decision: Deny}.IsAllowed())
	assert.False(t, CheckResult{Decision: Ask}.IsAllowed())
}

func TestCheckResult_IsDenied(t *testing.T) {
	assert.True(t, CheckResult{Decision: Deny}.IsDenied())
	assert.False(t, CheckResult{Decision: Allow}.IsDenied())
	assert.False(t, CheckResult{Decision: Ask}.IsDenied())
}

func TestCheckResult_NeedsPrompt(t *testing.T) {
	assert.True(t, CheckResult{Decision: Ask}.NeedsPrompt())
	assert.False(t, CheckResult{Decision: Allow}.NeedsPrompt())
	assert.False(t, CheckResult{Decision: Deny}.NeedsPrompt())
}
