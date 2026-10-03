package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// Review Focus 2: setup warnings, hook stderr, log output and a stray
// write to os.Stdout during a session never reach the protocol stream.
func TestStdoutCarriesOnlyProtocol(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "r1", Name: "read_file", Args: `{"path":"a.txt"}`}}},
		fakeprovider.Turn{Text: "done"})
	loadConfig := func() (*config.Config, error) {
		return &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10}, nil
	}

	ws := t.TempDir()
	write := func(path, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(ws, "a.txt"), "hello")
	// An untrusted repo grimoire's stream rules and an untrusted repo hook:
	// setup warnings.
	write(filepath.Join(ws, ".grimoire"), "## Stream Rules\n### repo-rule\n---\ncondition: bar\n---\nNo bar.\n")
	writeHooksFile(t, filepath.Join(ws, ".celeste", "hooks.json"), hookDef(t, hooks.EventPreToolUse, "*", "exit", "0", "repo-hook-stderr"))
	// A trusted global hook that writes to its stderr on every tool call.
	writeHooksFile(t, globalHooks(home), hookDef(t, hooks.EventPreToolUse, "*", "exit", "0", "global-hook-stderr"))

	inR, inW := io.Pipe()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	var errMu sync.Mutex
	errDone := make(chan struct{})
	go func() {
		defer close(errDone)
		buf := make([]byte, 4096)
		for {
			n, err := errR.Read(buf)
			errMu.Lock()
			stderr.Write(buf[:n])
			errMu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ran := make(chan error, 1)
	go func() {
		ran <- runACP(ctx, inR, outW, errW, loadConfig)
		outW.Close()
		errW.Close()
	}()

	lines := make(chan []byte, 64)
	go func() {
		defer close(lines)
		r := bufio.NewReader(outR)
		for {
			line, err := r.ReadBytes('\n')
			if len(line) > 0 {
				lines <- line
			}
			if err != nil {
				return
			}
		}
	}()
	// call sends a request and reads stdout up to its answer, checking
	// every line on the way.
	nextID := 0
	call := func(method string, params any) json.RawMessage {
		t.Helper()
		nextID++
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": nextID, "method": method, "params": params})
		if _, err := inW.Write(append(b, '\n')); err != nil {
			t.Fatal(err)
		}
		timeout := time.After(60 * time.Second)
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("%s: stdout closed", method)
				}
				var m struct {
					JSONRPC string          `json:"jsonrpc"`
					ID      json.RawMessage `json:"id"`
					Method  string          `json:"method"`
					Result  json.RawMessage `json:"result"`
					Error   json.RawMessage `json:"error"`
				}
				if err := json.Unmarshal(line, &m); err != nil || m.JSONRPC != "2.0" {
					t.Fatalf("stdout carried a line that is not JSON-RPC: %q", line)
				}
				if m.Method == "session/request_permission" {
					ans, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"outcome": map[string]any{"outcome": "selected", "optionId": "allow_once"}}})
					_, _ = inW.Write(append(ans, '\n'))
					continue
				}
				if m.Method == "" && string(m.ID) == fmt.Sprint(nextID) {
					if len(m.Error) > 0 {
						t.Fatalf("%s failed: %s", method, m.Error)
					}
					return m.Result
				}
			case <-timeout:
				t.Fatalf("%s: no answer", method)
			}
		}
	}

	call("initialize", map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
	var ns struct {
		SessionID string `json:"sessionId"`
	}
	_ = json.Unmarshal(call("session/new", map[string]any{"cwd": ws, "mcpServers": []any{}}), &ns)
	// A dependency printing to stdout mid-session.
	fmt.Println("stray-stdout-write")
	res := call("session/prompt", map[string]any{"sessionId": ns.SessionID, "prompt": []any{map[string]any{"type": "text", "text": "read a.txt"}}})
	if !strings.Contains(string(res), "end_turn") {
		t.Fatalf("prompt = %s", res)
	}

	inW.Close() // the editor hangs up: celeste acp exits
	select {
	case err := <-ran:
		if err != nil {
			t.Fatalf("runACP = %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runACP did not return after stdin closed")
	}
	for line := range lines {
		var m map[string]any
		if json.Unmarshal(line, &m) != nil || m["jsonrpc"] != "2.0" {
			t.Fatalf("stdout carried a line that is not JSON-RPC: %q", line)
		}
	}
	<-errDone
	errMu.Lock()
	defer errMu.Unlock()
	if !strings.Contains(stderr.String(), "stray-stdout-write") {
		t.Fatalf("a stray stdout write must go to stderr; stderr = %q", stderr.String())
	}
	if os.Stdout == outW || os.Stdout == errW {
		t.Fatal("runACP must restore os.Stdout")
	}
}

func TestACPCommandIsRouted(t *testing.T) {
	f := &fakeRunner{}
	if code := run([]string{"acp"}, f, io.Discard, io.Discard); code != 0 || f.lastCall != "acp" {
		t.Fatalf("run(acp) = %d, lastCall %q", code, f.lastCall)
	}
	if !strings.Contains(usageText, "acp ") {
		t.Fatal("celeste help does not list acp")
	}
}
