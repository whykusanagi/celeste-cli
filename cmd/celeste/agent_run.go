package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

type stringSliceFlag []string

func (s *stringSliceFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringSliceFlag) Set(value string) error {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	*s = append(*s, v)
	return nil
}

func runAgentCommand(args []string) {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	goal := fs.String("goal", "", "Task goal text")
	goalFile := fs.String("goal-file", "", "Path to a file containing task goal text")
	resume := fs.String("resume", "", "Resume an existing run by run id")
	listRuns := fs.Bool("list-runs", false, "List recent agent runs")
	evalFile := fs.String("eval", "", "Run evaluation cases from JSON file")
	benchmarkFile := fs.String("benchmark", "", "Run benchmark suite JSON file")
	benchmarkOut := fs.String("benchmark-out", "", "Write benchmark report JSON to this path")
	workspace := fs.String("workspace", "", "Workspace root for agent development tools (defaults to current directory)")
	artifactDir := fs.String("artifact-dir", "", "Directory where run artifact bundles are written")
	maxTurns := fs.Int("max-turns", 0, "Maximum agent turns")
	maxToolCalls := fs.Int("max-tool-calls", 0, "Maximum tool calls per turn")
	maxNoToolTurns := fs.Int("max-no-tool-turns", 0, "Maximum consecutive no-tool turns before stopping")
	requireMarker := fs.Bool("require-complete-marker", true, "Require completion marker in final response")
	completionMarker := fs.String("completion-marker", "TASK_COMPLETE:", "Completion marker token")
	requestTimeout := fs.Int("request-timeout", 0, "LLM request timeout in seconds")
	toolTimeout := fs.Int("tool-timeout", 0, "Tool execution timeout in seconds")
	verifyTimeout := fs.Int("verify-timeout", 0, "Verification command timeout in seconds")
	maxVerifyRetries := fs.Int("max-verify-retries", 0, "Maximum verification attempts before stopping")
	stopOnBlocker := fs.Bool("stop-on-blocker", true, "Stop run when assistant emits blocker marker")
	blockerMarker := fs.String("blocker-marker", "BLOCKED:", "Marker token for assistant blocker reports")
	enableMemory := fs.Bool("memory", true, "Enable cross-run project memory recall/writeback")
	memoryRecall := fs.Int("memory-recall", 0, "Number of memory entries to inject into run context")
	memoryMaxEntries := fs.Int("memory-max-entries", 0, "Maximum persisted memory entries per workspace")
	enablePlanning := fs.Bool("planner", true, "Enable explicit planning phase")
	planMaxSteps := fs.Int("plan-max-steps", 0, "Maximum steps extracted from planning phase")
	requireVerification := fs.Bool("require-verify", false, "Require verification commands to pass before completion")
	var verifyCommands stringSliceFlag
	fs.Var(&verifyCommands, "verify-cmd", "Verification command to run before completion (repeatable)")
	noArtifacts := fs.Bool("no-artifacts", false, "Disable per-run artifact bundle output")
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
	opts.EnablePlanning = *enablePlanning
	opts.PlanMaxSteps = *planMaxSteps
	opts.RequireVerification = *requireVerification
	opts.VerificationCommands = append(opts.VerificationCommands, verifyCommands...)
	opts.StopOnBlocker = *stopOnBlocker
	opts.BlockerMarker = strings.TrimSpace(*blockerMarker)
	opts.EnableMemory = *enableMemory
	opts.ArtifactDir = strings.TrimSpace(*artifactDir)
	opts.EmitArtifacts = !*noArtifacts
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
	if *verifyTimeout > 0 {
		opts.VerifyTimeout = time.Duration(*verifyTimeout) * time.Second
	}
	if *maxVerifyRetries > 0 {
		opts.MaxVerificationRetries = *maxVerifyRetries
	}
	if *memoryRecall > 0 {
		opts.MemoryRecallLimit = *memoryRecall
	}
	if *memoryMaxEntries > 0 {
		opts.MemoryMaxEntries = *memoryMaxEntries
	}

	runner, err := agent.NewRunner(cfg, opts, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating agent runner: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	if *benchmarkFile != "" {
		suite, err := agent.LoadBenchmarkSuite(*benchmarkFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading benchmark suite: %v\n", err)
			os.Exit(1)
		}
		report, err := runner.RunBenchmark(ctx, suite)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Benchmark failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Benchmark: %s\n", report.SuiteName)
		for _, result := range report.Results {
			fmt.Printf("- %s pass=%d/%d (%.2f%%) avg_turns=%.2f avg_tools=%.2f avg_ms=%.2f\n",
				result.CaseName,
				result.PassedIterations,
				result.Iterations,
				result.PassRate*100.0,
				result.AverageTurns,
				result.AverageToolCalls,
				result.AverageDurationMS)
			if len(result.FailureReasons) > 0 {
				fmt.Printf("  failures: %s\n", strings.Join(result.FailureReasons, "; "))
			}
		}
		fmt.Printf("\nBenchmark Summary: cases passed %d/%d\n", report.PassedCases, report.TotalCases)

		if strings.TrimSpace(*benchmarkOut) != "" {
			data, err := json.MarshalIndent(report, "", "  ")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error serializing benchmark report: %v\n", err)
				os.Exit(1)
			}
			if err := os.WriteFile(*benchmarkOut, data, 0644); err != nil {
				fmt.Fprintf(os.Stderr, "Error writing benchmark report: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Benchmark report written: %s\n", *benchmarkOut)
		}

		if report.FailedCases > 0 {
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
		fmt.Fprintln(os.Stderr, "       celeste agent --benchmark <suite.json>")
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
	if strings.TrimSpace(state.ArtifactBundlePath) != "" {
		fmt.Printf("Artifacts: %s\n", state.ArtifactBundlePath)
	}
	if strings.TrimSpace(state.BlockerReason) != "" {
		fmt.Printf("Blocker: %s\n", state.BlockerReason)
	}
	if len(state.MemoryContext) > 0 {
		fmt.Printf("Memory Context Entries: %d\n", len(state.MemoryContext))
	}
	if state.LastAssistantResponse != "" {
		fmt.Printf("\nFinal Response:\n%s\n", state.LastAssistantResponse)
	}
	if state.Error != "" {
		fmt.Printf("\nError: %s\n", state.Error)
	}
}
