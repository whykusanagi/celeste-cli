package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

var update = flag.Bool("update", false, "rewrite contract goldens")

type rpc struct {
	id     int64
	method string
	params any
}

// call runs one MCP session (initialize + the given requests) and returns
// each response's "result" keyed by id.
func call(t *testing.T, cfg Config, reqs ...rpc) map[int64]json.RawMessage {
	t.Helper()
	srv := New(cfg)
	RegisterHandlers(srv)
	defer srv.Close()
	var in bytes.Buffer
	write := func(id int64, method string, params any) {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		in.Write(append(b, '\n'))
	}
	write(0, "initialize", map[string]any{"protocolVersion": "2024-11-05"})
	for _, r := range reqs {
		write(r.id, r.method, r.params)
	}
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := srv.serveStdioStreams(ctx, &in, &out); err != nil && err != context.Canceled {
		t.Fatalf("serve: %v", err)
	}
	res := map[int64]json.RawMessage{}
	sc := bufio.NewScanner(&out)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		var m struct {
			ID     *int64          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(sc.Bytes(), &m) == nil && m.ID != nil {
			if m.Result != nil {
				res[*m.ID] = m.Result
			} else {
				res[*m.ID] = m.Error
			}
		}
	}
	return res
}

// volatileKeys lists field names whose values change per run, per machine, or
// per release and must be masked so goldens stay stable. db_path was added
// after -update surfaced a real temp-dir path (ADAPTED, 2026-09-27): the
// celeste_index tools cache the SQLite index under a per-HOME,
// per-workspace-hash path, which is neither a stable value nor caught by the
// workspace-path masking below (it embeds a hash of the workspace, not the
// workspace string itself).
const volatileKeys = `uptime[a-z_]*|commit|version|started_at|elapsed[a-z_]*|timestamp|duration[a-z_]*|run_id|agent_run_id|created_at|updated_at|db_path`

var volatile = regexp.MustCompile(`"(` + volatileKeys + `)"\s*:\s*("[^"]*"|[0-9.]+)`)

// volatileEscaped matches the same fields one level deep inside a JSON
// string. ADAPTED (2026-09-27): several MCP tool results (celeste_status,
// celeste_index) wrap their JSON payload as a content[].text string, so in
// the raw response bytes its quotes are backslash-escaped at the point this
// function runs (before the outer json.Unmarshal below unescapes them). The
// plain `volatile` regex above never matches that escaped form, so
// "commit"/"uptime"/"version"/"elapsed"/"db_path" were slipping into the
// status and index_rebuild/index_status goldens unmasked. This regex is
// applied to the still-escaped raw string, so its replacement must also be
// escaped JSON-string content.
var volatileEscaped = regexp.MustCompile(`\\"(` + volatileKeys + `)\\"\s*:\s*(\\"[^"\\]*\\"|[0-9.]+)`)

// matchPercent masks celeste_code_search's "NN% match" score in its
// human-readable text output. ADAPTED (2026-09-27): the score comes from
// MinHash similarity (codegraph/minhash.go NewMinHasher), which draws fresh
// crypto/rand seeds on every index build -- confirmed genuinely flaky by
// running TestContractCodeGraphTools 5x head-to-head: 43%, 49%, 44%, 47%,
// 51% for the identical "helper" query against identical source. Not a JSON
// key, so it isn't caught by the volatile/volatileEscaped regexes above.
var matchPercent = regexp.MustCompile(`\d+% match`)

// normalize masks fields that change per run/machine so goldens are stable
// across time, OSes and temp directories.
func normalize(b []byte, ws string) []byte {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if ws != "" {
		s = strings.ReplaceAll(s, ws, "<WORKSPACE>")
		s = strings.ReplaceAll(s, filepath.ToSlash(ws), "<WORKSPACE>")
		esc, _ := json.Marshal(ws)
		s = strings.ReplaceAll(s, strings.Trim(string(esc), `"`), "<WORKSPACE>")
	}
	s = volatile.ReplaceAllString(s, `"$1":"<MASKED>"`)
	s = volatileEscaped.ReplaceAllString(s, `\"$1\":\"<MASKED>\"`)
	s = matchPercent.ReplaceAllString(s, "<MASKED>% match")
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		out, _ := json.MarshalIndent(v, "", "  ")
		return append(out, '\n')
	}
	return []byte(s)
}

