package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
)

// jev_gate "on" without a key falls back to the heuristic, which flags
// rm -rf src (build output is exempt): the headless run's call is denied
// with the gate's reason, though -auto-approve trusts every other call
// (2.0 W3).
func TestAgentJevGateAddsAnAskHeadless(t *testing.T) {
	t.Setenv("TYPESAFE_API_KEY", "")
	srv := fakeprovider.NewOpenAI(t,
		fakeprovider.Turn{ToolCalls: []fakeprovider.ToolCall{{ID: "b", Name: "bash", Args: `{"command":"rm -rf src"}`}}},
		fakeprovider.Turn{Text: "TASK_COMPLETE: asked first"},
	)
	r, _ := steerRunner(t, srv, func(c *config.Config) { c.JevGate = "on" })
	if _, err := r.RunGoal(context.Background(), "tidy up"); err != nil {
		t.Fatal(err)
	}
	msgs := srv.Requests()[1].Body["messages"].([]any)
	var result string
	for _, m := range msgs {
		if mm := m.(map[string]any); mm["role"] == "tool" {
			result, _ = mm["content"].(string)
		}
	}
	if !strings.Contains(result, "jev_gate: destructive") || !strings.Contains(result, "Permission denied") {
		t.Errorf("the bash result = %q", result)
	}
}
