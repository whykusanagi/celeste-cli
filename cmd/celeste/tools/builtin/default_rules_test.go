package builtin

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// Every plain tool name in the default permission rules must be a registered
// tool; the allow rule once named search_files, which doesn't exist (#188).
func TestDefaultPermissionRulesNameRealTools(t *testing.T) {
	registry := tools.NewRegistry()
	RegisterAll(registry, t.TempDir(), nil, nil, nil)

	cfg := permissions.DefaultConfig()
	for _, rule := range append(cfg.AlwaysAllow, cfg.AlwaysDeny...) {
		name := rule.ToolPattern
		if i := strings.IndexByte(name, '('); i >= 0 {
			name = name[:i] // bash(sudo *) -> bash
		}
		if strings.ContainsAny(name, "*?") {
			continue
		}
		if _, ok := registry.Get(name); !ok {
			t.Errorf("default rule %q names a tool that isn't registered", rule.ToolPattern)
		}
	}
}
