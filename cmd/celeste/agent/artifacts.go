package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/privfs"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/shellrun"
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
	// Owner-only: the bundle holds the run's transcript, verification
	// output and the workspace diff.
	bundleDir := filepath.Join(baseDir, state.RunID)
	if err := privfs.MkdirAll(bundleDir); err != nil {
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
	if err := atomicfile.Write(filepath.Join(bundleDir, "summary.md"), []byte(summary), privfs.FilePerm); err != nil {
		return "", fmt.Errorf("write summary: %w", err)
	}

	gitStatus, gitDiff := captureGitWorkspaceArtifacts(state.Options.Workspace, state.Options.VerifyTimeout)
	if strings.TrimSpace(gitStatus) != "" {
		_ = atomicfile.Write(filepath.Join(bundleDir, "git_status.txt"), []byte(gitStatus), privfs.FilePerm)
	}
	if strings.TrimSpace(gitDiff) != "" {
		_ = atomicfile.Write(filepath.Join(bundleDir, "git_diff.patch"), []byte(gitDiff), privfs.FilePerm)
	}

	return bundleDir, nil
}

func resolveArtifactBaseDir(artifactDir string) (string, error) {
	isDefault := strings.TrimSpace(artifactDir) == ""
	if isDefault {
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
	// A new directory is owner-only; an existing --artifact-dir the user
	// chose keeps its mode, the default one under ~/.celeste is tightened.
	mkdir := func(dir string) error { return os.MkdirAll(dir, privfs.DirPerm) }
	if isDefault {
		mkdir = privfs.MkdirAll
	}
	if err := mkdir(abs); err != nil {
		return "", fmt.Errorf("create artifact dir: %w", err)
	}
	return abs, nil
}

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal json for %s: %w", filepath.Base(path), err)
	}
	if err := atomicfile.Write(path, data, privfs.FilePerm); err != nil {
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

	if out, err := runGit(workspace, timeout, "rev-parse", "--is-inside-work-tree"); err != nil || !strings.Contains(out, "true") {
		return "", ""
	}

	statusOut, _ := runGit(workspace, timeout, "status", "--porcelain")
	diffOut, _ := runGit(workspace, timeout, "diff", "--no-ext-diff")
	return statusOut, diffOut
}

// gitMaxOutput bounds one git call's output in memory. It is far above any
// realistic patch, so git_diff.patch is not cut; a var so tests can lower it.
var gitMaxOutput = 64 << 20

// runGit runs git directly (no shell) through shellrun: its own process
// group, killed whole on timeout, and a child of git still holding the
// output pipe after git exits is given shellrun.WaitDelay, then killed.
// Output over gitMaxOutput ends with a trailer saying it was cut, so a cut
// patch is never taken for a whole one.
func runGit(workdir string, timeout time.Duration, args ...string) (string, error) {
	res := shellrun.Run(context.Background(), shellrun.Options{Dir: workdir, Args: append([]string{"git"}, args...), Timeout: timeout, MaxOutput: gitMaxOutput})
	if res.Truncated {
		res.Output += fmt.Sprintf("\n# celeste: output truncated at %d bytes\n", gitMaxOutput)
	}
	switch {
	case res.TimedOut:
		return res.Output, fmt.Errorf("git %s timed out", strings.Join(args, " "))
	case res.Err != nil:
		return res.Output, res.Err
	case res.ExitCode != 0:
		return res.Output, fmt.Errorf("git %s: exit status %d", strings.Join(args, " "), res.ExitCode)
	}
	return res.Output, nil
}
