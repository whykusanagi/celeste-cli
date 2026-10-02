package builtin

import (
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// Builtins register with Register (programmer-controlled names); none may
// silently replace another (2.0 W4, ruling 12).
func TestBuiltinNamesAreUnique(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	reg := tools.NewRegistry()
	RegisterAll(reg, t.TempDir(), nil, nil, nil)
	if dup := reg.Overwritten(); len(dup) > 0 {
		t.Fatalf("builtins registered twice: %v", dup)
	}
}
