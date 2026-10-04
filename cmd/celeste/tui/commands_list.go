package tui

// KnownCommands returns a copy of the slash commands the TUI knows, for help
// text checks outside this package.
func KnownCommands() []string {
	return append([]string(nil), knownCommands...)
}
