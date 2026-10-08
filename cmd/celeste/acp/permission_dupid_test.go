package acp

import (
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// Two calls in one turn that share a provider ID are asked
// about one by one, each with its own title and input: the approval for
// one is never shown with the other's details.
func TestPermissionBoundToTheAskingCall(t *testing.T) {
	turn := fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{
		{ID: "dup", Name: "write_file", Args: `{"path":"a.txt","content":"x"}`},
		{ID: "dup", Name: "write_file", Args: `{"path":"b.txt","content":"y"}`},
	}}
	srv := fakeprovider.NewOpenAI(t, turn, fakeprovider.Turn{Text: "done"})
	c := newTestClient(t, testConfig(srv, 0))
	var mu sync.Mutex
	var titles, ids []string
	c.permit = func(p map[string]any) map[string]any {
		mu.Lock()
		defer mu.Unlock()
		tc := p["toolCall"].(map[string]any)
		title, _ := tc["title"].(string)
		raw, _ := tc["rawInput"].(map[string]any)
		path, _ := raw["path"].(string)
		if !strings.HasSuffix(title, path) {
			t.Errorf("ask title %q does not match its input path %q", title, path)
		}
		titles = append(titles, title)
		tid, _ := tc["toolCallId"].(string)
		ids = append(ids, tid)
		return selected(OptionAllowOnce)
	}
	sid := c.newSession(t.TempDir())
	if _, err := c.call("session/prompt", textPrompt(sid, "write two files")); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	sort.Strings(titles)
	if strings.Join(titles, ",") != "write_file: a.txt,write_file: b.txt" {
		t.Fatalf("ask titles = %v, want one per call", titles)
	}
	if len(ids) != 2 || ids[0] == ids[1] {
		t.Fatalf("toolCallIds = %v, want two distinct", ids)
	}
}
