package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRuleFile(t *testing.T) {
	r, err := Parse("no-todo", "x.md", []byte("---\ncondition: '\\bTODO\\b'\nscope: text, tool_args:write_file.content\naction: queue\nrepeat: after-gap:2\n---\nDon't leave TODOs.\n"))
	if err != nil {
		t.Fatal(err)
	}
	if r.Name != "no-todo" || r.Action != Queue || r.Once || r.Gap != 2 || len(r.Scopes) != 2 || r.Message != "Don't leave TODOs." {
		t.Errorf("rule = %+v", r)
	}
	if r.Scopes[1] != (Scope{Kind: ScopeToolArgs, Tool: "write_file", Field: "content"}) {
		t.Errorf("scope = %+v", r.Scopes[1])
	}
	if !r.Condition.MatchString("a TODO here") || r.Condition.MatchString("TODOS") {
		t.Error(`'\bTODO\b' must keep its backslashes`)
	}
}

func TestParseRejectsBadRules(t *testing.T) {
	for name, src := range map[string]string{
		"no frontmatter": "condition: x\n",
		"no condition":   "---\naction: append\n---\nbody\n",
		"bad regex":      "---\ncondition: (\n---\nbody\n",
		"bad action":     "---\ncondition: x\naction: shout\n---\nbody\n",
		"bad scope":      "---\ncondition: x\nscope: tool_args:bash\n---\nbody\n",
		"bad repeat":     "---\ncondition: x\nrepeat: twice\n---\nbody\n",
		"unknown key":    "---\ncondition: x\ncolour: red\n---\nbody\n",
		"no body":        "---\ncondition: x\n---\n\n",
	} {
		if _, err := Parse("r", name, []byte(src)); err == nil {
			t.Errorf("%s: parsed, want an error", name)
		}
	}
	if r, err := Parse("r", "off", []byte("---\nenabled: false\n---\n")); err != nil || !r.Disabled {
		t.Errorf("enabled: false = %+v, %v", r, err)
	}
}

