package compact

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// def is a tool definition of roughly size tokens.
func def(name string, size int) tui.SkillDefinition {
	return tui.SkillDefinition{
		Name:        name,
		Description: "Does " + name + ". " + strings.Repeat("More detail about it. ", size/5),
		Parameters: map[string]any{"type": "object", "properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "The path. " + strings.Repeat("Long words here. ", size/10)},
		}},
	}
}

// chatTools is a tool list shaped like the chat's: the core tools, 40
// other builtins and two MCP tools, ~10k tokens in all.
func chatTools() []tui.SkillDefinition {
	var defs []tui.SkillDefinition
	for _, n := range CoreTools {
		defs = append(defs, def(n, 150))
	}
	for i := range 40 {
		defs = append(defs, def(fmt.Sprintf("extra_%02d", i), 120))
	}
	defs = append(defs, def("mcp__srv__alpha", 120), def("mcp__srv__beta", 120))
	slices.SortFunc(defs, func(a, b tui.SkillDefinition) int { return strings.Compare(a.Name, b.Name) })
	return defs
}

func names(defs []tui.SkillDefinition) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}

const chatSystem = 3_830 // the chat's lite system prompt at 8,192 (#310)

// #310: at 8,192 the first request (system prompt and tools) fits and
// leaves the history a quarter of the window.
func TestFitToolsSmallWindowFitsWithRoomForHistory(t *testing.T) {
	defs := chatTools()
	fit := FitTools(defs, 8_192, chatSystem, nil)
	if !fit.Reduced {
		t.Fatal("~10k of tools at 8,192 were not reduced")
	}
	prefix := chatSystem + DefinitionTokens(fit.Defs)
	if HistoryBudget(8_192, prefix) < 8_192/4 {
		t.Fatalf("prefix %d leaves the history %d tokens, want at least a quarter of the window", prefix, HistoryBudget(8_192, prefix))
	}
	got := names(fit.Defs)
	for _, n := range CoreTools {
		if !slices.Contains(got, n) {
			t.Errorf("core tool %s was dropped", n)
		}
	}
	if fit.Total != len(defs) || fit.Dropped != len(defs)-len(fit.Defs) {
		t.Fatalf("Total %d Dropped %d for %d of %d", fit.Total, fit.Dropped, len(fit.Defs), len(defs))
	}
	if !slices.IsSorted(got) {
		t.Fatalf("the order changed: %v", got)
	}
}

// At 32K and 200K the full tool set goes as it is.
func TestFitToolsLargeWindowUnchanged(t *testing.T) {
	defs := chatTools()
	for _, w := range []int{32_768, 200_000, 0} {
		fit := FitTools(defs, w, 6_000, nil)
		if fit.Reduced || len(fit.Defs) != len(defs) {
			t.Fatalf("window %d: reduced to %d of %d", w, len(fit.Defs), len(defs))
		}
		for i := range defs {
			if fit.Defs[i].Description != defs[i].Description {
				t.Fatalf("window %d: %s's description changed", w, defs[i].Name)
			}
		}
	}
}

// MCP tools are the last to be kept: a builtin that fits goes before them.
func TestFitToolsDropsMCPFirst(t *testing.T) {
	var defs []tui.SkillDefinition
	for _, n := range CoreTools {
		defs = append(defs, def(n, 20))
	}
	defs = append(defs, def("aaa_builtin", 200), def("mcp__srv__tool", 200))
	all := func(string) bool { return true }
	kept := defs[:len(defs)-1]
	sys := 8_192 - 1_024 - 2_048 - mustBytes(kept, compactAll(kept, maxCompactParamDescription), all)/4 // room for exactly the builtin
	fit := FitTools(defs, 8_192, sys, nil)
	got := names(fit.Defs)
	if !fit.Reduced || !slices.Contains(got, "aaa_builtin") || slices.Contains(got, "mcp__srv__tool") {
		t.Fatalf("kept %v", got)
	}
}

// A tool the model activated this session (find_tools) is kept on a
// small window while the activated tools fit.
func TestFitToolsKeepsPinnedTools(t *testing.T) {
	defs := chatTools()
	fit := FitTools(defs, 8_192, chatSystem, []string{"mcp__srv__beta", "extra_39"})
	got := names(fit.Defs)
	if !slices.Contains(got, "mcp__srv__beta") || !slices.Contains(got, "extra_39") {
		t.Fatalf("pinned tools dropped: %v", got)
	}
	if fit.PinnedDropped != 0 {
		t.Fatalf("PinnedDropped %d", fit.PinnedDropped)
	}
}

