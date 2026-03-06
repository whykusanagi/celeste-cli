package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func (r *Runner) persistArtifacts(state *RunState) {
	if state == nil || !state.Options.EmitArtifacts {
		return
	}

	bundlePath, err := writeArtifactBundle(state)
	if err != nil {
		fmt.Fprintf(r.errOut, "Warning: failed to write artifact bundle: %v\n", err)
		return
	}
	state.ArtifactBundlePath = bundlePath
}

func writeArtifactBundle(state *RunState) (string, error) {
	if state == nil {
		return "", fmt.Errorf("run state is nil")
	}

	baseDir, err := resolveArtifactBaseDir(state.Options.ArtifactDir)
	if err != nil {
		return "", err
	}
	bundleDir := filepath.Join(baseDir, state.RunID)
	if err := os.MkdirAll(bundleDir, 0755); err != nil {
		return "", fmt.Errorf("create artifact bundle dir: %w", err)
	}

	state.ArtifactBundlePath = bundleDir
	if err := writeJSON(filepath.Join(bundleDir, "run_state.json"), state); err != nil {
		return "", err
	}
	if err := writeJSON(filepath.Join(bundleDir, "plan.json"), state.Plan); err != nil {
		return "", err
	}
	if err := writeJSON(filepath.Join(bundleDir, "steps.json"), state.Steps); err != nil {
		return "", err
	}
	if err := writeJSON(filepath.Join(bundleDir, "verification.json"), state.Verification); err != nil {
		return "", err
	}

	summary := renderArtifactSummary(state)
	if err := os.WriteFile(filepath.Join(bundleDir, "summary.md"), []byte(summary), 0644); err != nil {
		return "", fmt.Errorf("write summary: %w", err)
	}

	gitStatus, gitDiff := captureGitWorkspaceArtifacts(state.Options.Workspace, state.Options.VerifyTimeout)
	if strings.TrimSpace(gitStatus) != "" {
		_ = os.WriteFile(filepath.Join(bundleDir, "git_status.txt"), []byte(gitStatus), 0644)
	}
	if strings.TrimSpace(gitDiff) != "" {
		_ = os.WriteFile(filepath.Join(bundleDir, "git_diff.patch"), []byte(gitDiff), 0644)
	}

	return bundleDir, nil
}

func resolveArtifactBaseDir(artifactDir string) (string, error) {
	if strings.TrimSpace(artifactDir) == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve home dir: %w", err)
		}
		artifactDir = filepath.Join(homeDir, ".celeste", "agent", "artifacts")
	}

	abs, err := filepath.Abs(artifactDir)
	if err != nil {
		return "", fmt.Errorf("resolve artifact dir: %w", err)
	}
	if err := os.MkdirAll(abs, 0755); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	return abs, nil
}

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json for %s: %w", filepath.Base(path), err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), err)
	}
	return nil
}

func renderArtifactSummary(state *RunState) string {
	var b strings.Builder
	b.WriteString("# Agent Run Summary\n\n")
	b.WriteString(fmt.Sprintf("- Run ID: `%s`\n", state.RunID))
	b.WriteString(fmt.Sprintf("- Status: `%s`\n", state.Status))
	b.WriteString(fmt.Sprintf("- Goal: %s\n", state.Goal))
	b.WriteString(fmt.Sprintf("- Created: %s\n", state.CreatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- Updated: %s\n", state.UpdatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- Turns: %d\n", state.Turn))
	b.WriteString(fmt.Sprintf("- Tool calls: %d\n", state.ToolCallCount))
	b.WriteString(fmt.Sprintf("- Phase: %s\n", state.Phase))
	b.WriteString("\n")

	if len(state.Plan) > 0 {
		b.WriteString("## Plan\n")
		for _, step := range state.Plan {
			b.WriteString(fmt.Sprintf("- [%s] %d. %s\n", step.Status, step.Index, step.Title))
		}
		b.WriteString("\n")
	}

	if len(state.Verification) > 0 {
		b.WriteString("## Verification\n")
		for _, check := range state.Verification {
			status := "FAIL"
			if check.Passed {
				status = "PASS"
			}
			b.WriteString(fmt.Sprintf("- [%s] `%s` (exit=%d timed_out=%v)\n", status, check.Command, check.ExitCode, check.TimedOut))
		}
		b.WriteString("\n")
	}

	if strings.TrimSpace(state.LastAssistantResponse) != "" {
		b.WriteString("## Final Response\n\n")
		b.WriteString(state.LastAssistantResponse)
		b.WriteString("\n")
	}

	if strings.TrimSpace(state.Error) != "" {
		b.WriteString("\n## Error\n\n")
		b.WriteString(state.Error)
		b.WriteString("\n")
	}

	return b.String()
}

func captureGitWorkspaceArtifacts(workspace string, timeout time.Duration) (string, string) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", ""
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	if out, err := runShellCommand(workspace, timeout, "git rev-parse --is-inside-work-tree"); err != nil || !strings.Contains(out, "true") {
		return "", ""
	}

	statusOut, _ := runShellCommand(workspace, timeout, "git status --porcelain")
	diffOut, _ := runShellCommand(workspace, timeout, "git diff --no-ext-diff")
	return statusOut, diffOut
}

func runShellCommand(workdir string, timeout time.Duration, command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = workdir
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("command timed out: %s", command)
	}
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}
