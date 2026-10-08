package main

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// Aikido 806780137: modules with a known advisory stay at or above the
// fixed release, even as indirect requirements.
func TestGoModKeepsPatchedDependencyFloors(t *testing.T) {
	b, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for mod, floor := range map[string][3]int{
		"github.com/libp2p/go-libp2p": {0, 27, 8},
	} {
		m := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(mod) + `\s+v(\d+)\.(\d+)\.(\d+)`).FindStringSubmatch(string(b))
		if m == nil {
			continue // no longer required at all
		}
		var got [3]int
		for i := range got {
			got[i], _ = strconv.Atoi(m[i+1])
		}
		for i := range got {
			if got[i] != floor[i] {
				if got[i] < floor[i] {
					t.Errorf("go.mod requires %s v%d.%d.%d, want at least v%d.%d.%d", mod, got[0], got[1], got[2], floor[0], floor[1], floor[2])
				}
				break
			}
		}
	}
}
