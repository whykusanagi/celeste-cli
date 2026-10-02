package rules

// builtin returns the shipped rule named name (nil if a build dropped it).
func builtin(name string) *Rule {
	for _, r := range Builtins() {
		if r.Name == name {
			return r
		}
	}
	return nil
}

var (
	voiceRule       = builtin("persona-voice-in-files")
	destructiveRule = builtin("destructive-bash")
)

// VoiceLeak reports persona voice in file content as the
// persona-voice-in-files rule judges it (fenced code, quotation lines,
// docs and persona paths exempt). The watchdog's heuristic uses it.
func VoiceLeak(path, content string) bool {
	if voiceRule == nil {
		return false
	}
	h := Hit{Rule: voiceRule, Scope: Scope{Kind: ScopeToolArgs, Tool: "write_file", Field: "content"},
		Call: &Call{Name: "write_file", Input: map[string]any{"path": path, "content": content}}}
	return voiceRule.Condition.MatchString(content) && voiceInFileGuard(nil, h)
}

// Destructive reports a bash command the destructive-bash rule fires on
// (a force push, a recursive forced rm outside build output), read as a
// shell would read it.
func Destructive(command string) bool {
	if destructiveRule == nil {
		return false
	}
	return destructiveRule.Condition.MatchString(command) && destructiveShell(command, 0)
}
