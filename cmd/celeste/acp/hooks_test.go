package acp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/hooktest"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/sandbox"
)

// repoWithPromptHook is a workspace whose .celeste/hooks.json adds marker
// as context to every prompt (UserPromptSubmit).
func repoWithPromptHook(t *testing.T, marker string) string {
	t.Helper()
	ws := t.TempDir()
	b, err := json.Marshal(map[string]any{"hooks": []any{map[string]any{
		"event": "UserPromptSubmit", "command": hooktest.Command(t, "context", marker),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws, ".celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".celeste", "hooks.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

// hookAsks answers the repo-hook asks with answer (an option ID) and every
// tool ask with allow_once, counting the hook asks.
type hookAsks struct {
	mu     sync.Mutex
	asks   []map[string]any
	answer string
}

func (h *hookAsks) permit(p map[string]any) map[string]any {
	tc := p["toolCall"].(map[string]any)
	if title, _ := tc["title"].(string); strings.HasPrefix(title, "Run repository hooks") {
		h.mu.Lock()
		h.asks = append(h.asks, p)
		h.mu.Unlock()
		return selected(h.answer)
	}
	return selected(OptionAllowOnce)
}

func (h *hookAsks) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.asks)
}

// Review Focus 3: an untrusted repo hook is asked about once, at the
// session's first prompt. "Skip" runs the prompt without it and never asks
// again; "Trust these hooks" stores trust and the hook runs in that same
// prompt (ruling 9, F0).
func TestRepoHooksAskOnceAtFirstPrompt(t *testing.T) {
	t.Run("skip", func(t *testing.T) {
		srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"})
		c := newTestClient(t, testConfig(srv, 0))
		h := &hookAsks{answer: OptionRejectOnce}
		c.permit = h.permit
		ws := repoWithPromptHook(t, "HOOK-RAN")
		sid := c.newSession(ws)
		if h.count() != 0 {
			t.Fatal("session/new asked about the hooks; the ask belongs to the first prompt")
		}
		for _, text := range []string{"first", "second"} {
			if res, err := c.call("session/prompt", textPrompt(sid, text)); err != nil || stopReason(t, res) != "end_turn" {
				t.Fatalf("%s prompt = %s %v", text, res, err)
			}
		}
		if h.count() != 1 {
			t.Fatalf("hook asks = %d, want 1", h.count())
		}
		ask := h.asks[0]
		tc := ask["toolCall"].(map[string]any)
		if tc["kind"] != "execute" || !strings.Contains(tc["title"].(string), filepath.Join(".celeste", "hooks.json")) ||
			!strings.Contains(mustJSON(tc["content"]), "UserPromptSubmit") {
			t.Fatalf("hook ask = %+v", ask)
		}
		opts := mustJSON(ask["options"])
		if !strings.Contains(opts, `"Trust these hooks"`) || !strings.Contains(opts, `"Skip"`) {
			t.Fatalf("hook ask options = %s", opts)
		}
		for i, r := range srv.Requests() {
			if strings.Contains(string(r.Raw), "HOOK-RAN") {
				t.Fatalf("request %d carries the skipped hook's context", i)
			}
		}
		if hooks.LoadTrust(c.home).Status(hooks.Source{Path: filepath.Join(ws, ".celeste", "hooks.json")}) == hooks.Trusted {
			t.Fatal("Skip stored trust")
		}
	})
	t.Run("trust", func(t *testing.T) {
		srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"})
		c := newTestClient(t, testConfig(srv, 0))
		h := &hookAsks{answer: OptionAllowAlways}
		c.permit = h.permit
		ws := repoWithPromptHook(t, "HOOK-RAN")
		sid := c.newSession(ws)
		if res, err := c.call("session/prompt", textPrompt(sid, "first")); err != nil || stopReason(t, res) != "end_turn" {
			t.Fatalf("prompt = %s %v", res, err)
		}
		if h.count() != 1 {
			t.Fatalf("hook asks = %d, want 1", h.count())
		}
		if reqs := srv.Requests(); len(reqs) != 1 || !strings.Contains(string(reqs[0].Raw), "HOOK-RAN") {
			t.Fatal("the trusted hook did not run in the prompt that asked")
		}
		data, err := os.ReadFile(hooks.TrustPath(c.home))
		if err != nil || !strings.Contains(string(data), "hooks.json") {
			t.Fatalf("trust store = %s (%v)", data, err)
		}
		// A new session in the same repo runs the hook without asking.
		srv.Push(fakeprovider.Turn{Text: "two"})
		sid2 := c.newSession(ws)
		if _, err := c.call("session/prompt", textPrompt(sid2, "again")); err != nil {
			t.Fatal(err)
		}
		if h.count() != 1 {
			t.Fatalf("a trusted hook was asked about again (%d asks)", h.count())
		}
	})
}

// A session/cancel while the hook ask is open cancels the prompt; the next
// prompt asks again (the user never answered).
func TestRepoHookAskCancelled(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "after"})
	c := newTestClient(t, testConfig(srv, 0))
	var mu sync.Mutex
	asks := 0
	c.permit = func(map[string]any) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		asks++
		if asks == 1 {
			return nil // never answered
		}
		return selected(OptionRejectOnce)
	}
	sid := c.newSession(repoWithPromptHook(t, "HOOK-RAN"))
	first := c.callAsync("session/prompt", textPrompt(sid, "first"))
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return asks == 1 })
	c.notify("session/cancel", map[string]any{"sessionId": sid})
	r := <-first
	if r.err != nil || stopReason(t, r.result) != "cancelled" {
		t.Fatalf("cancelled prompt = %s %v", r.result, r.err)
	}
	if res, err := c.call("session/prompt", textPrompt(sid, "second")); err != nil || stopReason(t, res) != "end_turn" {
		t.Fatalf("second prompt = %s %v", res, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if asks != 2 {
		t.Fatalf("asks = %d, want 2", asks)
	}
}

// waitFor polls cond for up to 30 s.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// repoWithSandboxOff is a workspace whose .celeste/config.json turns the
// bash sandbox off: a loosening that applies only once trusted.
func repoWithSandboxOff(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, ".celeste"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, ".celeste", "config.json"), []byte(`{"sandbox":{"enabled":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return ws
}

// trustAsks answers every repo trust ask (hooks, rules, sandbox) with
// answer and every tool ask with allow_once, recording the trust asks.
func trustAsks(h *hookAsks) func(map[string]any) map[string]any {
	return func(p map[string]any) map[string]any {
		tc := p["toolCall"].(map[string]any)
		if id, _ := tc["toolCallId"].(string); strings.HasPrefix(id, "hooks_") {
			h.mu.Lock()
			h.asks = append(h.asks, p)
			h.mu.Unlock()
			return selected(h.answer)
		}
		return selected(OptionAllowOnce)
	}
}

// A repository's sandbox loosening is asked about as sandbox settings,
// never as hooks, and names the real config file: Skip keeps the sandbox
// as it was, Trust stores the approval and the loosening applies.
func TestRepoSandboxAsk(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer string
	}{{"skip", OptionRejectOnce}, {"trust", OptionAllowAlways}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"})
			c := newTestClient(t, testConfig(srv, 0))
			h := &hookAsks{answer: tc.answer}
			c.permit = trustAsks(h)
			ws := repoWithSandboxOff(t)
			sid := c.newSession(ws)
			if res, err := c.call("session/prompt", textPrompt(sid, "first")); err != nil || stopReason(t, res) != "end_turn" {
				t.Fatalf("prompt = %s %v", res, err)
			}
			if h.count() != 1 {
				t.Fatalf("trust asks = %d, want 1", h.count())
			}
			ask := h.asks[0]
			call := ask["toolCall"].(map[string]any)
			title, _ := call["title"].(string)
			body := mustJSON(call["content"])
			opts := mustJSON(ask["options"])
			rel := filepath.Join(".celeste", "config.json")
			if title != "Apply the sandbox settings in "+rel+"?" {
				t.Fatalf("title = %q", title)
			}
			if !strings.Contains(body, "loosen the sandbox") || strings.Contains(body, "These commands run") {
				t.Fatalf("body = %s", body)
			}
			if !strings.Contains(opts, `"Trust these settings"`) || strings.Contains(opts, "hooks") {
				t.Fatalf("options = %s", opts)
			}
			locs := call["locations"].([]any)
			loc, _ := locs[0].(map[string]any)["path"].(string)
			if loc != filepath.Join(ws, ".celeste", "config.json") {
				t.Fatalf("location = %q, want the config file", loc)
			}
			if _, err := os.Stat(loc); err != nil {
				t.Fatalf("location does not exist: %v", err)
			}
			s := c.agent.session(sid)
			s.mu.Lock()
			enabled := s.env.SandboxPolicy.Enabled
			s.mu.Unlock()
			if tc.answer == OptionRejectOnce {
				if enabled != sandbox.DefaultEnabled {
					t.Fatalf("Skip changed the sandbox: enabled = %v", enabled)
				}
				data, _ := os.ReadFile(hooks.TrustPath(c.home))
				if strings.Contains(string(data), "#sandbox") {
					t.Fatal("Skip stored trust")
				}
				return
			}
			if enabled {
				t.Fatal("Trust did not apply the repository's sandbox settings")
			}
			data, err := os.ReadFile(hooks.TrustPath(c.home))
			if err != nil || !strings.Contains(string(data), "#sandbox") {
				t.Fatalf("trust store = %s (%v)", data, err)
			}
		})
	}
}

// A session reloaded in another folder asks about that folder's hooks at
// its next prompt, and never about the old folder's.
func TestReloadInAnotherFolderAsksItsHooks(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"})
	c := newTestClient(t, testConfig(srv, 0))
	h := &hookAsks{answer: OptionAllowAlways}
	c.permit = h.permit
	sid := c.newSession(t.TempDir())
	if _, err := c.call("session/prompt", textPrompt(sid, "first")); err != nil {
		t.Fatal(err)
	}
	if h.count() != 0 {
		t.Fatalf("asks in a folder without hooks = %d", h.count())
	}
	ws2 := repoWithPromptHook(t, "SECOND-FOLDER-HOOK")
	if _, err := c.call("session/load", loadParams(sid, ws2)); err != nil {
		t.Fatal(err)
	}
	if _, err := c.call("session/prompt", textPrompt(sid, "second")); err != nil {
		t.Fatal(err)
	}
	if h.count() != 1 {
		t.Fatalf("asks after reloading in a folder with hooks = %d, want 1", h.count())
	}
	reqs := srv.Requests()
	if !strings.Contains(string(reqs[len(reqs)-1].Raw), "SECOND-FOLDER-HOOK") {
		t.Fatal("the trusted hook of the new folder did not run")
	}
}

// Pending asks of the old folder are dropped when a session is reloaded
// elsewhere: the user is never asked about a file outside the new cwd.
func TestReloadDropsOldFolderAsks(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"})
	c := newTestClient(t, testConfig(srv, 0))
	h := &hookAsks{answer: OptionRejectOnce}
	c.permit = h.permit
	ws1 := repoWithPromptHook(t, "OLD")
	sid := c.newSession(ws1)
	if _, err := c.call("session/load", loadParams(sid, t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if _, err := c.call("session/prompt", textPrompt(sid, "first")); err != nil {
		t.Fatal(err)
	}
	if h.count() != 0 {
		t.Fatalf("asked about the old folder's hooks: %v", h.asks)
	}
}
