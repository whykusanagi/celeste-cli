package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// tuiSnap is one named, rendered TUI component view.
type tuiSnap struct {
	name    string // filesystem-safe slug (also the .ansi/.png basename)
	title   string // human label
	content string // rendered View() output
}

// renderComponents builds representative states for every TUI component added
// in the mcp-connectivity sprint and returns their rendered View() output.
// Constructing + rendering each here is itself the smoke test: a broken render
// path panics.
func renderComponents() []tuiSnap {
	base := time.Now()

	askSingle := NewAskPromptModel()
	askSingle.SetSize(72, 0)
	askSingle, _ = askSingle.Update(AskRequestMsg{
		Question: "Which migration strategy should I use?",
		Options: []AskOption{
			{Label: "In-place ALTER", Description: "fastest, brief lock"},
			{Label: "Shadow table + backfill", Description: "zero-downtime, slower"},
			{Label: "Blue/green swap", Description: "safest, needs 2x storage"},
		},
		Response: make(chan AskResponseMsg, 1),
	})

	askMulti := NewAskPromptModel()
	askMulti.SetSize(72, 0)
	askMulti, _ = askMulti.Update(AskRequestMsg{
		Question:    "Which platforms should CI cover?",
		MultiSelect: true,
		Options:     []AskOption{{Label: "ubuntu-latest"}, {Label: "macos-latest"}, {Label: "windows-latest"}},
		Response:    make(chan AskResponseMsg, 1),
	})
	askMulti, _ = askMulti.Update(tea.KeyMsg{Type: tea.KeySpace}) // check the first box

	panel := NewMCPPanelModel()
	panel.SetSize(72, 20)
	panel.servers = []MCPServerInfo{
		{Name: "celeste", Transport: "stdio", Connected: true, ToolCount: 8, Enabled: true, Origin: "/Users/you/.celeste/mcp.json"},
		{Name: "x-bridge", Transport: "http", Connected: false, Enabled: true, Origin: "/Users/you/.celeste/mcp.json"},
		{Name: "github", Transport: "stdio", Connected: false, Enabled: false, Origin: "/Users/you/.cursor/mcp.json"},
	}
	panel.active = true

	tp := ToolProgressModel{
		width: 72,
		entries: []toolProgressEntry{
			{name: "read_file", state: "executing", message: "read cmd/celeste/tui/app.go", startedAt: base},
			{name: "search", state: "done", message: "12 matches for \"StatusLineModel\"", startedAt: base.Add(-1200 * time.Millisecond), doneAt: base},
			{name: "bash", state: "failed", message: "exit 1: go vet ./...", startedAt: base.Add(-800 * time.Millisecond), doneAt: base},
			{name: "agent", displayName: "mizu", element: "water", state: "executing", subMessage: "spawning sub-task 2/3", startedAt: base},
		},
	}

	hints := fmt.Sprintf("chat:  %s\npanel: %s\nmcp:   %s",
		hintsFor("chat", false), hintsFor("skills", false), hintsFor("chat", true))

	return []tuiSnap{
		{"b1-statusline-wide", "B1 status line (160 cols)", NewStatusLineModel().
			SetWidth(160).SetGit("feat/mcp-connectivity", 3, 2, 1).SetProject("celeste-cli").
			SetModel("fugu").SetEffort("high").SetPermMode("trust").SetSession("night-session").View()},
		{"b1-statusline-narrow", "B1 status line (40 cols, narrowed)", NewStatusLineModel().
			SetWidth(40).SetGit("main", 0, 0, 0).SetModel("fugu").SetSession("night-session").View()},
		{"b2-key-hints", "B2 contextual key hints", hints},
		{"b3-tool-cards", "B3 tool-call cards", tp.View()},
		{"a3-ask-single", "A3 ask prompt (single select)", askSingle.View()},
		{"a3-ask-multi", "A3 ask prompt (multi select)", askMulti.View()},
		{"a2-mcp-panel", "A2 /mcp panel", panel.View()},
		{"full-chat-frame", "Full TUI (chat view)", fullChatFrame(100, 30)},
		{"skills-browser", "Skills browser (paginated)", skillsBrowserFrame("", 24)},
		{"skills-browser-search", "Skills browser (type-ahead search)", skillsBrowserFrame("task", 24)},
	}
}

