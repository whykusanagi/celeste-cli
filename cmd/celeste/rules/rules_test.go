package rules

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	if hits := scanNow(m, "All done. Audio sa"); len(hits) != 0 {
		t.Fatalf("early hit %v", hits)
	}
	if hits := scanNow(m, "ved: /tmp/x.mp3"); len(hits) != 1 || hits[0].Text != "Audio saved:" {
		t.Fatalf("split match: %v", hits)
	}
	if hits := scanNow(m, " Audio saved: again"); len(hits) != 0 {
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
		for _, h := range scanNow(m, "once gap") {
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
		// Normal work that is not persona voice (W3-1 review I2).
		{".gitignore", "*~\n", false},
		{"deploy.sh", "cd ~\n", false},
		{"paper.tex", "as Fig.~\\ref{x} shows\n", false},
		{"main.go", "x := 1 // ~\n", false},
		{"README.md", "Made with \u2665\n", false},
		{".gitignore", "notes.txt~\n", false},
		{"build.sh", "cp a b~\n", false},
		{"notes.txt", "all done~\n", true},
		{"notes.txt", "\u5b8c\u4e86~\n", true},
		{"README.md", "the ~~old~~\n", false},
		{"README.md", "Use ~~foo~~ instead of bar.\n", false},
		{"setup.sh", "echo done, darling\n", true},
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
	if names(scanNow(m, "Audio saved: /tmp/a.mp3")) != "unbacked-audio-claim" {
		t.Error("an unbacked claim must fire")
	}
	m = builtinMatcher(t)
	m.ToolResult("generate_speech", false)
	m.StartRequest()
	if hits := scanNow(m, "Audio saved: /tmp/a.mp3"); len(hits) != 0 {
		t.Errorf("a claim after a real TTS call fired: %v", hits)
	}
}

func TestBuiltinTaskCompleteNeedsAnUncheckedEdit(t *testing.T) {
	m := builtinMatcher(t)
	m.StartRequest()
	if hits := scanNow(m, "TASK_COMPLETE: nothing changed"); len(hits) != 0 {
		t.Errorf("no edit, fired %v", hits)
	}
	m = builtinMatcher(t)
	m.ToolResult("write_file", false)
	m.ToolResult("bash", false)
	m.StartRequest()
	if hits := scanNow(m, "TASK_COMPLETE: tested"); len(hits) != 0 {
		t.Errorf("edit then bash, fired %v", hits)
	}
	m.ToolResult("patch_file", false)
	m.StartRequest()
	if names(scanNow(m, "TASK_COMPLETE: done")) != "task-complete-before-verify" {
		t.Error("an edit after the last command must fire")
	}
	m = builtinMatcher(t)
	m.Facts().RuntimeVerifies = true
	m.ToolResult("write_file", false)
	m.StartRequest()
	if hits := scanNow(m, "TASK_COMPLETE: done"); len(hits) != 0 {
		t.Errorf("the runtime verifies; fired %v", hits)
	}
}

func TestBuiltinDestructiveBash(t *testing.T) {
	for cmd, fire := range map[string]bool{
		"git push --force origin main":            true,
		"git push -f":                             true,
		"git push --force-with-lease origin main": false,
		"rm -rf src":                              true,
		"rm -fr /tmp/x":                           true,
		"rm -Rf vendor":                           true,
		"rm --recursive --force lib":              true,
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

// A match that spans many deltas and more than a few hundred bytes is still
// found (ext-review): the matcher keeps a bounded tail of the reply.
func TestTextMatchSpanningManyDeltas(t *testing.T) {
	m := NewMatcher(set(t, "---\ncondition: (?s)BEGIN.{900,}END\n---\nx\n"))
	m.StartRequest()
	if hits := scanNow(m, "BEGIN"); len(hits) != 0 {
		t.Fatal("early hit")
	}
	for i := 0; i < 30; i++ {
		if hits := scanNow(m, strings.Repeat("y", 50)); len(hits) != 0 {
			t.Fatal("early hit")
		}
	}
	if hits := scanNow(m, "END"); len(hits) != 1 {
		t.Fatalf("a match spanning 1.5 KB of deltas was missed: %v", hits)
	}
}

// The matcher's retained text stays bounded however long the reply.
func TestTextMatcherRetainsABoundedTail(t *testing.T) {
	m := NewMatcher(set(t, "---\ncondition: never-there\n---\nx\n"))
	m.StartRequest()
	for i := 0; i < 100; i++ {
		m.Text(strings.Repeat("z", 1000))
	}
	if n := len(m.tail); n > scanBack {
		t.Errorf("retained %d bytes, cap %d", n, scanBack)
	}
}

// An oversized rule file is skipped with a warning, not read whole.
func TestLoadSkipsOversizedRuleFiles(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".celeste", "rules")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "huge.md"), []byte("---\ncondition: x\n---\n"+strings.Repeat("a", maxRuleBytes+1)), 0o644)
	var warns []string
	set := Load(home, nil, func(s string) { warns = append(warns, s) })
	for _, r := range set.Rules {
		if r.Name == "huge" {
			t.Error("an oversized rule loaded")
		}
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "huge.md") {
		t.Errorf("warnings = %v", warns)
	}
}

