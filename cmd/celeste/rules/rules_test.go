package rules

import (
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
