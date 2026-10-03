package builtin

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// docs/ROADMAP.md is rewritten for 2.0 (#176): what 2.0 shipped, then what
// comes next with where each deferred item came from, and no dates.
func TestRoadmapDescribes20(t *testing.T) {
	doc := repoFile(t, "docs/ROADMAP.md")
	for _, want := range []string{
		"## 2.0 (shipped)",
		"## Next",
		// #176's Docs bullet: compaction, sandboxing and AGENTS.md as delivered.
		"AGENTS.md", "Compaction", "Sandbox", "ACP", "PLAN_MODE.md",
		// Deferred items, each named with its source.
		"persona_lore", "previous_response_id", "ThoughtSignature",
		"Windows sandbox", "LSP diagnostics", "Agent Skills",
		"server compaction", "#199",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/ROADMAP.md does not mention %q", want)
		}
	}
	for _, stale := range []string{"Current Release: v1.16.0", "v2.1.0 Planned", "Last updated: August 2026"} {
		if strings.Contains(doc, stale) {
			t.Errorf("docs/ROADMAP.md still says %q", stale)
		}
	}
	checkNoLocalPaths(t, "docs/ROADMAP.md", doc)
}

// A public doc never names a local machine path.
func checkNoLocalPaths(t *testing.T, file, doc string) {
	t.Helper()
	for _, p := range []string{"/Users/", "/home/", `C:\Users`, ".worktrees", "superpowers/"} {
		if strings.Contains(doc, p) {
			t.Errorf("%s contains a local path fragment %q", file, p)
		}
	}
}

// The seven tools #176 and the 2.0 design compare celeste against.
var comparedTools = []string{"Claude Code", "Codex CLI", "opencode", "Crush", "Gemini CLI", "oh-my-pi", "pi"}

// docs/COMPARISON.md is a feature table against the seven tools: every row
// fills every column (a cell that could not be checked says "unverified"),
// every tool has sources, and celeste's numbers come from the code.
func TestComparisonCoversSevenTools(t *testing.T) {
	const file = "docs/COMPARISON.md"
	doc := repoFile(t, file)
	header := "| Feature | **Celeste** |"
	for _, tool := range comparedTools {
		header += " **" + tool + "** |"
	}
	lines := strings.Split(doc, "\n")
	start := -1
	for i, l := range lines {
		if l == header {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("%s has no table with header %q", file, header)
	}
	features := map[string]bool{}
	for _, row := range lines[start+2:] {
		if !strings.HasPrefix(row, "|") {
			break
		}
		cells := splitTableRow(row)
		if len(cells) != len(comparedTools)+2 {
			t.Errorf("%s row %q has %d cells, want %d", file, row, len(cells), len(comparedTools)+2)
			continue
		}
		for i, c := range cells {
			if strings.TrimSpace(c) == "" {
				t.Errorf("%s row %q has an empty cell %d; say \"unverified\" instead", file, row, i)
			}
		}
		features[strings.Trim(strings.TrimSpace(cells[0]), "*")] = true
	}
	for _, f := range []string{
		"Project context files", "Skills", "Hooks", "Permission model", "Sandbox",
		"Compaction", "Checkpoints and rewind", "Subagents", "Plan mode",
		"MCP client", "MCP server", "ACP", "LSP", "Providers", "Local models",
		"Code graph and structural review", "Persona",
	} {
		if !features[f] {
			t.Errorf("%s has no %q row", file, f)
		}
	}

	// pi 1.0 shipped on Thursday 1 October 2026; MCP support arrived in 0.99.
	if strings.Contains(doc, "2 October 2026") {
		t.Errorf("%s dates pi 1.0 to 2 October 2026; it shipped on 1 October 2026", file)
	}
	if !strings.Contains(doc, "since 0.99") {
		t.Errorf("%s does not say pi's MCP client arrived in 0.99", file)
	}

	at := strings.Index(doc, "\n## Sources\n")
	if at < 0 {
		t.Fatalf("%s has no Sources section", file)
	}
	sources := doc[at:]
	if !strings.Contains(sources, "https://gigazine.net/gsc_news/en/20261002-pi-1-0/") {
		t.Errorf("%s Sources section does not cite GIGAZINE for the pi 1.0 date", file)
	}
	if !strings.Contains(sources, "Retrieved") {
		t.Errorf("%s Sources section gives no retrieval date", file)
	}
	for _, tool := range comparedTools {
		if !strings.Contains(sources, "### "+tool+"\n") {
			t.Errorf("%s Sources section has no list for %s", file, tool)
		}
	}

	truth := computeTruths(t)
	for _, c := range []claim{
		{file, `\| \*\*Built-in tools\*\* \| (\d+) `, func(d docTruths) int { return d.allTools }, "built-in tools"},
		{file, `(\d+) chat providers`, func(d docTruths) int { return d.chatProviders }, "chat providers"},
	} {
		m := regexp.MustCompile(c.pattern).FindAllStringSubmatch(doc, -1)
		if len(m) == 0 {
			t.Errorf("%s: no %s claim matches %q", file, c.what, c.pattern)
		}
		for _, x := range m {
			if got, _ := strconv.Atoi(x[1]); got != c.want(truth) {
				t.Errorf("%s says %d %s; the code has %d", file, got, c.what, c.want(truth))
			}
		}
	}
	for _, stale := range []string{"April 2026", "OpenClaw", "Picobot"} {
		if strings.Contains(doc, stale) {
			t.Errorf("%s still mentions %q from the 1.x comparison", file, stale)
		}
	}
	checkNoLocalPaths(t, file, doc)
}

// splitTableRow splits a Markdown table row into cells, keeping an escaped
// pipe (\|) inside its cell.
func splitTableRow(row string) []string {
	const placeholder = "\x00"
	row = strings.ReplaceAll(row, `\|`, placeholder)
	cells := strings.Split(strings.Trim(row, "|"), "|")
	for i, c := range cells {
		cells[i] = strings.ReplaceAll(c, placeholder, `\|`)
	}
	return cells
}

func TestSplitTableRowKeepsEscapedPipes(t *testing.T) {
	got := splitTableRow(`| a | b \| c | d |`)
	if len(got) != 3 || strings.TrimSpace(got[1]) != `b \| c` {
		t.Fatalf("splitTableRow = %q, want 3 cells with \"b \\| c\" in the middle", got)
	}
}
