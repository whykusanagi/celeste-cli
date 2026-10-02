package main

import (
	"fmt"
	"io"
	"sync"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
)

// runPersonaCommand is `celeste persona verify` (W5 ruling 20). It exits 0
// with a one-line summary when this binary runs the official persona, and 1
// when only the public persona is active. release.yml runs it on the built
// binary, so a release without a working key fails instead of shipping.
func runPersonaCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "verify" {
		fmt.Fprintln(stderr, "Usage: celeste persona verify")
		return 2
	}
	summary, err := prompts.VerifyPersona()
	if err != nil {
		fmt.Fprintf(stderr, "persona: public persona only: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, summary)
	return 0
}

// personaNoticeOnce keeps the public-persona notice to one line per
// process outside the chat (ruling 17). A variable so a test can reset it.
var personaNoticeOnce sync.Once

// printPersonaNoticeOnce prints the public-persona notice, if any, once.
func printPersonaNoticeOnce(w io.Writer) {
	personaNoticeOnce.Do(func() {
		if n := prompts.PersonaNotice(); n != "" {
			fmt.Fprintln(w, "ℹ "+n)
		}
	})
}
