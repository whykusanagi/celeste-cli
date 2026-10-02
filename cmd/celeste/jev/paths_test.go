package jev

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRedactPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	ws := filepath.Join(home, "src", "proj")
	wsSlash := filepath.ToSlash(ws)
	cases := map[string]string{
		"edit " + ws + "/cmd/main.go now":        "edit cmd/main.go now",
		`{"path":"` + wsSlash + `/a/b.go"}`:      `{"path":"a/b.go"}`,
		"cd " + ws:                               "cd .",
		"see " + wsSlash + "/x.go:12:3.":         "see x.go:12:3.",
		"read /etc/passwd and /var/log/x.log":    "read <path> and <path>",
		"open ~/notes/plan.md":                   "open <path>",
		"open ~/src/proj/main.go":                "open main.go",
		"cat " + wsSlash + "-old/secret.txt":     "cat <path>",
		`C:\Users\someone\file.txt here`:         "<path> here",
		"https://example.com/a/b stays":          "https://example.com/a/b stays",
		"and/or, 1/2, ./rel/path, ../up/x stay":  "and/or, 1/2, ./rel/path, ../up/x stay",
		"darling~ and ~ alone stay":              "darling~ and ~ alone stay",
		"(/opt/tool/bin) [" + wsSlash + "/q.go]": "(<path>) [q.go]",
		"cmd=" + wsSlash + "/build.sh":           "cmd=build.sh",
		"no workspace given: /home/u/x":          "no workspace given: <path>",
		"cat a >/Users/alice/secret.txt":         "cat a ><path>",
		"a|/Users/alice/x":                       "a|<path>",
		"cp a;/Users/alice/x":                    "cp a;<path>",
		"a&&/Users/alice/x":                      "a&&<path>",
		"open file:///Users/alice/notes.md now":  "open file://<path> now",
		"open file://" + wsSlash + "/n.md":       "open file://n.md",
		"cat '/Users/alice/My Project/x'":        "cat '<path>'",
		`{"path":"/Users/alice/My Project/x"}`:   `{"path":"<path>"}`,
		`{"path":"` + wsSlash + `/My Dir/a.go"}`: `{"path":"My Dir/a.go"}`,
		"it's /usr/bin and it's fine":            "it's <path> and it's fine",
	}
	for in, want := range cases {
		w := ws
		if strings.HasPrefix(in, "no workspace") {
			w = ""
		}
		if got := RedactPaths(in, w); got != want {
			t.Errorf("RedactPaths(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

// Everything Ask sends passes through path and secret redaction, whatever
// the caller built (pruning excerpts, ballot state, plain text).
func TestAskRedactsPathsAndSecretsInState(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"answers":{}}`))
	}))
	defer srv.Close()
	ws := filepath.Join(t.TempDir(), "proj")
	secret := "sk-" + strings.Repeat("Y", 24)
	c := &Client{Key: "k", URL: srv.URL, Workspace: ws}
	state := map[string]any{
		"goal":  "fix " + filepath.ToSlash(ws) + "/main.go using " + secret,
		"turns": []any{map[string]any{"args": `{"path":"/usr/local/etc/x.conf"}`}},
	}
	if _, _, err := c.Ask(context.Background(), state, map[string]Question{"q": {Type: "noul", Instructions: "?"}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body["state"])
	s := string(raw)
	if strings.Contains(s, secret) || strings.Contains(s, filepath.ToSlash(ws)) || strings.Contains(s, "/usr/local") {
		t.Fatalf("state leaked: %s", s)
	}
	sent := body["state"].(map[string]any)
	args := sent["turns"].([]any)[0].(map[string]any)["args"]
	if !strings.HasPrefix(sent["goal"].(string), "fix main.go using") || args != `{"path":"<path>"}` {
		t.Errorf("state = %s", s)
	}
	if _, _, err := c.Ask(context.Background(), "plain "+filepath.ToSlash(ws)+"/a.go", map[string]Question{"q": {Type: "noul"}}); err != nil {
		t.Fatal(err)
	}
	if body["state"] != "plain a.go" {
		t.Errorf("text state = %v", body["state"])
	}
}
