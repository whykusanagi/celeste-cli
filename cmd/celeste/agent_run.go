package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

func runAgentCommand(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	goal := fs.String("goal", "", "Task goal text")
	goalFile := fs.String("goal-file", "", "Path to a file containing task goal text")
	resume := fs.String("resume", "", "Resume an existing run by run id")
	listRuns := fs.Bool("list-runs", false, "List recent agent runs")
	evalFile := fs.String("eval", "", "Run evaluation cases from JSON file")
	workspace := fs.String("workspace", "", "Workspace root for agent development tools (defaults to current directory)")
	maxTurns := fs.Int("max-turns", 0, "Maximum agent turns")
	maxToolCalls := fs.Int("max-tool-calls", 0, "Maximum tool calls per turn")
	maxNoToolTurns := fs.Int("max-no-tool-turns", 0, "Maximum consecutive no-tool turns before stopping")
	requireMarker := fs.Bool("require-complete-marker", true, "Require completion marker in final response")
	completionMarker := fs.String("completion-marker", "TASK_COMPLETE:", "Completion marker token")
	requestTimeout := fs.Int("request-timeout", 0, "LLM request timeout in seconds")
	toolTimeout := fs.Int("tool-timeout", 0, "Tool execution timeout in seconds")
	verbose := fs.Bool("verbose", true, "Print turn-by-turn output")
	noCheckpoint := fs.Bool("no-checkpoint", false, "Disable checkpoint persistence for this run")

	_ = fs.Parse(args)

	if *listRuns {
		store, err := agent.NewCheckpointStore("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening run store: %v\n", err)
			os.Exit(1)
		}
		runs, err := store.List(20)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing runs: %v\n", err)
			os.Exit(1)
		}
		if len(runs) == 0 {
			fmt.Println("No agent runs found")
			return
		}

		fmt.Printf("Recent Agent Runs (%d):\n", len(runs))
		for _, r := range runs {
			goalPreview := strings.TrimSpace(r.Goal)
			if len(goalPreview) > 60 {
				goalPreview = goalPreview[:60] + "..."
			}
			fmt.Printf("- %s [%s] turns=%d tools=%d updated=%s\n  goal: %s\n",
				r.RunID, r.Status, r.Turn, r.ToolCalls, r.UpdatedAt.Format("2006-01-02 15:04:05"), goalPreview)
		}
		return
	}

	cfg, err := config.LoadNamed(configName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}

	if cfg.APIKey == "" && !cfg.GoogleUseADC && cfg.GoogleCredentialsFile == "" {
		fmt.Fprintln(os.Stderr, "No API key or Google ADC credentials configured.")
		os.Exit(1)
	}

	opts := agent.DefaultOptions()
	opts.Workspace = *workspace
	opts.RequireCompletionMarker = *requireMarker
	opts.CompletionMarker = strings.TrimSpace(*completionMarker)
	opts.DisableCheckpoints = *noCheckpoint
	opts.Verbose = *verbose
	if *maxTurns > 0 {
		opts.MaxTurns = *maxTurns
	}
	if *maxToolCalls > 0 {
		opts.MaxToolCallsPerTurn = *maxToolCalls
	}
	if *maxNoToolTurns > 0 {
		opts.MaxConsecutiveNoToolTurns = *maxNoToolTurns
	}
	if *requestTimeout > 0 {
		opts.RequestTimeout = time.Duration(*requestTimeout) * time.Second
	}
	if *toolTimeout > 0 {
		opts.ToolTimeout = time.Duration(*toolTimeout) * time.Second
	}

	runner, err := agent.NewRunner(cfg, opts, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent runner: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	if *evalFile != "" {
		cases, err := agent.LoadEvalCases(*evalFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading eval cases: %v\n", err)
			os.Exit(1)
		}
		results, err := runner.RunEval(ctx, cases)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Eval failed: %v\n", err)
			os.Exit(1)
		}

		passed := 0
		for _, result := range results {
			status := "FAIL"
			if result.Passed {
				status = "PASS"
				passed++
			}
			fmt.Printf("[%s] %s (%s) - %s\n", status, result.CaseName, result.Status, result.Reason)
		}
		fmt.Printf("\nEval Summary: %d/%d passed\n", passed, len(results))
		if passed != len(results) {
			os.Exit(1)
		}
		return
	}

	if *resume != "" {
		state, err := runner.Resume(ctx, *resume)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Resume failed: %v\n", err)
			os.Exit(1)
		}
		printRunSummary(state)
		if state.Status != agent.StatusCompleted {
			os.Exit(1)
		}
		return
	}

	finalGoal := strings.TrimSpace(*goal)
	if *goalFile != "" {
		data, err := os.ReadFile(*goalFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading goal file: %v\n", err)
			os.Exit(1)
		}
		if finalGoal != "" {
			finalGoal += "\n\n"
		}
		finalGoal += strings.TrimSpace(string(data))
	}
	if finalGoal == "" {
		finalGoal = strings.TrimSpace(strings.Join(fs.Args(), " "))
	}

	if finalGoal == "" {
		fmt.Fprintln(os.Stderr, "Usage: celeste agent --goal \"<task>\" [--workspace <path>] [--max-turns N]")
		fmt.Fprintln(os.Stderr, "       celeste agent --resume <run-id>")
		fmt.Fprintln(os.Stderr, "       celeste agent --list-runs")
		fmt.Fprintln(os.Stderr, "       celeste agent --eval <cases.json>")
		os.Exit(1)
	}

	state, err := runner.RunGoal(ctx, finalGoal)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Agent failed: %v\n", err)
		if state != nil {
			printRunSummary(state)
		}
		os.Exit(1)
	}

	printRunSummary(state)
	if state.Status != agent.StatusCompleted {
		os.Exit(1)
	}
}

func printRunSummary(state *agent.RunState) {
	if state == nil {
		return
	}
	fmt.Printf("\nRun ID: %s\n", state.RunID)
	fmt.Printf("Status: %s\n", state.Status)
	fmt.Printf("Turns: %d\n", state.Turn)
	fmt.Printf("Tool Calls: %d\n", state.ToolCallCount)
	if state.LastAssistantResponse != "" {
		fmt.Printf("\nFinal Response:\n%s\n", state.LastAssistantResponse)
	}
	if state.Error != "" {
		fmt.Printf("\nError: %s\n", state.Error)
	}
}
