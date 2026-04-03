package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
		fmt.Printf("Resume session: %s (use `celeste chat` to start with this session)\n", args[0])
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
	fmt.Println("Plan mode is available in interactive chat.")
	fmt.Println("  /plan <goal>       Enter plan mode")
	fmt.Println("  /plan execute      Execute the plan")
	fmt.Println("  /plan show         Show current plan")
	fmt.Println("  /plan cancel       Cancel plan mode")
}

func runRevertCommand(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: celeste revert <file-path>")
		os.Exit(1)
	}
	fmt.Printf("File revert is available in interactive chat via /undo.\n")
	fmt.Printf("Standalone revert not yet implemented.\n")
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
