package selfupdate

import (
	"runtime/debug"
	"testing"
)

// A go install of a fork (another module path) is not upgraded to the
// upstream release: only builds of this module are Module builds.
func TestClassifyOnlyTheUpstreamModule(t *testing.T) {
	for path, want := range map[string]Kind{
		"github.com/whykusanagi/celeste-cli":    Module,
		"github.com/whykusanagi/celeste-cli/v2": Module,
		"github.com/someone/celeste-cli":        Source,
		"github.com/whykusanagi/celeste-cli-x":  Source,
		"":                                      Source,
	} {
		bi := &debug.BuildInfo{Main: debug.Module{Path: path, Version: "v2.0.0"}}
		if kind, _ := Classify(bi, true, false, false); kind != want {
			t.Errorf("Classify(%q) = %v, want %v", path, kind, want)
		}
	}
}
