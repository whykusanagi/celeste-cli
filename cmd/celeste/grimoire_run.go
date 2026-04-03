package main

import (
	"fmt"
	"os"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
)

// runInitCommand handles the "celeste init" subcommand.
func runInitCommand(args []string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}

	path, err := grimoire.Init(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Created %s\n", path)
	fmt.Println("Edit this file to customize your project context for Celeste.")
}

// runGrimoireCommand handles the "celeste grimoire" subcommand.
func runGrimoireCommand(args []string) {
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: cannot determine working directory: %v\n", err)
		os.Exit(1)
	}

	g, err := grimoire.LoadAll(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading grimoire: %v\n", err)
		os.Exit(1)
	}

	if g.IsEmpty() {
		fmt.Println("No .grimoire found. Run `celeste init` to create one.")
		return
	}

	// Show sources
	if len(g.Sources) > 0 {
		fmt.Println("Sources:")
		for _, s := range g.Sources {
			fmt.Printf("  - %s\n", s)
		}
		fmt.Println()
	}

	// Show rendered grimoire
	fmt.Print(g.Render())
}
