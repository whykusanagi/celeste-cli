package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type BenchmarkCase struct {
	Name           string   `json:"name"`
	Goal           string   `json:"goal"`
	Iterations     int      `json:"iterations,omitempty"`
	MaxTurns       int      `json:"max_turns,omitempty"`
	MustContain    []string `json:"must_contain,omitempty"`
	MustNotContain []string `json:"must_not_contain,omitempty"`
	VerifyCommands []string `json:"verify_commands,omitempty"`
}

type BenchmarkSuite struct {
	Name       string          `json:"name,omitempty"`
	Iterations int             `json:"iterations,omitempty"`
	Cases      []BenchmarkCase `json:"cases"`
}

type BenchmarkResult struct {
	CaseName          string   `json:"case_name"`
	Iterations        int      `json:"iterations"`
	PassedIterations  int      `json:"passed_iterations"`
	FailedIterations  int      `json:"failed_iterations"`
	PassRate          float64  `json:"pass_rate"`
	AverageTurns      float64  `json:"average_turns"`
	AverageToolCalls  float64  `json:"average_tool_calls"`
	AverageDurationMS float64  `json:"average_duration_ms"`
	LastStatus        string   `json:"last_status"`
	FailureReasons    []string `json:"failure_reasons,omitempty"`
}

type BenchmarkReport struct {
	SuiteName   string            `json:"suite_name"`
	GeneratedAt time.Time         `json:"generated_at"`
	TotalCases  int               `json:"total_cases"`
	PassedCases int               `json:"passed_cases"`
	FailedCases int               `json:"failed_cases"`
	Results     []BenchmarkResult `json:"results"`
}

type benchmarkIteration struct {
	Passed    bool
	Reason    string
	Status    string
	Turns     int
	ToolCalls int
	Duration  time.Duration
}

func LoadBenchmarkSuite(path string) (BenchmarkSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return BenchmarkSuite{}, err
	}

	var suite BenchmarkSuite
	if err := json.Unmarshal(data, &suite); err == nil && len(suite.Cases) > 0 {
		normalizeBenchmarkSuite(&suite, path)
		return suite, nil
	}

	var direct []BenchmarkCase
	if err := json.Unmarshal(data, &direct); err != nil {
		return BenchmarkSuite{}, fmt.Errorf("parse benchmark file: %w", err)
	}

	suite = BenchmarkSuite{Cases: direct}
	normalizeBenchmarkSuite(&suite, path)
	return suite, nil
}

func normalizeBenchmarkSuite(suite *BenchmarkSuite, path string) {
	if suite == nil {
		return
	}
	if strings.TrimSpace(suite.Name) == "" {
		suite.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	if suite.Iterations <= 0 {
		suite.Iterations = 1
	}
}

func (r *Runner) RunBenchmark(ctx context.Context, suite BenchmarkSuite) (*BenchmarkReport, error) {
	normalizeBenchmarkSuite(&suite, suite.Name)

	report := &BenchmarkReport{
		SuiteName:   suite.Name,
		GeneratedAt: time.Now(),
		TotalCases:  len(suite.Cases),
		Results:     make([]BenchmarkResult, 0, len(suite.Cases)),
	}

	for _, c := range suite.Cases {
		caseName := strings.TrimSpace(c.Name)
		if caseName == "" {
			caseName = strings.TrimSpace(c.Goal)
		}
		if caseName == "" {
			caseName = "unnamed_case"
		}

		iterations := c.Iterations
		if iterations <= 0 {
			iterations = suite.Iterations
		}
		if iterations <= 0 {
			iterations = 1
		}

		runs := make([]benchmarkIteration, 0, iterations)
		for i := 0; i < iterations; i++ {
			iterRunner := *r
			opts := r.options
			opts.DisableCheckpoints = true
			opts.EmitArtifacts = false
			opts.EnableMemory = false
			opts.Verbose = false
			if c.MaxTurns > 0 {
				opts.MaxTurns = c.MaxTurns
			}
			if len(c.VerifyCommands) > 0 {
				opts.RequireVerification = true
				opts.VerificationCommands = append([]string(nil), c.VerifyCommands...)
			}
			iterRunner.options = opts

			start := time.Now()
			state, err := iterRunner.RunGoal(ctx, c.Goal)
			duration := time.Since(start)
			if err != nil {
				runs = append(runs, benchmarkIteration{
					Passed:   false,
					Reason:   err.Error(),
					Status:   StatusFailed,
					Duration: duration,
				})
				continue
			}

			evalCase := EvalCase{
				Name:           c.Name,
				Goal:           c.Goal,
				MustContain:    c.MustContain,
				MustNotContain: c.MustNotContain,
			}
			passed, reason := evaluateCase(evalCase, state.Status, strings.TrimSpace(state.LastAssistantResponse))
			runs = append(runs, benchmarkIteration{
				Passed:    passed,
				Reason:    reason,
				Status:    state.Status,
				Turns:     state.Turn,
				ToolCalls: state.ToolCallCount,
				Duration:  duration,
			})
		}

		result := aggregateBenchmarkCase(caseName, runs)
		report.Results = append(report.Results, result)
		if result.FailedIterations == 0 {
			report.PassedCases++
		} else {
			report.FailedCases++
		}
	}

	return report, nil
}

func aggregateBenchmarkCase(caseName string, runs []benchmarkIteration) BenchmarkResult {
	result := BenchmarkResult{
		CaseName:   caseName,
		Iterations: len(runs),
	}
	if len(runs) == 0 {
		return result
	}

	var turnsSum int
	var toolsSum int
	var durationSum time.Duration
	failures := make([]string, 0)

	for _, run := range runs {
		if run.Passed {
			result.PassedIterations++
		} else {
			result.FailedIterations++
			if strings.TrimSpace(run.Reason) != "" {
				failures = append(failures, run.Reason)
			}
		}
		result.LastStatus = run.Status
		turnsSum += run.Turns
		toolsSum += run.ToolCalls
		durationSum += run.Duration
	}

	if result.Iterations > 0 {
		result.PassRate = float64(result.PassedIterations) / float64(result.Iterations)
		result.AverageTurns = float64(turnsSum) / float64(result.Iterations)
		result.AverageToolCalls = float64(toolsSum) / float64(result.Iterations)
		result.AverageDurationMS = float64(durationSum.Milliseconds()) / float64(result.Iterations)
	}

	result.FailureReasons = uniqueStrings(failures)
	return result
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
