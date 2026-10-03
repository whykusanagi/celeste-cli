package sandbox

import "strings"

// writeDenials are what a write the sandbox refused looks like in a
// command's output, lower case: C tools print "Operation not permitted",
// Go tools "operation not permitted".
var writeDenials = []string{"operation not permitted", "read-only file system", "deny(1) file-write"}

// networkFailures are what a cut network looks like, lower case.
var networkFailures = []string{"could not resolve host", "temporary failure in name resolution", "network is unreachable", "nodename nor servname", "no such host"}

// Hint explains a failed command that ran under the kind sandbox with
// policy p, when its output looks like the sandbox refused it: the
// sandbox and the config key to change (spec §5 W4, ruling 10). "" when
// the output names nothing the sandbox blocks.
func Hint(kind string, p Policy, output string) string {
	output = strings.ToLower(output)
	if containsAny(output, writeDenials) {
		return "blocked by celeste's sandbox (" + kind + "): writes are limited to the workspace, temp and cache directories; " +
			"add the directory to \"sandbox.writable\" in .celeste/config.json, or set \"sandbox.enabled\": false there " +
			"(that file must be trusted: celeste hooks trust)"
	}
	if !p.Network && containsAny(output, networkFailures) {
		return "blocked by celeste's sandbox (" + kind + "): network is off (\"sandbox.network\": false in ~/.celeste/config.json or .celeste/config.json)"
	}
	return ""
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
