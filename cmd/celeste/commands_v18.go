package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/checkpoints"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/costs"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/sessions"
)

func runCostsCommand(args []string) {
	tracker := costs.NewSessionTracker()
	homeDir, _ := os.UserHomeDir()
	costPath := filepath.Join(homeDir, ".celeste", "session-costs.json")
	if err := tracker.Load(costPath); err != nil {
		fmt.Println("No cost data found for current session.")
		return
	}
	summary := tracker.GetSummary()
	fmt.Printf("Session Costs:\n")
	fmt.Printf("  Model:    %s\n", summary.Model)
	fmt.Printf("  Input:    %d tokens\n", summary.TotalInput)
	fmt.Printf("  Output:   %d tokens\n", summary.TotalOutput)
	fmt.Printf("  Cost:     $%.4f\n", summary.TotalCostUSD)
	fmt.Printf("  Turns:    %d\n", summary.Turns)
}

func runMemoriesCommand(args []string) {
	cwd, _ := os.Getwd()
	store := memories.NewStore(cwd)
	mems, err := store.List()
	if err != nil || len(mems) == 0 {
		fmt.Println("No memories found for this project.")
		return
	}
	fmt.Printf("Memories (%d):\n\n", len(mems))
	for _, m := range mems {
		fmt.Printf("  [%s] %s\n", m.Type, m.Name)
		fmt.Printf("    %s\n\n", m.Description)
	}
}

func runRememberCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: celeste remember \"<text>\"")
		os.Exit(1)
	}
	text := strings.Join(args, " ")
	cwd, _ := os.Getwd()
	store := memories.NewStore(cwd)
	slug := generateMemorySlug(text)
	mem := memories.NewMemory(slug, text, "feedback", "", text)
	_ = mem.Created // already set by NewMemory
	if err := store.Save(mem); err != nil {
		fmt.Fprintf(os.Stderr, "Error saving memory: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Remembered: %s\n", mem.Name)
}

func runForgetCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: celeste forget <memory-name>")
		os.Exit(1)
	}
	cwd, _ := os.Getwd()
	store := memories.NewStore(cwd)
	if err := store.Delete(args[0]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Forgot: %s\n", args[0])
}

func runResumeCommand(args []string) {
	cwd, _ := os.Getwd()
	mgr, err := sessions.NewManager(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	if len(args) > 0 {
		// Try to load the requested session to verify it exists
		_, err := mgr.ResumeSession(args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Session '%s' not found: %v\n", args[0], err)
			os.Exit(1)
		}
		fmt.Printf("Session '%s' exists. Launch with: celeste --session %s\n", args[0], args[0])
		return
	}
	list, err := mgr.ListSessions()
	if err != nil || len(list) == 0 {
		fmt.Println("No sessions found.")
		return
	}
	fmt.Printf("Recent sessions (%d):\n\n", len(list))
	for _, s := range list {
		fmt.Printf("  %s  %s  (%d entries)\n", s.ID, s.Title, s.EntryCount)
	}
}

func runPlanCommand(args []string) {
	cwd, _ := os.Getwd()
	planPaths := []string{
		filepath.Join(cwd, ".celeste", "plan.md"),
		filepath.Join(cwd, "CODEBASE_FIX_PLAN.md"),
		filepath.Join(cwd, "PLAN.md"),
		filepath.Join(cwd, "plan.md"),
		filepath.Join(cwd, "FIX_PLAN.md"),
	}

	// Find first existing plan
	for _, p := range planPaths {
		data, err := os.ReadFile(p)
		if err == nil {
			fmt.Printf("Plan (%s):\n\n%s\n", filepath.Base(p), string(data))
			return
		}
	}

	fmt.Println("No active plan found.")
	fmt.Println()
	fmt.Println("To create and execute plans:")
	fmt.Println("  celeste agent <goal>    Autonomous planning + execution")
	fmt.Println("  /plan <goal>            Enter plan mode (in interactive chat)")
	fmt.Println("  celeste plan show       Show current plan")
}

func runRevertCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: celeste revert <file-path>")
		fmt.Fprintln(os.Stderr, "\nReverts a file to its most recent checkpoint (pre-edit snapshot).")
		fmt.Fprintln(os.Stderr, "Checkpoints are created automatically before each write_file/patch_file.")
		os.Exit(1)
	}

	filePath := args[0]
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving path: %v\n", err)
		os.Exit(1)
	}

	// Find the most recent checkpoint for this file
	sm := checkpoints.NewSnapshotManager(fmt.Sprintf("cli-%d", os.Getpid()))
	if err := sm.Revert(absPath); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintln(os.Stderr, "\nNo checkpoint found. Checkpoints are created during interactive chat sessions.")
		fmt.Fprintln(os.Stderr, "Use `celeste chat` and edit files — checkpoints are saved automatically.")
		os.Exit(1)
	}
	fmt.Printf("Reverted: %s\n", filePath)
}

func runIndexCommand(args []string) {
	cwd, _ := os.Getwd()

	// Parse subcommands
	if len(args) > 0 {
		switch args[0] {
		case "rebuild", "--rebuild":
			// Delete and rebuild from scratch
			dbPath := codegraph.DefaultIndexPath(cwd)
			if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "Warning: could not remove old index: %v\n", err)
			}
			// Remove WAL/SHM files too
			os.Remove(dbPath + "-wal")
			os.Remove(dbPath + "-shm")
			fmt.Println("Old index removed. Rebuilding...")

		case "status":
			dbPath := codegraph.DefaultIndexPath(cwd)
			if _, err := os.Stat(dbPath); os.IsNotExist(err) {
				fmt.Println("No index exists for this project.")
				fmt.Println("Run `celeste index` to create one.")
				return
			}
			indexer, err := codegraph.NewIndexer(cwd, dbPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			defer indexer.Close()
			fmt.Println(indexer.ProjectSummary())
			return

		case "reset":
			// Delete index entirely
			dbPath := codegraph.DefaultIndexPath(cwd)
			os.Remove(dbPath)
			os.Remove(dbPath + "-wal")
			os.Remove(dbPath + "-shm")
			fmt.Println("Index deleted for current project.")
			return
		}
	}

	// Default: build/update index
	dbPath := codegraph.DefaultIndexPath(cwd)
	indexer, err := codegraph.NewIndexer(cwd, dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating indexer: %v\n", err)
		os.Exit(1)
	}
	defer indexer.Close()

	fmt.Printf("Indexing %s...\n", cwd)
	start := time.Now()
	if err := indexer.Update(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	elapsed := time.Since(start)

	fmt.Println(indexer.ProjectSummary())
	fmt.Printf("Completed in %s\n", elapsed.Round(time.Millisecond))
}

// generateMemorySlug creates a short slug from text for use as a memory name.
func generateMemorySlug(text string) string {
	slug := strings.ToLower(text)
	slug = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		if r == ' ' || r == '-' || r == '_' {
			return '-'
		}
		return -1
	}, slug)
	// Collapse multiple dashes
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	if len(slug) > 48 {
		slug = slug[:48]
		slug = strings.TrimRight(slug, "-")
	}
	if slug == "" {
		slug = fmt.Sprintf("memory-%d", time.Now().Unix())
	}
	return slug
}