// #310 review: every find_tools call activates more tools, and kept at any
// cost they pushed the prefix back past the window. The most recent
// activation is always sent; older ones only while they fit, newest
// first, and the fit says how many it dropped.
func TestFitToolsBoundsPinnedTools(t *testing.T) {
	defs := chatTools()
	var pinned []string
	for i := range 40 {
		pinned = append(pinned, fmt.Sprintf("extra_%02d", i))
	}
	fit := FitTools(defs, 8_192, chatSystem, pinned)
	got := names(fit.Defs)
	if !slices.Contains(got, "extra_39") {
		t.Fatalf("the newest activation was dropped: %v", got)
	}
	if slices.Contains(got, "extra_00") {
		t.Fatalf("the oldest activation was kept over newer ones: %v", got)
	}
	if fit.PinnedDropped == 0 {
		t.Fatal("PinnedDropped is 0 with activations dropped")
	}
	if prefix := chatSystem + DefinitionTokens(fit.Defs); HistoryBudget(8_192, prefix) < 8_192/4 {
		t.Fatalf("prefix %d leaves no room for history", prefix)
	}
	n := new(ToolNotices).Notice(fit, 8_192)
	if !strings.Contains(n, "find_tools activated earlier") {
		t.Fatalf("the notice does not say activated tools were dropped: %q", n)
	}
}

// Reduced definitions are shortened copies: the input is untouched.
func TestFitToolsShortensWithoutMutating(t *testing.T) {
	defs := chatTools()
	before := defs[0].Parameters["properties"].(map[string]any)["path"].(map[string]any)["description"]
	fit := FitTools(defs, 8_192, chatSystem, nil)
	after := defs[0].Parameters["properties"].(map[string]any)["path"].(map[string]any)["description"]
	if before != after {
		t.Fatal("the input's parameter description was changed")
	}
	for _, d := range fit.Defs {
		if len(d.Description) > maxCompactDescription {
			t.Fatalf("%s kept a %d-byte description", d.Name, len(d.Description))
		}
		p := d.Parameters["properties"].(map[string]any)["path"].(map[string]any)["description"].(string)
		if p != "The path." {
			t.Fatalf("%s's parameter description is %q", d.Name, p)
		}
	}
}

func TestShortDescription(t *testing.T) {
	cases := map[string]string{
		"Read a file. Then more.":       "Read a file.",
		"Line one\nline two":            "Line one",
		"No stop at all":                "No stop at all",
		"Version 1.5 is here. And more": "Version 1.5 is here.",
		strings.Repeat("a", 300):        strings.Repeat("a", maxCompactDescription-3) + "...",
	}
	for in, want := range cases {
		if got := shortDescription(in, maxCompactDescription); got != want {
			t.Errorf("shortDescription(%.20q) = %q, want %q", in, got, want)
		}
	}
}

// The notice names the window, the counts and context_limit, once per
// (kept, total, window) for one session.
func TestToolFitNoticeOnce(t *testing.T) {
	fit := ToolFit{Reduced: true, Total: 49, Dropped: 38, Defs: make([]tui.SkillDefinition, 11)}
	var seen ToolNotices
	n := seen.Notice(fit, 8_111)
	for _, want := range []string{"8.1K", "11 of 49", "context_limit", "find_tools"} {
		if !strings.Contains(n, want) {
			t.Fatalf("notice %q lacks %q", n, want)
		}
	}
	if again := seen.Notice(fit, 8_111); again != "" {
		t.Fatalf("second notice %q", again)
	}
	// Another session (an ACP editor session, a subagent) is told too.
	if other := new(ToolNotices).Notice(fit, 8_111); other == "" {
		t.Fatal("a second session was not told")
	}
	// Nothing dropped, only shortened: it does not claim a subset.
	all := seen.Notice(ToolFit{Reduced: true, Total: 23, Defs: make([]tui.SkillDefinition, 23)}, 8_111)
	if strings.Contains(all, "23 of 23") || !strings.Contains(all, "all 23 tools are sent with short descriptions") {
		t.Fatalf("notice %q", all)
	}
	if seen.Notice(ToolFit{Total: 3, Defs: make([]tui.SkillDefinition, 3)}, 8_111) != "" {
		t.Fatal("an unreduced fit has a notice")
	}
}

func TestChatToolsShapeIsRealistic(t *testing.T) {
	if n := DefinitionTokens(chatTools()); n < 9_000 || n > 12_000 {
		t.Fatalf("the fixture is %d tokens, want ~10k like the chat's", n)
	}
}

// When even the shortened core set is over the budget, the parameters lose
// their descriptions, and the core set is still sent.
func TestFitToolsMinimalTier(t *testing.T) {
	defs := chatTools()
	fit := FitTools(defs, 8_192, 4_900, nil)
	for _, n := range CoreTools {
		if !slices.Contains(names(fit.Defs), n) {
			t.Fatalf("core tool %s dropped", n)
		}
	}
	p := fit.Defs[0].Parameters["properties"].(map[string]any)["path"].(map[string]any)
	if _, ok := p["description"]; ok {
		t.Fatalf("the minimal tier kept a parameter description: %v", p)
	}
	if p["type"] != "string" {
		t.Fatalf("the minimal tier lost the parameter's type: %v", p)
	}
}
