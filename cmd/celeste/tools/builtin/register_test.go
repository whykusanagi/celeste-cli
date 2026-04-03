package builtin

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestRegisterAll_DevToolsOnly(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterAll(registry, t.TempDir(), nil, nil, nil)
	// 6 dev tools + 2 web tools + 14 config-free skills = 22
	assert.True(t, registry.Count() > 6, "expected more than 6 tools, got %d", registry.Count())
	bash, ok := registry.Get("bash")
	assert.True(t, ok)
	assert.Equal(t, "bash", bash.Name())
}

func TestRegisterAll_WithModeFiltering(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterAll(registry, t.TempDir(), nil, nil, nil)
	agentTools := registry.GetTools(tools.ModeAgent)
	// Agent should have dev tools but not chat-only skills like tarot
	for _, tool := range agentTools {
		assert.NotEqual(t, "generate_uuid", tool.Name(), "uuid should not be in agent mode")
		assert.NotEqual(t, "tarot_reading", tool.Name(), "tarot should not be in agent mode")
	}
}

func TestRegisterAll_ChatModeHasSkills(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterAll(registry, t.TempDir(), nil, nil, nil)
	chatTools := registry.GetTools(tools.ModeChat)

	// Chat mode should have dev tools + config-free skills
	toolNames := make(map[string]bool)
	for _, tool := range chatTools {
		toolNames[tool.Name()] = true
	}

	// Dev tools should be in chat mode
	assert.True(t, toolNames["bash"], "bash should be in chat mode")
	assert.True(t, toolNames["read_file"], "read_file should be in chat mode")

	// Config-free skills should be in chat mode
	assert.True(t, toolNames["generate_uuid"], "generate_uuid should be in chat mode")
	assert.True(t, toolNames["generate_hash"], "generate_hash should be in chat mode")
	assert.True(t, toolNames["base64_encode"], "base64_encode should be in chat mode")
	assert.True(t, toolNames["convert_currency"], "convert_currency should be in chat mode")
	assert.True(t, toolNames["generate_qr_code"], "generate_qr_code should be in chat mode")
	assert.True(t, toolNames["convert_units"], "convert_units should be in chat mode")
	assert.True(t, toolNames["convert_timezone"], "convert_timezone should be in chat mode")
}

func TestRegisterAll_NoWorkspace(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterAll(registry, "", nil, nil, nil)
	// Should only have config-free skills (no dev tools)
	_, ok := registry.Get("bash")
	assert.False(t, ok, "bash should not be registered without workspace")
	_, ok = registry.Get("generate_uuid")
	assert.True(t, ok, "generate_uuid should be registered without workspace")
}

func TestRegisterReadOnlyDevTools(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterReadOnlyDevTools(registry, t.TempDir())
	assert.Equal(t, 3, registry.Count(), "should have 3 read-only dev tools")
	_, ok := registry.Get("read_file")
	assert.True(t, ok)
	_, ok = registry.Get("list_files")
	assert.True(t, ok)
	_, ok = registry.Get("search")
	assert.True(t, ok)
}

func TestToolCount(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterAll(registry, t.TempDir(), nil, nil, nil)
	// 6 dev tools + 2 git tools + 2 web tools + 14 config-free skills + 1 todo = 25
	// (config-dependent tools not registered when configLoader is nil)
	assert.Equal(t, 25, registry.Count(), "expected 25 tools without configLoader")
}
