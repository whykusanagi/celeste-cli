package decide

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
)

var typed = []Question{
	{ID: "loop", Kind: YesNo, Text: "Looping?", True: "same calls", False: "progress"},
	{ID: "track", Kind: Score, Text: "On track?", Levels: []string{"off", "partly", "on"}},
	{ID: "lane", Kind: Choice, Text: "Which lane?", Options: map[string]string{"code": "programming", "content": ""}},
}

func TestJevOracleSendsTypedQuestionsAndRedactsState(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{
			"loop":{"type":"noul","noul":0.82},
			"track":{"type":"score","score":1.6,"probabilities":{"1":0.4,"2":0.6},"confidence":0.5},
			"lane":{"type":"choice","choice":"code","probabilities":{"code":0.7,"content":0.3},"confidence":0.6}}}`))
	}))
	defer srv.Close()
	secret := "sk-" + strings.Repeat("X", 24)
	st := State{Goal: "deploy with " + secret, Turns: []TurnView{{Calls: []CallView{{Tool: "bash", Args: `{"command":"echo ` + secret + `"}`}}}}}
	got, err := Jev{Client: &jev.Client{Key: "k", URL: srv.URL}}.Ask(context.Background(), st.String(), typed)
	if err != nil {
		t.Fatal(err)
	}
	if got["loop"].P != 0.82 || got["track"].Score != 1.6 || got["lane"].Choice != "code" || got["lane"].Source != "jev" {
		t.Errorf("answers = %+v", got)
	}
	raw, _ := json.Marshal(body["state"])
	if strings.Contains(string(raw), secret) {
		t.Fatalf("state sent unredacted: %s", raw)
	}
	qs := body["questions"].(map[string]any)
	if qs["track"].(map[string]any)["type"] != "score" || qs["lane"].(map[string]any)["criteria"].(map[string]any)["content"] != nil {
		t.Errorf("questions = %v", qs)
	}
}

func TestLLMOracleParsesLenientJSON(t *testing.T) {
	var prompt string
	o := LLM{Complete: func(_ context.Context, system, user string) (string, error) {
		prompt = user
		return "Sure:\n```json\n{\"loop\": 0.9, \"track\": 2, \"lane\": \"rust\"}\n```", nil
	}}
	got, err := o.Ask(context.Background(), State{Goal: "g"}.String(), typed)
	if err != nil {
		t.Fatal(err)
	}
	if got["loop"].P != 0.9 || got["track"].Score != 2 || got["loop"].Source != "llm" {
		t.Errorf("answers = %+v", got)
	}
	if _, ok := got["lane"]; ok {
		t.Error("a choice outside the options must be dropped (the heuristic answers it)")
	}
	for _, want := range []string{"loop (yes/no)", "0 = off", `"code" (programming)`} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if _, err := (LLM{Complete: func(context.Context, string, string) (string, error) { return "no idea", nil }}).Ask(context.Background(), "s", typed); err == nil {
		t.Error("a reply without JSON must be an error (Guarded falls back)")
	}
}

func TestNewPicksTheConfiguredOracle(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	var logged []string
	logf := func(s string) { logged = append(logged, s) }
	if g := New("jev", nil, "ballot", "", logf).(guarded); g.primary != nil {
		t.Errorf("jev without a key must be the heuristic, got %T", g.primary)
	}
	if g := New("llm", nil, "ballot", "", logf).(guarded); g.primary != nil {
		t.Errorf("llm without a small model must be the heuristic, got %T", g.primary)
	}
	if g := New("", nil, "ballot", "", logf).(guarded); g.primary != nil {
		t.Error("the default is the heuristic")
	}
	if g := New("llm", func(context.Context, string, string) (string, error) { return "{}", nil }, "ballot", "", logf).(guarded); g.primary == nil {
		t.Error("llm with a small model must use it")
	}
	if len(logged) != 2 {
		t.Errorf("logged = %v, want one line each for jev and llm", logged)
	}
}

// Both model-backed oracles send the state with secrets redacted and file
// paths made workspace-relative or <path> (coordinator ruling, W3-2).
func TestOraclesRedactPathsBeforeSending(t *testing.T) {
	ws := filepath.Join(t.TempDir(), "proj")
	secret := "ghp_" + strings.Repeat("Z", 30)
	st := State{
		Goal:  "fix " + filepath.ToSlash(ws) + "/cmd/main.go",
		Turns: []TurnView{{Calls: []CallView{{Tool: "read_file", Args: `{"path":"/etc/hosts"}`, Result: "token: " + secret}}}},
	}.String()
	check := func(who, sent string) {
		t.Helper()
		if strings.Contains(sent, filepath.ToSlash(ws)) || strings.Contains(sent, "/etc/hosts") || strings.Contains(sent, secret) {
			t.Errorf("%s sent unredacted state: %s", who, sent)
		}
		if !strings.Contains(sent, "fix cmd/main.go") {
			t.Errorf("%s lost the workspace-relative path: %s", who, sent)
		}
	}
	var prompt string
	_, _ = LLM{Workspace: ws, Complete: func(_ context.Context, _, user string) (string, error) {
		prompt = user
		return "{}", nil
	}}.Ask(context.Background(), st, typed)
	check("llm", prompt)

	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"answers":{}}`))
	}))
	defer srv.Close()
	prev := newJevClient
	newJevClient = func() (*jev.Client, error) { return &jev.Client{Key: "k", URL: srv.URL}, nil }
	defer func() { newJevClient = prev }()
	_, _ = NewJev("ballot", ws, nil).Ask(context.Background(), st, typed)
	raw, _ := json.Marshal(body["state"])
	var sent bytes.Buffer
	_ = json.Indent(&sent, raw, "", "")
	check("jev", strings.ReplaceAll(sent.String(), `<`, "<"))
}
