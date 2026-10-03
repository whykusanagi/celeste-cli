package rules

import (
	"path"
	"strings"
	"sync/atomic"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/shellparse"
)

// buildDirs are build output a project regenerates: removing one inside
// the workspace is routine, not destructive.
var buildDirs = map[string]bool{"build": true, "dist": true, "node_modules": true, "target": true, ".cache": true, "out": true, "coverage": true}

// destructiveBash fires on a force push, or on an rm that is both
// recursive and forced (-rf, -r -f, --recursive --force or GNU prefixes,
// flags before or after the paths) unless every path it removes is a build
// directory (or inside one) under the workspace. It reads the command with
// internal/shellparse, as the bash tool's blocking check does, and also
// fires on everything that check refuses (shellparse.DestructiveRm), so a
// line the bash tool blocks is never invisible here. The condition is any
// command (\S): spellings such as r\<newline>m or $'\x72m' defeat a
// regex, so the guard decides. Not covered: find -delete, scripts the
// command runs, and rm fed its paths on stdin (xargs) unless the bash tool
// refuses it.
func destructiveBash(h Hit) bool {
	if h.Call == nil {
		return true
	}
	cmd, _ := h.Call.Input["command"].(string)
	return destructiveShell(cmd)
}

func destructiveShell(cmd string) bool {
	// One walk: a refused rm, too deep a nesting (TooDeep), or the rule's
	// own policy.
	return shellparse.Walk(cmd, func(words []string) bool {
		if shellparse.RmRefused(words) {
			return true
		}
		switch name, args := shellparse.Command(words); name {
		case "rm":
			return rmRecursiveForce(args)
		case "git":
			return gitForcePush(args)
		}
		return false
	}) != shellparse.None
}

// shellSteps counts the words this file's own loops examine, on top of
// shellparse.Steps, so tests can check destructiveShell's work is linear.
var shellSteps atomic.Int64

func gitForcePush(args []string) bool {
	push := false
	var c int64
	defer func() {
		if c > 0 {
			shellSteps.Add(c)
		}
	}()
	for _, a := range args {
		c++
		switch {
		case a == "push":
			push = true
		case push && (a == "--force" || a == "-f" || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "f"))):
			return true
		}
	}
	return false
}

// rmRecursiveForce: a recursive forced rm of anything but build output.
// Its policy differs from the bash tool's on purpose: it asks about any
// path outside build directories, not only system and home paths.
func rmRecursiveForce(args []string) bool {
	recursive, force, targets := shellparse.RmFlags(args)
	if !recursive || !force {
		return false
	}
	if len(targets) == 0 {
		return true
	}
	if len(targets) > 0 {
		shellSteps.Add(int64(len(targets)))
	}
	for _, t := range targets {
		if !buildDirTarget(t) {
			return true
		}
	}
	return false
}

// buildDirTarget: a relative path inside the workspace whose first element
// is a build directory.
func buildDirTarget(t string) bool {
	if t == "" || strings.ContainsAny(t, "$~*?`\\") || strings.HasPrefix(t, "/") {
		return false
	}
	clean := path.Clean(t)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	first, _, _ := strings.Cut(clean, "/")
	return buildDirs[first]
}
