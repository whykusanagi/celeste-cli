package subagents

import (
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

func TestSpawnAgentToolTimeout(t *testing.T) {
	if got := tools.TimeoutFor(NewSpawnAgentTool(nil), 45*time.Second); got != 10*time.Minute {
		t.Fatalf("spawn_agent timeout = %v, want 10m (subagents manage their own turn limits)", got)
	}
}