// The matcher scans in batches (W3-1 review I1): a match that straddles a
// batch boundary is still found, and Flush finds one the batching held
// back at the end of the stream.
func TestTextThrottleStillFindsEveryMatch(t *testing.T) {
	m := NewMatcher(set(t, "---\ncondition: Audio saved:\nrepeat: after-gap:1\n---\nx\n"))
	m.StartRequest()
	pad := strings.Repeat("a", scanEvery-3)
	var hits []Hit
	for _, d := range []string{pad, "Aud", "io sa", "ved: x"} {
		hits = append(hits, m.Text(d)...)
	}
	hits = append(hits, m.Flush()...)
	if len(hits) != 1 {
		t.Fatalf("straddling match: %v", hits)
	}

	m.StartRequest()
	if hits := m.Text("short: Audio saved: y"); len(hits) != 0 {
		t.Fatalf("a short delta without a newline should wait for the batch: %v", hits)
	}
	m2 := NewMatcher(set(t, "---\ncondition: Audio saved:\nrepeat: after-gap:1\n---\nx\n"))
	m2.StartRequest()
	m2.Text("short: Audio saved: y")
	if hits := m2.Flush(); len(hits) != 1 {
		t.Fatalf("Flush must scan what the batching held back: %v", hits)
	}
	m2.StartRequest()
	if hits := m2.Text("line one Audio saved: z\n"); len(hits) != 1 {
		t.Fatalf("a newline scans at once: %v", hits)
	}
}

// scanNow feeds a delta and flushes, as if the stream ended after it.
func scanNow(m *Matcher, delta string) []Hit {
	return append(m.Text(delta), m.Flush()...)
}

// destructive-bash repeats (after-gap:1), exempts common build output
// directories under the workspace, and catches split flags (review I3).
func TestBuiltinDestructiveBashRepeatsAndExemptsBuildDirs(t *testing.T) {
	for cmd, fire := range map[string]bool{
		"rm -rf build":                  false,
		"rm -rf ./build":                false,
		"rm -rf node_modules dist":      false,
		"rm -rf target/ .cache out":     false,
		"rm -rf coverage":               false,
		"rm -rf build src":              true,
		"rm -rf /build":                 true,
		"rm -rf ../build":               true,
		"rm -rf ~/build":                true,
		"rm -r -f docs":                 true,
		"rm -f -r docs":                 true,
		"rm -rf build && rm -rf src":    true,
		"rm -rf $HOME":                  true,
		"cd sub && rm -rf node_modules": false,
	} {
		m := builtinMatcher(t)
		m.StartRequest()
		got := names(m.Calls([]Call{{Name: "bash", Input: map[string]any{"command": cmd}}})) == "destructive-bash"
		if got != fire {
			t.Errorf("%q: fired=%v, want %v", cmd, got, fire)
		}
	}
	m := builtinMatcher(t)
	for i := 0; i < 3; i++ {
		m.StartRequest()
		if names(m.Calls([]Call{{Name: "bash", Input: map[string]any{"command": "rm -rf src"}}})) != "destructive-bash" {
			t.Fatalf("request %d: an earlier fire must not disable the rule", i+1)
		}
	}
}

// destructive-bash sees through quoting, paths, subshells, nested shells
// and flags after targets; redirections are not targets (re-review item 1).
func TestBuiltinDestructiveBashEvasions(t *testing.T) {
	for cmd, fire := range map[string]bool{
		"/bin/rm -rf src":                        true,
		"/usr/bin/rm -rf src":                    true,
		`\rm -rf src`:                            true,
		`"rm" -rf src`:                           true,
		"(rm -rf src)":                           true,
		"echo $(rm -rf src)":                     true,
		"echo `rm -rf src`":                      true,
		`sh -c 'rm -rf src'`:                     true,
		`bash -c "rm -rf src"`:                   true,
		`zsh -c 'cd x && rm -rf src'`:            true,
		`ssh host 'rm -rf /srv/app'`:             true,
		`docker exec web sh -c 'rm -rf /data'`:   true,
		`eval 'rm -rf src'`:                      true,
		"rm src -rf":                             true,
		"rm -r src -f":                           true,
		`bash -c 'git push --force origin main'`: true,
		"rm -rf build 2>&1":                      false,
		"rm -rf build > /dev/null":               false,
		"rm -rf dist >out.log 2>/dev/null":       false,
		`sh -c 'rm -rf node_modules'`:            false,
		"/bin/rm -rf ./build":                    false,
		"rm build -rf":                           false,
		`echo "rm -rf src is dangerous" > notes`: false,
		"grep -r farm -f x":                      false,
	} {
		m := builtinMatcher(t)
		m.StartRequest()
		got := names(m.Calls([]Call{{Name: "bash", Input: map[string]any{"command": cmd}}})) == "destructive-bash"
		if got != fire {
			t.Errorf("%q: fired=%v, want %v", cmd, got, fire)
		}
	}
}

