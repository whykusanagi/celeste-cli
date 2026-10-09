package loop

import (
	"context"
	"regexp"
	"strings"
	"testing"

	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

var recallIDInNote = regexp.MustCompile(`recall_tool_result with id "([^"]+)"`)

// A session or call id longer than a spill directory or file name may be
// still spills, under a stable shortened name, and the result can be
// recalled by the id the notice names.
func TestLoopLongIDsStillSpillAndRecall(t *testing.T) {
	content := strings.Repeat("q", 200*1024)
	big := &fakeTool{name: "big", run: func(context.Context, map[string]any) (tools.ToolResult, error) {
		return tools.ToolResult{Content: content}, nil
	}}
	for _, tc := range []struct{ name, session, call string }{
		{"long session", strings.Repeat("s", 200), "c1"},
		{"long call", "s", strings.Repeat("c", 200)},
		{"both long", strings.Repeat("a.", 100), strings.Repeat("b/", 100)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := execLoop(t, big)
			l.SessionID = tc.session
			out := run(l, llm.ToolCallResult{ID: tc.call, Name: "big", Arguments: `{}`})
			got := out.messages[0].Content
			if strings.Contains(got, "could not be saved") {
				t.Fatalf("the spill failed for a long id: %q", got[len(got)-400:])
			}
			m := recallIDInNote.FindStringSubmatch(got)
			if m == nil {
				t.Fatalf("no recall id in the notice: %q", got[len(got)-400:])
			}
			full, err := ctxmgr.LoadSpilled(l.SpillDir, m[1])
			if err != nil || full != content {
				t.Fatalf("recall %q: %d bytes, err %v", m[1], len(full), err)
			}
		})
	}
	// The shortened name is stable for one id and differs between ids.
	a := &Loop{SessionID: strings.Repeat("s", 200)}
	b := &Loop{SessionID: strings.Repeat("s", 199) + "t"}
	a2 := &Loop{SessionID: strings.Repeat("s", 200)}
	if a.sessionID() != a2.sessionID() || a.sessionID() == b.sessionID() || len(a.sessionID()) > 128 {
		t.Fatalf("shortened session ids: %q, %q", a.sessionID(), b.sessionID())
	}
}
