package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	iofs "io/fs"
	"os"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
)

// runInitCommand handles "celeste init [--agents]".
func runInitCommand(args []string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}
	if err := initProject(cwd, args, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// initProject writes .grimoire in dir and, with --agents, AGENTS.md (2.0
// W4, ruling 7). A file that is already there is left alone and noted; it
// is an error only when nothing was written.
func initProject(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(out)
	agents := fs.Bool("agents", false, "also write AGENTS.md (build and test commands for coding agents)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	steps := []func(string) (string, error){grimoire.Init}
	if *agents {
		steps = append(steps, grimoire.InitAgents)
	}
	var wrote int
	var skipped []string
	for _, step := range steps {
		path, err := step(dir)
		switch {
		case errors.Is(err, iofs.ErrExist):
			skipped = append(skipped, err.Error())
			fmt.Fprintf(out, "%v (left as it is)\n", err)
		case err != nil:
			return err
		default:
			wrote++
			fmt.Fprintf(out, "Created %s\n", path)
		}
	}
	if wrote == 0 {
		return errors.New(strings.Join(skipped, "; "))
	}
	fmt.Fprintln(out, "Edit these files to describe the project; Celeste loads them into every session.")
	return nil
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

// showGrimoire prints the merged grimoire and then the AGENTS.md /
// CLAUDE.md section the project context carries under it (2.0 W4).
func showGrimoire(dir string, out io.Writer) error {
	g, err := grimoire.LoadAll(dir)
	if err != nil {
		return err
	}
	files, warns := grimoire.ContextFiles(dir)
	for _, w := range warns {
		fmt.Fprintln(out, "⚠ "+w)
	}
	if g.IsEmpty() && len(files) == 0 {
		fmt.Fprintln(out, "No .grimoire, AGENTS.md or CLAUDE.md found. Run `celeste init` to create a .grimoire.")
		return nil
	}
	sources := append([]string(nil), g.Sources...)
	for _, f := range files {
		sources = append(sources, f.Path)
	}
	if len(sources) > 0 {
		fmt.Fprintln(out, "Sources:")
		for _, s := range sources {
			fmt.Fprintf(out, "  - %s\n", s)
		}
		fmt.Fprintln(out)
	}
	if !g.IsEmpty() {
		fmt.Fprint(out, g.Render())
	}
	if section := grimoire.RenderContextFiles(files); section != "" {
		if !g.IsEmpty() {
			fmt.Fprintln(out)
		}
		fmt.Fprint(out, section)
	}
	return nil
}