func TestNormalizeMasksVolatileFields(t *testing.T) {
	in := []byte("{\"commit\":\"abc123\",\"uptime\":\"3s\",\"path\":\"C:\\\\tmp\\\\ws\\\\a.go\",\"n\":1}\r\n")
	got := string(normalize(in, `C:\tmp\ws`))
	for _, bad := range []string{"abc123", "3s", `tmp\\ws`} {
		if strings.Contains(got, bad) {
			t.Errorf("normalize kept %q:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, `"n": 1`) {
		t.Errorf("normalize dropped stable data:\n%s", got)
	}
}

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "contract", name+".golden")
	if *update {
		os.MkdirAll(filepath.Dir(path), 0o755)
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update once, review, commit): %v", path, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Errorf("MCP contract changed for %s (spec §3.1 frozen). If additive and intended, rerun with -update and commit.\n--- got ---\n%s", name, got)
	}
}

func contractCfg(t *testing.T, srv *fakeprovider.Server) (Config, string) {
	t.Helper()
	// ADAPTED (2026-09-27, ruling: hermetic HOME): one temp dir for both HOME
	// and USERPROFILE, so the test stays hermetic on Windows too, where
	// os.UserHomeDir() reads USERPROFILE rather than HOME.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := t.TempDir()
	os.WriteFile(filepath.Join(ws, "main.go"), []byte("package main\n\nfunc main() { helper() }\n\nfunc helper() {}\n"), 0o644)
	cc := &config.Config{APIKey: "k", Model: "fake-model", Timeout: 10}
	if srv != nil {
		cc.BaseURL = srv.BaseURL()
	}
	cfg := DefaultConfig()
	cfg.CelesteConfig = cc
	cfg.Workspace = ws
	return cfg, ws
}

func TestContractToolsList(t *testing.T) {
	cfg, ws := contractCfg(t, nil)
	res := call(t, cfg, rpc{1, "tools/list", map[string]any{}})
	golden(t, "tools_list", normalize(res[1], ws))
}

func TestContractCodeGraphTools(t *testing.T) {
	cfg, ws := contractCfg(t, nil)
	res := call(t, cfg,
		rpc{1, "tools/call", map[string]any{"name": "celeste_index", "arguments": map[string]any{"operation": "rebuild", "workspace": ws}}},
		rpc{2, "tools/call", map[string]any{"name": "celeste_index", "arguments": map[string]any{"operation": "status", "workspace": ws}}},
		rpc{3, "tools/call", map[string]any{"name": "celeste_code_search", "arguments": map[string]any{"query": "helper", "top_k": 3, "workspace": ws}}},
		rpc{4, "tools/call", map[string]any{"name": "celeste_code_graph", "arguments": map[string]any{"symbol": "helper", "direction": "callers", "depth": 1, "workspace": ws}}},
		rpc{5, "tools/call", map[string]any{"name": "celeste_code_symbols", "arguments": map[string]any{"file": "main.go", "workspace": ws}}},
		rpc{6, "tools/call", map[string]any{"name": "celeste_code_review", "arguments": map[string]any{"kinds": "ALL", "max_results": 30, "include_tests": false, "workspace": ws}}},
	)
	for id, name := range map[int64]string{1: "index_rebuild", 2: "index_status", 3: "code_search", 4: "code_graph", 5: "code_symbols", 6: "code_review"} {
		golden(t, name, normalize(res[id], ws))
	}
}

func TestContractStatusTakesNoArguments(t *testing.T) {
	cfg, ws := contractCfg(t, nil)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste_status", "arguments": map[string]any{}}})
	golden(t, "status", normalize(res[1], ws))
}

