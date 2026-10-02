package selfupdate

import (
	"fmt"
	"runtime/debug"
	"testing"
)

func info(version string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{
		Main:     debug.Module{Path: "github.com/whykusanagi/celeste-cli", Version: version},
		Settings: settings,
	}
}

var vcs = []debug.BuildSetting{{Key: "vcs", Value: "git"}, {Key: "vcs.revision", Value: "4e51705aabbccddeeff00112233445566778899"}}

// Ruling 25 and Review Focus 12: only a keyless, VCS-free, release-tagged
// build is a module build. A checkout at a tag carries the same version
// since Go 1.24, so the vcs setting decides.
func TestClassify(t *testing.T) {
	cases := []struct {
		name              string
		info              *debug.BuildInfo
		ok, official, key bool
		kind              Kind
		tag               string
	}{
		{"go install @v2.0.0", info("v2.0.0"), true, false, false, Module, "v2.0.0"},
		{"go install a pre-release", info("v2.0.0-rc.1"), true, false, false, Module, "v2.0.0-rc.1"},
		{"checkout built with -buildvcs=false", info("(devel)"), true, false, false, Source, ""},
		{"checkout at a tag, VCS-stamped", info("v2.0.0", vcs...), true, false, false, Source, ""},
		{"dirty checkout", info("v2.0.1-0.20261001120000-4e51705aabbc+dirty", vcs...), true, false, false, Source, ""},
		{"go install @main (pseudo-version)", info("v2.0.1-0.20261001120000-4e51705aabbc"), true, false, false, Source, ""},
		{"pre-release pseudo-version", info("v2.0.0-rc.1.0.20261001120000-4e51705aabbc"), true, false, false, Source, ""},
		{"+incompatible", info("v2.0.0+incompatible"), true, false, false, Source, ""},
		{"no build info", nil, false, false, false, Source, ""},
		{"make build with the persona key", info("(devel)", vcs...), true, false, true, Keyed, ""},
		{"official release", info("v2.0.0", vcs...), true, true, true, Official, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			kind, tag := Classify(c.info, c.ok, c.official, c.key)
			if kind != c.kind || tag != c.tag {
				t.Fatalf("Classify = %s %q, want %s %q", kindName(kind), tag, kindName(c.kind), c.tag)
			}
		})
	}
}

func TestValidTagRejectsAnythingButATag(t *testing.T) {
	for _, s := range []string{"v2.0.0", "v2.0.0-rc.1", "v10.20.30-beta.2"} {
		if !ValidTag(s) {
			t.Errorf("ValidTag(%q) = false", s)
		}
	}
	for _, s := range []string{"", "2.0.0", "v2.0", "v02.0.0", "v2.0.0+meta", "latest", "(devel)",
		"v2.0.0/../../evil", "v2.0.0-rc.1/x", "v2.0.0?x=1", "v2.0.1-0.20261001120000-4e51705aabbc"} {
		if ValidTag(s) {
			t.Errorf("ValidTag(%q) = true", s)
		}
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v2.0.1", "v2.0.0", true},
		{"v2.1.0", "v2.0.9", true},
		{"v10.0.0", "v9.9.9", true},
		{"v2.0.0", "v2.0.0", false},
		{"v2.0.0", "v2.0.1", false},
		{"v2.0.0", "v2.0.0-rc.1", true}, // a release beats its pre-releases
		{"v2.0.0-rc.1", "v2.0.0", false},
		{"v2.0.0-rc.2", "v2.0.0-rc.1", true},
		{"v2.0.0-rc.10", "v2.0.0-rc.9", true}, // numeric identifiers compare as numbers
		{"v2.0.0-rc.1", "v2.0.0-beta.9", true},
		{"v2.0.0-rc.1.1", "v2.0.0-rc.1", true},
		{"garbage", "v1.0.0", false},
		{"v1.0.0", "garbage", false},
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

// kindName names a Kind in failure messages; Kind has no String method.
func kindName(k Kind) string {
	if n, ok := map[Kind]string{Source: "Source", Module: "Module", Keyed: "Keyed", Official: "Official"}[k]; ok {
		return n
	}
	return fmt.Sprintf("Kind(%d)", int(k))
}
