package main

import (
	"errors"
	"fmt"
	"io"
)

// setModeError is the answer to `config --set-mode` (#144, spec §6.2).
func setModeError(value string) error {
	if value == "" {
		return nil
	}
	return errors.New("--set-mode was removed in celeste 2.0: chat always runs tools in a loop, so there is no mode to set. See MIGRATING-2.0.md")
}

// skipPersonaError answers --skip-persona (W5 ruling 18).
func skipPersonaError(value string) error {
	if value == "" {
		return nil
	}
	return errors.New("--skip-persona was removed in celeste 2.0: the persona is always on in chat and agent runs. See MIGRATING-2.0.md")
}

// resolveMaxIterFlags merges --set-max-tool-iterations (newVal) and the
// deprecated --set-claw-max-iterations (legacyVal). -1 means the flag was not
// given; any other value <= 0 was typed by the user and is invalid, so the
// error names whichever flag carried it (M5) instead of always blaming
// --set-max-tool-iterations, and a negative value is rejected rather than
// silently treated as "not given" and ignored (M5). The new flag wins when
// both are given; the legacy one warns.
func resolveMaxIterFlags(newVal, legacyVal int, warn io.Writer) (int, error) {
	if legacyVal != -1 && legacyVal <= 0 {
		return 0, errors.New("--set-claw-max-iterations must be greater than zero")
	}
	if newVal != -1 && newVal <= 0 {
		return 0, errors.New("--set-max-tool-iterations must be greater than zero")
	}
	if legacyVal > 0 {
		fmt.Fprintln(warn, "Warning: --set-claw-max-iterations is deprecated; use --set-max-tool-iterations (see MIGRATING-2.0.md)")
	}
	if newVal > 0 {
		return newVal, nil
	}
	return legacyVal, nil
}

// removedTemplates are the `config --init` names 2.0 dropped with the
// runtime mode (#144).
var removedTemplates = map[string]string{
	"celeste-classic": "openai",
	"celeste-claw":    "openai",
}

// setTimeoutError refuses a negative --set-timeout: 0 selects the default,
// and a timeout below that means nothing.
func setTimeoutError(v int, given bool) error {
	if given && v < 0 {
		return fmt.Errorf("--set-timeout must be 0 (the default) or a number of seconds, got %d", v)
	}
	return nil
}
