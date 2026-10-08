package main

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// advisoryRange is one affected range of an advisory: versions from lo
// (inclusive, "" for no lower bound) up to the patched release hi
// (exclusive).
type advisoryRange struct{ lo, hi string }

// advisoryFloors lists, per module with a known advisory, the advisory's
// affected ranges. go.mod must require a tagged release outside all of them.
var advisoryFloors = map[string][]advisoryRange{
	// GHSA-876p-8259-xjgg (Aikido 806780137).
	"github.com/libp2p/go-libp2p": {{"", "v0.27.8"}, {"v0.28.0", "v0.28.2"}, {"v0.29.0", "v0.29.1"}},
}

var goModVersion = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// parseModVersion splits a Go module version into its numeric release and
// whether it is a prerelease (pseudo-versions included).
func parseModVersion(v string) (rel [3]int, pre bool, ok bool) {
	m := goModVersion.FindStringSubmatch(v)
	if m == nil {
		return rel, false, false
	}
	for i := range rel {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return rel, false, false
		}
		rel[i] = n
	}
	return rel, m[4] != "", true
}

// releaseLess reports a < b for two tagged releases.
func releaseLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// advisoryVerdict returns "" when v is a tagged release outside every
// affected range, or why it is not acceptable. A prerelease or
// pseudo-version is never accepted: it may predate the fix it sorts near.
func advisoryVerdict(v string, ranges []advisoryRange) string {
	rel, pre, ok := parseModVersion(v)
	switch {
	case !ok:
		return "is not a Go module version"
	case pre:
		return "is a prerelease or pseudo-version, not a patched release"
	}
	for _, r := range ranges {
		hi, _, _ := parseModVersion(r.hi)
		if !releaseLess(rel, hi) {
			continue
		}
		if r.lo == "" {
			return "is below the patched release " + r.hi
		}
		if lo, _, _ := parseModVersion(r.lo); !releaseLess(rel, lo) {
			return "is in the affected range " + r.lo + " up to " + r.hi
		}
	}
	return ""
}

// CodeRabbit review on #423: the floor is the advisory's affected ranges,
// not one minimum, and a prerelease of the patched release is not it.
func TestAdvisoryVerdict(t *testing.T) {
	ranges := advisoryFloors["github.com/libp2p/go-libp2p"]
	for v, ok := range map[string]bool{
		"v0.27.7":                               false,
		"v0.27.8":                               true,
		"v0.27.8-rc1":                           false,
		"v0.28.0":                               false,
		"v0.28.1":                               false,
		"v0.28.2":                               true,
		"v0.29.0":                               false,
		"v0.29.1":                               true,
		"v0.30.0":                               true,
		"v1.0.0":                                true,
		"v0.30.0+incompatible":                  true,
		"0.30.0":                                false,
		"v0.27.9-0.20230601000000-abcdefabcdef": false,
	} {
		if got := advisoryVerdict(v, ranges); (got == "") != ok {
			t.Errorf("advisoryVerdict(%s) = %q, want accepted=%v", v, got, ok)
		}
	}
}

// Aikido 806780137: modules with a known advisory stay on a patched
// release, even as indirect requirements.
func TestGoModKeepsPatchedDependencyFloors(t *testing.T) {
	b, err := os.ReadFile("../../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for mod, ranges := range advisoryFloors {
		m := regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(mod) + `\s+(\S+)`).FindStringSubmatch(string(b))
		if m == nil {
			continue // no longer required at all
		}
		if why := advisoryVerdict(m[1], ranges); why != "" {
			t.Errorf("go.mod requires %s %s, which %s", mod, m[1], why)
		}
	}
}
