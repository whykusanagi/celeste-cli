package server

import (
	"bufio"
	"bytes"
	"context"
	"embed"
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
	"github.com/whykusanagi/celeste-cli/cmd/celeste/memories"
)

var update = flag.Bool("update", false, "rewrite contract goldens")

//go:embed testdata/contract/*.golden
var goldenFS embed.FS

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
// per release and must be masked so goldens stay stable. db_path is included
// because the celeste_index tools cache the SQLite index under a per-HOME,
// per-workspace-hash path, which is neither a stable value nor caught by the
// workspace-path masking below (it embeds a hash of the workspace, not the
// workspace string itself).
const volatileKeys = `uptime[a-z_]*|commit|version|started_at|elapsed[a-z_]*|timestamp|duration[a-z_]*|run_id|agent_run_id|created_at|updated_at|db_path`

var volatile = regexp.MustCompile(`"(` + volatileKeys + `)"\s*:\s*("[^"]*"|[0-9.]+)`)

// volatileEscaped matches the same fields one level deep inside a JSON string.
// Several MCP tool results (celeste_status, celeste_index) wrap their JSON
// payload as a content[].text string, so in the raw response bytes its quotes
// are backslash-escaped at the point this function runs (before the outer
// json.Unmarshal below unescapes them). The plain `volatile` regex above
// never matches that escaped form, so
// "commit"/"uptime"/"version"/"elapsed"/"db_path" were slipping into the
// status and index_rebuild/index_status goldens unmasked. This regex is
// applied to the still-escaped raw string, so its replacement must also be
// escaped JSON-string content.
var volatileEscaped = regexp.MustCompile(`\\"(` + volatileKeys + `)\\"\s*:\s*(\\"(?:\\\\|[^"\\])*?\\"|[0-9.]+)`)

// matchPercent masks celeste_code_search's "NN% match" score in its
// human-readable text output. The score comes from MinHash similarity
// (codegraph/minhash.go NewMinHasher), which draws fresh crypto/rand seeds on
// every index build -- confirmed genuinely flaky by running
// TestContractCodeGraphTools 5x head-to-head: 43%, 49%, 44%, 47%, 51% for the
// identical "helper" query against identical source. Not a JSON key, so it
// isn't caught by the volatile/volatileEscaped regexes above.
var matchPercent = regexp.MustCompile(`\d+% match`)