// skillsBrowserFrame renders the skills browser over a representative tool list,
// optionally with an active filter query, at the given height.
func skillsBrowserFrame(query string, height int) string {
	// Realistic descriptions (mirrors real MCP tool copy) so the snapshot is
	// representative rather than repeating one placeholder string.
	catalog := [][2]string{
		{"asset_add", "Register an asset (image/video) in the library and return its id"},
		{"asset_list", "List assets, newest first; filter by tag or kind"},
		{"backup_run", "Trigger a manual encrypted backup of the local database now"},
		{"calendar_get_month", "Return the stream calendar for a month with events per day"},
		{"document_create", "Create a foldered document (spec, plan, note) and return its id"},
		{"document_get", "Look up a single document by id with its comments and decisions"},
		{"document_update", "Patch a document's title, body, tags, folder, pin, or archive state"},
		{"milestone_create", "Add a milestone with a target date to track a goal"},
		{"pipeline_create", "Create a content piece in the idea→post pipeline"},
		{"pipeline_move", "Advance a content item to the next production stage"},
		{"scheduled_post_create", "Draft/queue a social post; approve by setting status to ready"},
		{"search", "Search for text in workspace files and return matching lines"},
		{"settings_get", "Read current app settings (auto_backup, auto_backup_hour, …)"},
		{"spawn_agent", "Spawn a subagent to handle a subtask and return its result"},
		{"stream_event_create", "Create a stream day entry (subathon|stream|special)"},
		{"tags_list", "Return every unique tag across documents and tasks with counts"},
		{"tarot_reading", "Generate a tarot reading with a three-card or single-card spread"},
		{"task_create", "Create a task. RULE: max 3 P1s in todo|doing per due_date"},
		{"task_delete", "Permanently delete a task by id. Irreversible — no soft delete"},
		{"task_update", "Patch any field on a task; complete with {status:'done'}"},
		{"tasks_create_batch", "Create 1–100 tasks in one call, each with the task fields"},
		{"tasks_list", "List tasks {count, tasks[]}; filter by area, status, priority, repo"},
		{"tasks_search", "Full-text search across task titles, descriptions, tags, and repo"},
		{"tasks_unblocked", "Return todo tasks whose every blocked_by id resolves to done"},
		{"web_fetch", "Fetch a URL and convert its HTML content to markdown"},
		{"web_search", "Search the web via DuckDuckGo and return up to 10 results"},
		{"write_file", "Write text to a workspace file, creating parent directories"},
	}
	skills := make([]SkillDefinition, len(catalog))
	for i, c := range catalog {
		skills[i] = SkillDefinition{Name: c[0], Description: c[1]}
	}
	sb := NewSkillsBrowserModel(skills)
	sb.width, sb.height = 90, height
	sb.query = query
	sb.applyFilter()
	// nudge the cursor down a few rows so the position indicator is visible
	for range 5 {
		updated, _ := sb.Update(tea.KeyMsg{Type: tea.KeyDown})
		sb = updated.(SkillsBrowserModel)
	}
	return sb.View()
}

// fullChatFrame builds a realistic AppModel — sized, with a short conversation —
// and returns its composed View(): the whole assembled TUI (header, chat,
// input, skills panel, status line, hints, status bar) as one frame.
func fullChatFrame(w, h int) string {
	app := NewApp(nil).SetVersion("1.12.1", "test").WithEndpoint("sakana").SetWorkDir(".")
	app.model = "fugu"
	app.effort = "high"
	app.skills = app.skills.SetConfig("sakana", "fugu", true, false, 47, "")

	sized, _ := app.Update(tea.WindowSizeMsg{Width: w, Height: h})
	app = sized.(AppModel)

	app.chat = app.chat.AddUserMessage("Review shapes.py for stub findings")
	app.chat = app.chat.AddAssistantMessage("Looked at `shapes.py` — the abstract methods are **fine**, darling. `Drawable.draw`, `Shape.area`, and the dunders are Protocol/ABC surface, not stubs. Nothing to flag. ♡")
	app = app.syncStatusLine()

	return app.View()
}

// TestRenderSmoke renders every sprint TUI component. It is silent in CI and
// runs on demand in one of two modes:
//
//	TUI_SMOKE=1        go test ./cmd/celeste/tui/ -run TestRenderSmoke -v   # print to stdout
//	TUI_SNAPSHOT_DIR=… go test ./cmd/celeste/tui/ -run TestRenderSmoke      # write colored .ansi files
func TestRenderSmoke(t *testing.T) {
	snapDir := os.Getenv("TUI_SNAPSHOT_DIR")
	if os.Getenv("TUI_SMOKE") == "" && snapDir == "" {
		t.Skip("set TUI_SMOKE=1 to print, or TUI_SNAPSHOT_DIR=<dir> to write .ansi snapshots")
	}

	// Force truecolor so styles emit ANSI even though `go test` isn't a TTY —
	// otherwise lipgloss strips color and the PNGs would be monochrome.
	lipgloss.SetColorProfile(termenv.TrueColor)

	comps := renderComponents()

	if snapDir != "" {
		if err := os.MkdirAll(snapDir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, c := range comps {
			p := filepath.Join(snapDir, c.name+".ansi")
			if err := os.WriteFile(p, []byte(c.content+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		fmt.Printf("wrote %d TUI snapshots to %s\n", len(comps), snapDir)
		return
	}

	for _, c := range comps {
		fmt.Printf("\n\x1b[1m── %s ──\x1b[0m\n%s\n", c.title, c.content)
	}
}
