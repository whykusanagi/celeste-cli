package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/jev"
)

// jev_prune "on": the agent's compactor asks Jev inside the loop, before
// the next request, and elides what Jev says is least needed (2.0 W3).
func TestAgentJevPruneOnElidesTheLeastNeeded(t *testing.T) {
	var asked atomic.Int32
	jevSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		var body struct {
			Questions map[string]any `json:"questions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		answers := map[string]any{}
		for id := range body.Questions {
			p := 0.9 // keep
			if id == "r1" {
				p = 0.01 // the second read is not needed
			}
			answers[id] = map[string]any{"type": "noul", "noul": p}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "jev-test", "answers": answers})
	}))
	defer jevSrv.Close()

	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{
			{ID: "a", Name: "read_file", Args: `{"path":"f0.txt"}`},
			{ID: "b", Name: "read_file", Args: `{"path":"f1.txt"}`},
			{ID: "c", Name: "read_file", Args: `{"path":"f2.txt"}`},
		}},
		// The reads must be seen before they can be pruned (#234): the
		// prune before the third request is the one Jev steers.
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{
			{ID: "d", Name: "read_file", Args: `{"path":"f3.txt"}`},
		}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: read them"},
	)
	r, _ := steerRunner(t, srv, func(c *config.Config) { c.JevPrune, c.ContextLimit = "on", 40_000 })
	r.jev = &jev.Client{Key: "k", URL: jevSrv.URL}
	for i := 0; i < 3; i++ {
		os.WriteFile(filepath.Join(r.options.Workspace, fmt.Sprintf("f%d.txt", i)), []byte(strings.Repeat("word ", 5200)), 0o644)
	}
	os.WriteFile(filepath.Join(r.options.Workspace, "f3.txt"), []byte("small"), 0o644)
	if _, err := r.RunGoal(context.Background(), "read f0, f1 and f2"); err != nil {
		t.Fatal(err)
	}
	if asked.Load() == 0 {
		t.Fatal("Jev was never asked inside the loop")
	}
	msgs := srv.Requests()[2].Body["messages"].([]any)
	elided := ""
	for _, m := range msgs {
		mm := m.(map[string]any)
		if c, _ := mm["content"].(string); mm["role"] == "tool" && strings.Contains(c, "elided to save context") {
			elided += mm["tool_call_id"].(string)
		}
	}
	// One elision meets the target: oldest-first would take a.
	if elided != "b" {
		t.Errorf("elided %q, want b (Jev: least needed)", elided)
	}
}