// normalize masks fields that change per run/machine so goldens are stable
// across time, OSes and temp directories.
func normalize(b []byte, ws string) []byte {
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	if ws != "" {
		s = strings.ReplaceAll(s, ws, "<WORKSPACE>")
		s = strings.ReplaceAll(s, filepath.ToSlash(ws), "<WORKSPACE>")
		esc, _ := json.Marshal(ws)
		escStr := strings.Trim(string(esc), `"`)
		s = strings.ReplaceAll(s, escStr, "<WORKSPACE>")
		// status/index_rebuild/index_status put "workspace" inside
		// content[].text, so their payload is a JSON object encoded AGAIN as a
		// JSON string (MCP's text-content wrapping).
		// On Windows, ws contains backslashes, which get escaped once when the
		// inner JSON is built (\ -> \\) and then escaped AGAIN when that text
		// is embedded as the outer "text" string value (\\ -> \\\\), so the
		// raw response bytes contain the workspace path double-escaped
		// (C:\\\\Users\\\\...). The single-escape replace above never matches
		// that form. Escape the already-escaped string a second time to get
		// the double-escaped form and mask it too.
		esc2, _ := json.Marshal(escStr)
		s = strings.ReplaceAll(s, strings.Trim(string(esc2), `"`), "<WORKSPACE>")
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

// TestNormalizeMasksDoubleEscapedWindowsPath verifies that MCP content blocks
// wrapping a tool's JSON payload as a content[].text STRING still get masked.
// On Windows the workspace path (and any volatile field) inside it is escaped
// twice over in the raw response bytes (once building the inner JSON, once
// embedding that JSON as the outer "text" string). Built with encoding/json
// rather than hand-typed backslash literals so the escaping is exactly what
// the real server produces, not an approximation of it.
func TestNormalizeMasksDoubleEscapedWindowsPath(t *testing.T) {
	ws := `C:\Users\vssadmin\AppData\Local\Temp\TestContractStatusTakesNoArguments\001`
	inner, err := json.Marshal(map[string]any{"workspace": ws, "commit": "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(inner)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := string(normalize(raw, ws))
	for _, bad := range []string{`Users`, `AppData`, `vssadmin`, "abc123"} {
		if strings.Contains(got, bad) {
			t.Errorf("normalize kept double-escaped %q in nested content[].text:\n%s", bad, got)
		}
	}
	// json.MarshalIndent HTML-escapes '<'/'>' by default, so the marker
	// appears as <WORKSPACE> in the final bytes; check for the
	// unescaped substring rather than the literal angle brackets.
	if !strings.Contains(got, "WORKSPACE") {
		t.Errorf("normalize dropped the workspace marker entirely:\n%s", got)
	}
}

func TestNormalizeMasksDoubleEscapedWindowsDBPath(t *testing.T) {
	inner, err := json.Marshal(map[string]any{"db_path": `C:\Users\vssadmin\.celeste\index-abc123.db`, "commit": "abc123"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"content": []any{map[string]any{"type": "text", "text": string(inner)}},
	})
	if err != nil {
		t.Fatal(err)
	}

	got := string(normalize(raw, ""))
	for _, bad := range []string{"Users", "vssadmin", "index-abc123", "abc123"} {
		if strings.Contains(got, bad) {
			t.Errorf("normalize kept double-escaped db_path substring %q in nested content[].text:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "MASKED") {
		t.Errorf("normalize dropped the db_path mask entirely:\n%s", got)
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
	want, err := goldenFS.ReadFile(filepath.ToSlash(path))
	if err != nil {
		t.Fatalf("missing golden %s (run with -update once, review, commit): %v", path, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n")), got) {
		t.Errorf("MCP contract changed for %s (spec §3.1 frozen). If additive and intended, rerun with -update and commit.\n--- got ---\n%s", name, got)
	}
}

func contractCfg(t *testing.T, srv *fakeprovider.Server) (Config, string) {
	t.Helper()
	// Use one temp dir for both HOME and USERPROFILE, so the test stays
	// hermetic on Windows too, where os.UserHomeDir() reads USERPROFILE rather
	// than HOME.
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

func TestContractChatModeRunsPatchFileAndSaveMemory(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "w", Name: "write_file", Args: `{"path":"NOTES.md","content":"hello"}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "p", Name: "patch_file", Args: `{"path":"NOTES.md","old_string":"hello","new_string":"hello world","replace_all":false}`}}},
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "s", Name: "save_memory", Args: `{"name":"f1-coverage","type":"project","content":"celeste-docs and celeste-context depend on patch_file and save_memory running inside MCP chat mode"}`}}},
		fakeprovider.Turn{Text: "Done"},
	)
	cfg, ws := contractCfg(t, llm)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "write, patch, and remember NOTES.md", "mode": "chat", "workspace": ws}}})
	if b, err := os.ReadFile(filepath.Join(ws, "NOTES.md")); err != nil || string(b) != "hello world" {
		t.Fatalf("chat mode did not patch write_file output: %v %q\nresult: %s", err, b, res[1])
	}
	mem, err := memories.NewStore(ws).Load("f1-coverage")
	if err != nil {
		t.Fatalf("saved memory was not loadable: %v\nresult: %s", err, res[1])
	}
	if !strings.Contains(mem.Content, "patch_file and save_memory") {
		t.Fatalf("saved memory content = %q, want substring %q", mem.Content, "patch_file and save_memory")
	}
}

// Baseline: MCP chat strips an unbacked "Audio saved:" claim (only here today;
// 2.0 W3 moves it to every mode).
func TestServerChatStripsUnbackedAudioClaim(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "Audio saved: /tmp/x.mp3"})
	cfg, ws := contractCfg(t, llm)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "say it", "mode": "chat", "workspace": ws}}})
	var parsed struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(res[1], &parsed); err != nil || len(parsed.Content) == 0 {
		t.Fatalf("chat returned error or invalid content result: %s", res[1])
	}
	if strings.TrimSpace(parsed.Content[0].Text) == "" {
		t.Fatalf("chat returned empty content text: %s", res[1])
	}
	if strings.Contains(string(res[1]), "Audio saved:") {
		t.Fatalf("unbacked audio claim was not stripped: %s", res[1])
	}
	const replacement = "I attempted to describe saved audio, but no audio file was actually generated this session (the TTS tool did not run). Please retry — no file was written."
	if !strings.Contains(string(res[1]), replacement) {
		t.Fatalf("unbacked audio claim replacement missing: %s", res[1])
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
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "loop", "mode": "chat", "workspace": ws}}})
	if n := len(llm.Requests()); n != 3 {
		t.Fatalf("server made %d requests; identical-call guard should stop at exactly 3", n)
	}
	const stopText = "Stopped: the model made the identical tool call 3 times in a row (stuck loop)."
	if !strings.Contains(string(res[1]), stopText) {
		t.Fatalf("identical-call guard did not report tripping: %s", res[1])
	}
}

func TestContractContentTool(t *testing.T) {
	llm := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "A short post."})
	cfg, ws := contractCfg(t, llm)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste_content", "arguments": map[string]any{"prompt": "write a post", "format": "markdown"}}})
	golden(t, "celeste_content", normalize(res[1], ws))
}

// Baseline: MCP chat's progress guard (handlers.go progressGuard, fed by the
// tool-name|result signature built around line 393) stops a run whose tool
// RESULTS repeat maxNoProgressStreak (6) times in a row, even when the call
// ARGUMENTS differ each time -- catching a stuck loop the args-based
// identical-call guard (which trips on 3 byte-identical *calls*) would miss.
//
// This test uses {"path":"main.go"} vs {"path":"main.go","start_line":1}
// because the arguments differ but the result does not. read_file's result
// JSON echoes the literal "path" argument back verbatim (tools/builtin/read_file.go,
// Execute(): result["path"] = path, before resolvePath normalizes it), so
// "main.go"/"./main.go" would produce two DISTINCT results and never drive the
// guard at all (see TestServerChatProgressGuardDistinctResultsNeverTrip below,
// kept as that baseline). These argument strings differ, so the identical-call
// guard's args-based signature never repeats, but start_line's default is
// already 1, so read_file's result (including the echoed "path" and
// "start_line" fields) is byte-identical either way. Measured: exactly 6
// requests, stopped by the progress guard's own message.
func TestServerChatProgressGuard(t *testing.T) {
	var turns []fakeprovider.Turn
	for i := 0; i < 12; i++ {
		args := `{"path":"main.go"}`
		if i%2 == 1 {
			args = `{"path":"main.go","start_line":1}`
		}
		turns = append(turns, fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r", Name: "read_file", Args: args}}})
	}
	turns = append(turns, fakeprovider.Turn{Text: "done"})
	llm := fakeprovider.NewOpenAI(t, turns...)
	cfg, ws := contractCfg(t, llm)
	res := call(t, cfg, rpc{1, "tools/call", map[string]any{"name": "celeste", "arguments": map[string]any{"prompt": "loop", "mode": "chat", "workspace": ws}}})
	if n := len(llm.Requests()); n != 6 {
		t.Fatalf("server made %d requests; the progress guard (6 identical results) should stop it at exactly 6", n)
	}
	if !strings.Contains(string(res[1]), "no new result 6 turns") {
		t.Fatalf("progress guard did not report tripping: %s", res[1])
	}
}

// Baseline: kept from the original (pre-review) version of the test above.
// "main.go" and "./main.go" alternate: different args, and -- because
// read_file echoes the literal path argument into its result -- also
// different results, so the progress guard's identical-result streak never
// exceeds 1 and never trips. The loop runs to the natural end of the
// scripted turns instead. See TestServerChatProgressGuard above for the
// guard actually tripping.
func TestServerChatProgressGuardDistinctResultsNeverTrip(t *testing.T) {
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
