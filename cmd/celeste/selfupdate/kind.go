// Package selfupdate turns a `go install` build of celeste into the official
// release binary of the same version, and backs `celeste update` (W5
// rulings 25–30). It checks everything it downloads against the release
// signing key embedded in the binary and never installs a file that failed a
// check.
package selfupdate

import (
	"errors"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
)

// Kind is what sort of build a binary is (ruling 25).
type Kind int

const (
	// Source is a checkout build, a pseudo-version or anything unrecognised:
	// the public persona, and the updater never touches the network.
	Source Kind = iota
	// Module is `go install …/cmd/celeste@vX.Y.Z` or @latest: no persona
	// key, a release tag, no VCS stamp. It upgrades itself.
	Module
	// Keyed is a local build that carries the persona key (make build with
	// ~/.celeste/persona.key): the full persona; it never downloads.
	Keyed
	// Official is a release binary: main.Channel == "release".
	Official
)

// ErrBadTag is a version string that is not a release tag.
var ErrBadTag = errors.New("not a release tag")

var (
	tagRE    = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$`)
	pseudoRE = regexp.MustCompile(`[0-9]{14}-[0-9a-f]{12}$`)
)

// ValidTag reports whether s is a release tag celeste will put in a URL:
// vMAJOR.MINOR.PATCH with an optional pre-release, not a Go pseudo-version.
func ValidTag(s string) bool { return tagRE.MatchString(s) && !pseudoRE.MatchString(s) }

// ModulePath is the module whose go install builds upgrade to the official
// release; a fork's build (another path) is left alone.
const ModulePath = "github.com/whykusanagi/celeste-cli"

// upstreamModule reports this module, or a major version of it (/v2).
func upstreamModule(path string) bool {
	if path == ModulePath {
		return true
	}
	v, ok := strings.CutPrefix(path, ModulePath+"/v")
	return ok && v != "" && strings.Trim(v, "0123456789") == ""
}

// Classify sorts a binary into a Kind (ruling 25). official is
// main.Channel == "release" and hasKey is prompts.HasPersonaKey(). The tag
// is returned only for Module: the version go install recorded.
func Classify(info *debug.BuildInfo, ok, official, hasKey bool) (Kind, string) {
	switch {
	case official:
		return Official, ""
	case hasKey:
		return Keyed, ""
	case !ok || info == nil:
		return Source, ""
	}
	for _, s := range info.Settings {
		// Since Go 1.24 a checkout build stamps its version from VCS too
		// (v2.0.0 on a clean tag); only a checkout records vcs.
		if s.Key == "vcs" {
			return Source, ""
		}
	}
	if v := info.Main.Version; upstreamModule(info.Main.Path) && ValidTag(v) {
		return Module, v
	}
	return Source, ""
}

// Newer reports whether release tag a comes after b in semver order. A
// string that is not a ValidTag is never newer, and nothing is newer than it.
func Newer(a, b string) bool { return compareTags(a, b) > 0 }

func compareTags(a, b string) int {
	if !ValidTag(a) || !ValidTag(b) {
		return 0
	}
	am, bm := tagRE.FindStringSubmatch(a), tagRE.FindStringSubmatch(b)
	for i := 1; i <= 3; i++ {
		if c := compareNum(am[i], bm[i]); c != 0 {
			return c
		}
	}
	ap, bp := strings.TrimPrefix(am[4], "-"), strings.TrimPrefix(bm[4], "-")
	switch {
	case ap == bp:
		return 0
	case ap == "":
		return 1 // a release beats its pre-releases
	case bp == "":
		return -1
	}
	return comparePre(strings.Split(ap, "."), strings.Split(bp, "."))
}

func compareNum(x, y string) int {
	if len(x) != len(y) { // no leading zeros, so the longer number is larger
		if len(x) > len(y) {
			return 1
		}
		return -1
	}
	return strings.Compare(x, y)
}

func comparePre(a, b []string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		_, xerr := strconv.ParseUint(a[i], 10, 64)
		_, yerr := strconv.ParseUint(b[i], 10, 64)
		switch {
		case xerr == nil && yerr == nil:
			if c := compareNum(a[i], b[i]); c != 0 {
				return c
			}
		case xerr == nil:
			return -1 // numeric identifiers sort before alphanumeric ones
		case yerr == nil:
			return 1
		default:
			if c := strings.Compare(a[i], b[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(a) > len(b):
		return 1
	case len(a) < len(b):
		return -1
	}
	return 0
}
