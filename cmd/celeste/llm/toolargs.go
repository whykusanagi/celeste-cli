package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StripUnbackedSpawnClaim removes a fabricated "subagent spawned (id: …)" success
// string the model may emit when it can't actually drive the spawn_agent tool (a
// non-reasoning/weak model flails then claims success — observed with a fake id
// "task-47"). If no spawn_agent tool ran this run (spawnRan == false) and the
// content claims a spawned subagent with an id, the claim is replaced with an
// honest error. Returned unchanged when a spawn ran or no claim is present.
func StripUnbackedSpawnClaim(content string, spawnRan bool) string {
	if spawnRan {
		return content
	}
	lc := strings.ToLower(content)
	if strings.Contains(lc, "subagent") && strings.Contains(lc, "spawn") && strings.Contains(lc, "id:") {
		return "I described spawning a subagent, but no subagent was actually spawned this run (the spawn_agent tool did not run, so any agent id mentioned is not real). Please retry the spawn explicitly."
	}
	return content
}

// validateToolArgs returns a non-empty error message when args is present but is
// not valid JSON — the signature of a dropped/corrupted stream delta. An empty
// args string is treated as valid (no-arg tool calls); the tool itself validates
// any required fields. The returned message is safe to surface to the model.
func validateToolArgs(tool, args string) string {
	if args == "" {
		return ""
	}
	if !json.Valid([]byte(args)) {
		return fmt.Sprintf("streamed tool-call arguments for %q were not valid JSON (likely a dropped stream delta); retry the call", tool)
	}
	return ""
}
