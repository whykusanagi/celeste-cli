package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

var updateSplitGolden = flag.Bool("update-split", false, "rewrite the split-view snapshots in testdata")

// splitViewWithPrompt drives a /orch run to a permission prompt the way
// the audit saw it live (V21): classified, the primary agent, a bash call,
// tool output, and the prompt for printf.
func splitViewWithPrompt(t *testing.T, w, h int) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var m tea.Model = NewApp(nil)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = m.Update(OrchestratorEventMsg{Kind: 0, Lane: "unknown", Text: "no lane matched · default model"})
	m, _ = m.Update(OrchestratorEventMsg{Kind: 1, Lane: "unknown", Model: "fugu", Text: "primary agent"})
	m, _ = m.Update(OrchestratorEventMsg{Kind: 1, Model: "fugu", Text: "turn 1/50"})
	m, _ = m.Update(OrchestratorEventMsg{Kind: 1, Model: "fugu", Text: "↩ turn 1 with a long summary line that runs well past the width of the left pane at either size", Response: "I will print a greeting.\n" + strings.Repeat("a very long line of model output that must be cut at the pane edge ", 4)})
	m, _ = m.Update(OrchestratorEventMsg{Kind: 2, Text: "⚙ bash"})
	m, _ = m.Update(PermissionRequestMsg{ToolName: "bash", InputSummary: `printf 'hi\n'`, RiskLevel: "write", Response: make(chan PermissionResponse, 1)})
	return m.View()
}

// V21: at 120x40 the split view lost its header and the pane tops, the
// right pane ran past the screen edge, and a permission prompt pushed the
// input off the screen. The view must fit the terminal exactly, keep the
// header, both panes' borders, the prompt, the status and the input.
func TestSplitViewFitsWithPermissionPrompt(t *testing.T) {
	for _, sz := range [][2]int{{120, 40}, {80, 24}} {
		w, h := sz[0], sz[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			view := splitViewWithPrompt(t, w, h)
			lines := strings.Split(view, "\n")
			if len(lines) > h {
				t.Errorf("view is %d rows, terminal has %d", len(lines), h)
			}
			for i, l := range lines {
				if lw := ansi.StringWidth(l); lw > w {
					t.Errorf("row %d is %d columns wide, terminal has %d: %q", i, lw, w, ansi.Strip(l))
				}
			}
			plain := ansi.Strip(view)
			for _, want := range []string{"Celeste CLI", "AGENT ACTIONS", "code output", "Permission Required", "printf 'hi\\n'", "Risk: write", "Type a message"} {
				if !strings.Contains(plain, want) {
					t.Errorf("view lacks %q", want)
				}
			}
			if !strings.Contains(ansi.Strip(lines[0]), "Celeste CLI") {
				t.Errorf("first row is not the header: %q", ansi.Strip(lines[0]))
			}
			// Both panes keep their four corners, the right one inside the
			// last column.
			var tops, bottoms int
			for _, l := range strings.Split(plain, "\n") {
				l = strings.TrimRight(l, " ")
				if strings.HasPrefix(l, "╭") && strings.Contains(l, "╮╭") && strings.HasSuffix(l, "╮") {
					tops++
				}
				if strings.HasPrefix(l, "╰") && strings.Contains(l, "╯╰") && strings.HasSuffix(l, "╯") {
					bottoms++
				}
			}
			if tops != 1 || bottoms != 1 {
				t.Errorf("pane borders: %d top rows and %d bottom rows, want 1 each:\n%s", tops, bottoms, plain)
			}
			checkSplitGolden(t, fmt.Sprintf("split_view_%dx%d.golden", w, h), plain)
		})
	}
}

// checkSplitGolden compares the plain view, trailing spaces trimmed, with
// testdata/name (go test -run SplitView -update-split rewrites it).
func checkSplitGolden(t *testing.T, name, plain string) {
	t.Helper()
	rows := strings.Split(plain, "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	got := strings.Join(rows, "\n") + "\n"
	path := filepath.Join("testdata", name)
	if *updateSplitGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot (run with -update-split to create it): %v", err)
	}
	// A Windows checkout converts the golden to CRLF (core.autocrlf); the
	// rendered view itself uses LF, so compare the text.
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Errorf("split view differs from %s:\n--- got\n%s--- want\n%s", path, got, want)
	}
}

// A terminal too short for the header, a panel, the prompt and the input
// drops the header first; the prompt and the input stay on screen.
func TestSplitViewShortTerminalKeepsPromptAndInput(t *testing.T) {
	view := splitViewWithPrompt(t, 80, 16)
	if n := strings.Count(view, "\n") + 1; n > 16 {
		t.Errorf("view is %d rows, terminal has 16", n)
	}
	plain := ansi.Strip(view)
	for _, want := range []string{"Permission Required", "[D] Always deny", "Type a message", "AGENT ACTIONS"} {
		if !strings.Contains(plain, want) {
			t.Errorf("view lacks %q:\n%s", want, plain)
		}
	}
}