func set(t *testing.T, rules ...string) *Set {
	t.Helper()
	s := &Set{}
	for i, src := range rules {
		r, err := Parse("r"+string(rune('a'+i)), "test", []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		s.Rules = append(s.Rules, r)
	}
	return s
}

func TestTextMatchAcrossDeltasOncePerRequest(t *testing.T) {
	m := NewMatcher(set(t, "---\ncondition: Audio saved:\n---\nx\n"))
	m.StartRequest()
	if hits := m.Text("All done. Audio sa"); len(hits) != 0 {
		t.Fatalf("early hit %v", hits)
	}
	if hits := m.Text("ved: /tmp/x.mp3"); len(hits) != 1 || hits[0].Text != "Audio saved:" {
		t.Fatalf("split match: %v", hits)
	}
	if hits := m.Text(" Audio saved: again"); len(hits) != 0 {
		t.Errorf("a rule fires at most once per request: %v", hits)
	}
}

func TestRepeatOnceAndGap(t *testing.T) {
	m := NewMatcher(set(t,
		"---\ncondition: once\n---\nx\n",
		"---\ncondition: gap\nrepeat: after-gap:2\n---\nx\n",
	))
	fires := map[string]int{}
	for i := 0; i < 5; i++ {
		m.StartRequest()
		for _, h := range m.Text("once gap") {
			fires[h.Rule.Name]++
		}
	}
	if fires["ra"] != 1 || fires["rb"] != 3 {
		t.Errorf("fires = %v, want once 1 and after-gap:2 3 (requests 1, 3, 5)", fires)
	}
}

func TestToolArgsScope(t *testing.T) {
	m := NewMatcher(set(t, "---\ncondition: rm -rf\nscope: tool_args:bash.command\n---\nx\n"))
	m.StartRequest()
	hits := m.Calls([]Call{
		{ID: "1", Name: "read_file", Input: map[string]any{"command": "rm -rf /"}},
		{ID: "2", Name: "bash", Input: map[string]any{"command": "rm -rf build"}},
	})
	if len(hits) != 1 || hits[0].Call.ID != "2" {
		t.Errorf("hits = %+v", hits)
	}
}

func TestStatsSnapshot(t *testing.T) {
	ResetStats()
	Record("a", true)
	Record("a", false)
	Record("b", false)
	s := Snapshot()
	if s["fires"] != 3 || s["acted"] != 1 || s["shadowed"] != 2 || s["by_rule"].(map[string]any)["a"] != 2 {
		t.Errorf("snapshot = %v", s)
	}
}

func TestBuiltinsParse(t *testing.T) {
	want := []string{"destructive-bash", "persona-voice-in-files", "task-complete-before-verify", "three-strikes", "unbacked-audio-claim"}
	var got []string
	for _, r := range Builtins() {
		got = append(got, r.Name)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("built-ins = %v, want %v", got, want)
	}
}

func TestLoadOverridesDisablesAndWarns(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".celeste", "rules")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "three-strikes.md"), []byte("---\nenabled: false\n---\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "broken.md"), []byte("not a rule"), 0o644)
	os.WriteFile(filepath.Join(dir, "mine.md"), []byte("---\ncondition: foo\n---\nNo foo.\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "deep.md"), []byte("---\ncondition: hmm\nscope: thinking\n---\nThink less.\n"), 0o644)
	grim := "### mine\n---\ncondition: bar\naction: interrupt\n---\nNo bar.\n\n### repo-rule\n---\ncondition: baz\n---\nNo baz.\n"
	var warns []string
	set := Load(home, []Section{{Source: "/ws/.grimoire", Body: grim}}, func(s string) { warns = append(warns, s) })
	byName := map[string]*Rule{}
	for _, r := range set.Rules {
		byName[r.Name] = r
	}
	if byName["three-strikes"] != nil {
		t.Error("enabled: false must remove the built-in")
	}
	if r := byName["mine"]; r == nil || r.Action != Interrupt || r.Source != "/ws/.grimoire#mine" {
		t.Errorf("the grimoire rule must replace the user file's: %+v", r)
	}
	if byName["repo-rule"] == nil || byName["unbacked-audio-claim"] == nil || byName["deep"] != nil {
		t.Errorf("rules = %v", byName)
	}
	if len(warns) != 2 || !strings.Contains(strings.Join(warns, "\n"), "broken.md") || !strings.Contains(strings.Join(warns, "\n"), "thinking") {
		t.Errorf("warnings = %v", warns)
	}
}

func builtinMatcher(t *testing.T) *Matcher {
	t.Helper()
	return NewMatcher(&Set{Rules: Builtins()})
}

func names(hits []Hit) string {
	var n []string
	for _, h := range hits {
		n = append(n, h.Rule.Name)
	}
	return strings.Join(n, ",")
}

func TestBuiltinVoiceInFilesExemptions(t *testing.T) {
	cases := []struct {
		path, content string
		fire          bool
	}{
		{"main.go", "// handled, darling~\n", true},
		{"README.md", "Install it *giggles*\n", true},
		{"README.md", "Her catchphrase:\n\n> ara ara, darling\n", false},
		{"notes.md", "```\nfmt.Println(\"darling\")\n```\n", false},
		{"docs/PERSONALITY.md", "She says darling.\n", false},
		{"prompts/persona/core.md", "darling\n", false},
		{"main.go", "func main() {}\n", false},
	}
	for _, c := range cases {
		m := builtinMatcher(t)
		m.StartRequest()
		hits := m.Calls([]Call{{Name: "write_file", Input: map[string]any{"path": c.path, "content": c.content}}})
		if got := names(hits) == "persona-voice-in-files"; got != c.fire {
			t.Errorf("%s %q: fired=%v, want %v", c.path, c.content, got, c.fire)
		}
	}
}

func TestBuiltinAudioClaimNeedsNoTTS(t *testing.T) {
	m := builtinMatcher(t)
	m.StartRequest()
	if names(m.Text("Audio saved: /tmp/a.mp3")) != "unbacked-audio-claim" {
		t.Error("an unbacked claim must fire")
	}
	m = builtinMatcher(t)
	m.ToolResult("generate_speech", false)
	m.StartRequest()
	if hits := m.Text("Audio saved: /tmp/a.mp3"); len(hits) != 0 {
		t.Errorf("a claim after a real TTS call fired: %v", hits)
	}
}

func TestBuiltinTaskCompleteNeedsAnUncheckedEdit(t *testing.T) {
	m := builtinMatcher(t)
	m.StartRequest()
	if hits := m.Text("TASK_COMPLETE: nothing changed"); len(hits) != 0 {
		t.Errorf("no edit, fired %v", hits)
	}
	m = builtinMatcher(t)
	m.ToolResult("write_file", false)
	m.ToolResult("bash", false)
	m.StartRequest()
	if hits := m.Text("TASK_COMPLETE: tested"); len(hits) != 0 {
		t.Errorf("edit then bash, fired %v", hits)
	}
	m.ToolResult("patch_file", false)
	m.StartRequest()
	if names(m.Text("TASK_COMPLETE: done")) != "task-complete-before-verify" {
		t.Error("an edit after the last command must fire")
	}
	m = builtinMatcher(t)
	m.Facts().RuntimeVerifies = true
	m.ToolResult("write_file", false)
	m.StartRequest()
	if hits := m.Text("TASK_COMPLETE: done"); len(hits) != 0 {
		t.Errorf("the runtime verifies; fired %v", hits)
	}
}

func TestBuiltinDestructiveBash(t *testing.T) {
	for cmd, fire := range map[string]bool{
		"git push --force origin main":            true,
		"git push -f":                             true,
		"git push --force-with-lease origin main": false,
		"rm -rf build":                            true,
		"rm -fr /tmp/x":                           true,
		"rm -Rf node_modules":                     true,
		"rm --recursive --force dist":             true,
		"rm -r build":                             false,
		"rm notes.txt":                            false,
		"echo farm -rf":                           false,
		"git push origin main":                    false,
	} {
		m := builtinMatcher(t)
		m.StartRequest()
		got := names(m.Calls([]Call{{Name: "bash", Input: map[string]any{"command": cmd}}})) == "destructive-bash"
		if got != fire {
			t.Errorf("%q: fired=%v, want %v", cmd, got, fire)
		}
	}
}

func TestStripUnbackedAudioClaim(t *testing.T) {
	if got := StripUnbackedAudioClaim("Audio saved: /x.mp3", false); strings.Contains(got, "Audio saved:") {
		t.Errorf("not stripped: %q", got)
	}
	if got := StripUnbackedAudioClaim("Audio saved: /x.mp3", true); got != "Audio saved: /x.mp3" {
		t.Errorf("a backed claim changed: %q", got)
	}
}