func TestDetectorsMatchTheBuiltins(t *testing.T) {
	if !VoiceLeak("main.go", "// ok darling") || VoiceLeak("docs/voice.md", "darling") || VoiceLeak("a.go", "package a") {
		t.Error("VoiceLeak disagrees with persona-voice-in-files")
	}
	for cmd, want := range map[string]bool{
		"git push --force":            true,
		"git push --force-with-lease": false,
		"rm -rf /":                    true,
		"rm -rf build":                false,
		"go test ./...":               false,
		"bash -c 'git push -f'":       true,
		// What the bash tool refuses, the rule sees (audit #7 D4).
		"bash -lc 'rm -rf ~'":    true,
		"rm -rf${IFS}/":          true,
		"rm -r /":                true,
		"rm --rec --for src":     true,
		"r\\\nm -rf src":         true,
		"echo # rm -rf src":      false,
		"rm -rf build # and src": false,
	} {
		if got := Destructive(cmd); got != want {
			t.Errorf("Destructive(%q) = %v, want %v", cmd, got, want)
		}
	}
}

// The edit and check tables are shared with the watchdog (steer).
func TestIsEditIsCheck(t *testing.T) {
	for _, tool := range []string{"write_file", "patch_file", "splice_file"} {
		if !IsEdit(tool) || IsCheck(tool) {
			t.Errorf("%s: edit", tool)
		}
	}
	if !IsCheck("bash") || IsEdit("bash") || IsEdit("read_file") || IsCheck("read_file") {
		t.Error("bash is the check; read_file is neither")
	}
}

func TestStripUnbackedSpawnClaim(t *testing.T) {
	fake := "Subagent spawned: subagent-sleep-task (id: task-47)"
	// No spawn ran → fabricated claim is replaced.
	got := StripUnbackedSpawnClaim(fake, false)
	if got == fake || !strings.Contains(got, "no subagent was actually spawned") {
		t.Fatalf("expected fabricated spawn claim to be stripped, got %q", got)
	}
	// A real spawn ran → content passes through unchanged.
	if got := StripUnbackedSpawnClaim(fake, true); got != fake {
		t.Fatalf("expected passthrough when spawn ran, got %q", got)
	}
	// Unrelated text is never touched.
	plain := "Here is a summary of the repo."
	if got := StripUnbackedSpawnClaim(plain, false); got != plain {
		t.Fatalf("expected unrelated text untouched, got %q", got)
	}
}

// destructive-bash's condition matches any command (\S), so its hit carries
// the command itself for the log, not the first character (review of
// cleanup-4).
func TestDestructiveBashHitTextIsTheCommand(t *testing.T) {
	m := builtinMatcher(t)
	m.StartRequest()
	hits := m.Calls([]Call{{Name: "bash", Input: map[string]any{"command": "  rm -rf src"}}})
	if len(hits) != 1 || hits[0].Text != "  rm -rf src" {
		t.Fatalf("hits = %+v", hits)
	}
}

// The advisory rule reads every bash call (condition \S) and the watchdog
// reads recent ones: 40 KB of adversarial shell stays fast.
func TestDestructiveLinearOnLongLines(t *testing.T) {
	// Compare growth, not wall time, so a slow CI runner can't fail it: four
	// times the input costs about 4x when linear and 16x when quadratic.
	for _, unit := range []string{"rm -r x ", "env ", "bash -lc ", "sudo -u x ", "git push x ", "a=1 "} {
		n := 40000 / len(unit)
		small, big := strings.Repeat(unit, n/4), strings.Repeat(unit, n)
		if r := growth(func() { Destructive(small) }, func() { Destructive(big) }); r > 8 {
			t.Errorf("%q x4 took %.1fx as long; want linear (~4x, quadratic is ~16x)", unit, r)
		}
	}
}

// growth is how many times longer big takes than small, each timed as the
// best of five runs.
func growth(small, big func()) float64 {
	best := func(f func()) time.Duration {
		d := time.Duration(1<<63 - 1)
		for i := 0; i < 5; i++ {
			start := time.Now()
			f()
			d = min(d, time.Since(start))
		}
		return max(d, time.Microsecond)
	}
	return float64(best(big)) / float64(best(small))
}