// celeste mode:"chat" must run a tool loop inside the server that edits the
// workspace (celeste-docs / celeste-context skills depend on it).
func TestContractChatModeEditsWorkspace(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"NOTES.md","content":"hello"}`}}},
		fakeprovider.Turn{Text: "Wrote NOTES.md"},
	)
	cfg, ws := contractCfg(t, llm)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "write NOTES.md", "mode": "chat", "workspace": ws}}})
	if b, err := os.ReadFile(filepath.Join(ws, "NOTES.md")); err != nil || string(b) != "hello" {
		t.Fatalf("chat mode did not edit the workspace: %v %q\nresult: %s", err, b, res[1])
	}
	golden(t, "celeste_chat", normalize(res[1], ws))
}

// Baseline: MCP chat strips an unbacked "Audio saved:" claim (only here today;
// 2.0 W3 moves it to every mode).
func TestServerChatStripsUnbackedAudioClaim(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "Audio saved: /tmp/x.mp3"})
	cfg, ws := contractCfg(t, llm)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "say it", "mode": "chat", "workspace": ws}}})
	if strings.Contains(string(res[1]), "Audio saved:") {
		t.Fatalf("unbacked audio claim was not stripped: %s", res[1])
	}
}

// Baseline: MCP chat's identical-call guard stops after 3 identical calls.
func TestServerChatIdenticalCallGuard(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 10; i++ {
		turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"main.go"}`}}})
	}
	turns = append(turns, fakeprovider.Turn{Text: "done"})
	llm := fakeprovider.NewOpenAI(t, turns...)
	cfg, ws := contractCfg(t, llm)
	call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "loop", "mode": "chat", "workspace": ws}}})
	if n := len(llm.Requests()); n > 5 {
		t.Fatalf("server made %d requests; identical-call guard (3) should stop the loop well before 10", n)
	}
}

func TestContractContentTool(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "A short post."})
	cfg, ws := contractCfg(t, llm)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste_content", "arguments": map[string]any{"prompt": "write a post", "format": "markdown"}}})
	golden(t, "celeste_content", normalize(res[1], ws))
}

// Baseline: MCP chat's progress guard stops a run whose tool results repeat
// (6 identical results) even when the call arguments differ, so the
// identical-call guard never trips. "main.go" and "./main.go" alternate:
// different args, same file, same result -- or so the brief assumed.
//
// ADJUSTED (2026-09-27, characterization): read_file's result JSON echoes the
// literal "path" argument back verbatim (tools/builtin/read_file.go,
// Execute(): result["path"] = path, the raw input string, before
// resolvePath normalizes it). So "main.go" and "./main.go" produce two
// DISTINCT result strings, not one repeated result, and progressGuard's
// streak (handlers.go, progressGuard.observe) never exceeds 1 -- the guard
// never trips. Confirmed by running this test unmodified: the server made 13
// requests (all 12 scripted tool-call turns plus the final "done" turn), not
// the <=8 the brief predicted. Per the brief's own escape hatch ("If the
// progress guard compares results in a way that ./main.go defeats ... record
// the observed request count as the baseline with a comment instead of
// failing"), this pins that observed behavior instead of asserting a bound
// the guard doesn't actually enforce here.
func TestServerChatProgressGuard(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 12; i++ {
		p := "main.go"
		if i%2 == 1 {
			p = "./main.go"
		}
		turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: `{"path":"` + p + `"}`}}})
	}
	turns = append(turns, fakeprovider.Turn{Text: "done"})
	llm := fakeprovider.NewOpenAI(t, turns...)
	cfg, ws := contractCfg(t, llm)
	call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "loop", "mode": "chat", "workspace": ws}}})
	if n := len(llm.Requests()); n != 13 {
		t.Fatalf("server made %d requests; baseline is 13 (the progress guard does not trip here because read_file's result echoes the literal path argument, so alternating \"main.go\"/\"./main.go\" never repeats a result)", n)
	}
}
