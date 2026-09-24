package llm

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

type modeStubTool struct{ name string }

func (s modeStubTool) Name() string                               { return s.name }
func (s modeStubTool) Description() string                        { return s.name }
func (s modeStubTool) Parameters() json.RawMessage                { return json.RawMessage(`{"type":"object"}`) }
func (s modeStubTool) IsConcurrencySafe(_ map[string]any) bool    { return true }
func (s modeStubTool) IsReadOnly() bool                           { return true }
func (s modeStubTool) ValidateInput(_ map[string]any) error       { return nil }
func (s modeStubTool) InterruptBehavior() tools.InterruptBehavior { return tools.InterruptCancel }
func (s modeStubTool) Execute(_ context.Context, _ map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	return tools.ToolResult{}, nil
}

func skillNames(c *Client) []string {
	var names []string
	for _, s := range c.GetSkills() {
		names = append(names, s.Name)
	}
	sort.Strings(names)
	return names
}

func equalNames(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestGetSkillsHonoursMode covers #167: GetSkills used registry.GetAll(), so
// chat-only tools reached agent runs and mode tags had no effect.
func TestGetSkillsHonoursMode(t *testing.T) {
	r := tools.NewRegistry()
	r.RegisterWithModes(modeStubTool{"chat_only"}, tools.ModeChat)
	r.RegisterWithModes(modeStubTool{"agent_only"}, tools.ModeAgent)
	r.RegisterWithModes(modeStubTool{"both"}, tools.ModeChat, tools.ModeAgent)
	r.Register(modeStubTool{"untagged"}) // no modes = every mode

	c := &Client{registry: r}
	if got, want := skillNames(c), []string{"both", "chat_only", "untagged"}; !equalNames(got, want) {
		t.Errorf("default (chat) mode skills = %v, want %v", got, want)
	}

	c.SetToolMode(tools.ModeAgent)
	if got, want := skillNames(c), []string{"agent_only", "both", "untagged"}; !equalNames(got, want) {
		t.Errorf("agent mode skills = %v, want %v", got, want)
	}
}

// TestGetSkillsHonoursDiscoveryHiding: hidden tools stay out of the request
// until find_tools activates them, which GetAll ignored.
func TestGetSkillsHonoursDiscoveryHiding(t *testing.T) {
	r := tools.NewRegistry()
	r.Register(modeStubTool{"visible"})
	r.Register(modeStubTool{"mcp_tool"})
	r.SetHidden("mcp_tool", true)
	r.SetDiscoveryMode(true)

	c := &Client{registry: r}
	if got, want := skillNames(c), []string{"visible"}; !equalNames(got, want) {
		t.Errorf("skills with mcp_tool hidden = %v, want %v", got, want)
	}

	r.Activate("mcp_tool")
	if got, want := skillNames(c), []string{"mcp_tool", "visible"}; !equalNames(got, want) {
		t.Errorf("skills after activation = %v, want %v", got, want)
	}
}

func TestGetSkillsNilRegistry(t *testing.T) {
	if got := (&Client{}).GetSkills(); got != nil {
		t.Errorf("GetSkills with no registry = %v, want nil", got)
	}
}
