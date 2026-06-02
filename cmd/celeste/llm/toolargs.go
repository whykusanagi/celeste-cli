package llm

import (
	"encoding/json"
	"fmt"
	"strings"
)

// StripUnbackedAudioClaim removes a fabricated "Audio saved:" success string the
// model may emit as prose when it gives up issuing the TTS tool call. If no TTS
// tool actually ran this session (ttsRan == false) and the content claims a saved
// file, the claim is replaced with an honest error. Content is returned
// unchanged when TTS ran or no claim is present.
func StripUnbackedAudioClaim(content string, ttsRan bool) string {
	if ttsRan || !strings.Contains(content, "Audio saved:") {
		return content
	}
	return "I attempted to describe saved audio, but no audio file was actually generated this session (the TTS tool did not run). Please retry — no file was written."
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
