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

// resolveMaxIterFlags merges --set-max-tool-iterations (newVal) and the
// deprecated --set-claw-max-iterations (legacyVal). -1 means the flag was not
// given. The new flag wins when both are given; the legacy one warns.
func resolveMaxIterFlags(newVal, legacyVal int, warn io.Writer) (int, error) {
	if newVal == 0 || legacyVal == 0 {
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
