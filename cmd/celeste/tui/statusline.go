// Package tui — statusline.go holds the segmented bottom status line
// (git, project, model, effort, permission mode, session), its git poll
// plumbing, and the contextual key-hint row.
package tui

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
)

// StatusLineModel renders a single-line segmented status bar. All fields are
// pushed in via setters; View() never performs I/O.
type StatusLineModel struct {
	branch     string
	dirtyCount int
	ahead      int
	behind     int
	project    string
	model      string
	effort     string
	permMode   string
	session    string
	width      int
}

// NewStatusLineModel creates an empty status line.
func NewStatusLineModel() StatusLineModel { return StatusLineModel{} }

// SetWidth sets the render width.
func (m StatusLineModel) SetWidth(w int) StatusLineModel { m.width = w; return m }

// SetGit sets the git segment values.
func (m StatusLineModel) SetGit(branch string, dirty, ahead, behind int) StatusLineModel {
	m.branch, m.dirtyCount, m.ahead, m.behind = branch, dirty, ahead, behind
	return m
}

// SetProject sets the project/directory segment.
func (m StatusLineModel) SetProject(p string) StatusLineModel { m.project = p; return m }

// SetModel sets the active model segment.
func (m StatusLineModel) SetModel(model string) StatusLineModel { m.model = model; return m }

// SetEffort sets the reasoning-effort segment ("off" hides it).
func (m StatusLineModel) SetEffort(effort string) StatusLineModel { m.effort = effort; return m }

// SetPermMode sets the permission-mode segment ("default"/"strict"/"trust").
func (m StatusLineModel) SetPermMode(mode string) StatusLineModel { m.permMode = mode; return m }

// SetSession sets the session-name segment.
func (m StatusLineModel) SetSession(name string) StatusLineModel { m.session = name; return m }

// permStyle colors the permission mode by risk.
func permStyle(mode string) lipgloss.Style {
	switch mode {
	case "trust":
		return lipgloss.NewStyle().Foreground(ColorWarning)
	case "strict":
		return lipgloss.NewStyle().Foreground(ColorError)
	default: // "default"
		return lipgloss.NewStyle().Foreground(ColorSuccess)
	}
}

// gitSegment builds the styled git segment, or "" when no branch is known.
func (m StatusLineModel) gitSegment() string {
	if m.branch == "" {
		return ""
	}
	seg := lipgloss.NewStyle().Foreground(ColorPurple).Render("⎇ " + m.branch)
	if m.dirtyCount > 0 {
		seg += lipgloss.NewStyle().Foreground(ColorWarning).Render(fmt.Sprintf(" ✎%d", m.dirtyCount))
	}
	if m.ahead > 0 {
		seg += lipgloss.NewStyle().Foreground(ColorSuccess).Render(fmt.Sprintf(" ↑%d", m.ahead))
	}
	if m.behind > 0 {
		seg += lipgloss.NewStyle().Foreground(ColorError).Render(fmt.Sprintf(" ↓%d", m.behind))
	}
	return seg
}

// View renders the segmented status line. Below 80 columns it keeps only the
// git and model segments so the bar never wraps.
func (m StatusLineModel) View() string {
	if m.width == 0 {
		return ""
	}
	muted := lipgloss.NewStyle().Foreground(ColorTextMuted)
	sep := muted.Render(" │ ")

	if m.width < 80 {
		var narrow []string
		if g := m.gitSegment(); g != "" {
			narrow = append(narrow, g)
		}
		if m.model != "" {
			narrow = append(narrow, ModelStyle.Render(m.model))
		}
		return StatusBarStyle.Width(m.width).Render(strings.Join(narrow, sep))
	}

	var segs []string
	if g := m.gitSegment(); g != "" {
		segs = append(segs, g)
	}
	if m.project != "" {
		segs = append(segs, muted.Render(m.project))
	}
	if m.model != "" {
		segs = append(segs, ModelStyle.Render(m.model))
	}
	if m.effort != "" && m.effort != "off" {
		segs = append(segs, muted.Render("effort:"+m.effort))
	}
	if m.permMode != "" {
		segs = append(segs, permStyle(m.permMode).Render(m.permMode))
	}
	if m.session != "" {
		segs = append(segs, EndpointStyle.Render(m.session))
	}
	return StatusBarStyle.Width(m.width).Render(strings.Join(segs, sep))
}

// parseDirtyCount counts changed files in `git status --short` output.
func parseDirtyCount(status string) int {
	n := 0
	for _, line := range strings.Split(status, "\n") {
		if strings.TrimSpace(line) != "" {
			n++
		}
	}
	return n
}

// parseAheadBehind parses `git rev-list --left-right --count @{u}...HEAD`
// output, which is "<behind>\t<ahead>". Returns (0,0) on any parse failure.
func parseAheadBehind(out string) (ahead, behind int) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 2 {
		return 0, 0
	}
	behind, _ = strconv.Atoi(fields[0])
	ahead, _ = strconv.Atoi(fields[1])
	return ahead, behind
}

// gitAheadBehind runs git to count commits ahead/behind the upstream.
// Returns (0,0) when there is no upstream or git fails.
func gitAheadBehind(workDir string) (ahead, behind int) {
	cmd := exec.Command("git", "rev-list", "--left-right", "--count", "@{u}...HEAD")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return 0, 0
	}
	return parseAheadBehind(string(out))
}

const gitPollInterval = 3 * time.Second

// GitStatusMsg carries a refreshed git snapshot into the Update loop.
type GitStatusMsg struct {
	Repo   bool
	Branch string
	Dirty  int
	Ahead  int
	Behind int
}

// gitStatus computes the current git snapshot (blocking; call off the UI loop).
func gitStatus(workDir string) GitStatusMsg {
	snap := grimoire.CaptureGitSnapshot(workDir)
	if snap == nil {
		return GitStatusMsg{Repo: false}
	}
	ahead, behind := gitAheadBehind(workDir)
	return GitStatusMsg{
		Repo:   true,
		Branch: snap.Branch,
		Dirty:  parseDirtyCount(snap.Status),
		Ahead:  ahead,
		Behind: behind,
	}
}

// gitFetchCmd fetches git status immediately (used at startup).
func gitFetchCmd(workDir string) tea.Cmd {
	return func() tea.Msg { return gitStatus(workDir) }
}

// gitPollCmd fetches git status after gitPollInterval; the handler re-arms it.
func gitPollCmd(workDir string) tea.Cmd {
	return tea.Tick(gitPollInterval, func(time.Time) tea.Msg { return gitStatus(workDir) })
}

// hintsFor returns the contextual key-hint row for the current view.
func hintsFor(mode string, mcpActive bool) string {
	if mcpActive {
		return "↑↓ move · ↵ select · c connect · d disconnect · r reconnect · esc close"
	}
	switch mode {
	case "skills", "sessions", "graph", "memories", "collections", "menu", "persona":
		return "↑↓ move · ↵ select · esc close"
	default:
		return "↵ send · ⇧↵ newline · / commands · PgUp/PgDn scroll · Ctrl+C exit"
	}
}
