package llm

import (
	"encoding/json"
	"fmt"
)

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
