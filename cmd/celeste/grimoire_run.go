package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/grimoire"
)

// runInitCommand handles "celeste init [--agents]".
func runInitCommand(args []string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}
	if err := initProject(cwd, args, os.Stdout); err != nil {
		if msg := initErrorText(err); msg != "" {
			fmt.Fprintln(os.Stderr, msg)
		}
		os.Exit(1)
	}
}

// initErrorText is what `celeste init` prints on stderr for err. When
// nothing was written because every file already exists, the "(left as it
// is)" lines on stdout already say so; the exit code alone reports it.
func initErrorText(err error) string {
	if errors.Is(err, iofs.ErrExist) {
		return ""
	}
	return fmt.Sprintf("Error: %v", err)
}

// initProject writes .grimoire in dir and, with --agents, AGENTS.md (2.0
// W4, ruling 7). A file that is already there is left alone and noted; it
// is an error only when nothing was written.
func initProject(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(out)
	agents := fs.Bool("agents", false, "also write AGENTS.md (build and test commands for coding agents)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil // the usage is printed
		}
		return err
	}
	lines, err := grimoire.RunInit(dir, *agents)
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}
	return err
}

// runGrimoireCommand handles the "celeste grimoire" subcommand.
func runGrimoireCommand(args []string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}
	if err := showGrimoire(cwd, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading grimoire: %v\n", err)
		os.Exit(1)
	}
}

// showGrimoire prints grimoire.Describe, the text /grimoire shows.
func showGrimoire(dir string, out io.Writer) error {
	text, _ := grimoire.Describe(dir)
	_, err := fmt.Fprintln(out, strings.TrimRight(text, "\n"))
	return err
}
