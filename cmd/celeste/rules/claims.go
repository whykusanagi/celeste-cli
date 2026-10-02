package rules

import "strings"

// StripUnbackedAudioClaim replaces a fabricated "Audio saved:" success
// string in a final reply when no TTS tool ran (ttsRan false). It is the
// unbacked-audio-claim rule's backstop: MCP chat applies it to every reply,
// whether stream rules are off, in shadow, or acting and out of retries.
// Moved here from llm/toolargs.go (2.0 W3).
func StripUnbackedAudioClaim(content string, ttsRan bool) string {
	if ttsRan || !strings.Contains(content, "Audio saved:") {
		return content
	}
	return "I attempted to describe saved audio, but no audio file was actually generated this session (the TTS tool did not run). Please retry — no file was written."
}
