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

// StripUnbackedSpawnClaim removes a fabricated "subagent spawned (id: …)"
// success string the model may emit when it can't actually drive the
// spawn_agent tool (a non-reasoning/weak model flails then claims success —
// observed with a fake id "task-47"). If no spawn_agent tool ran this run
// (spawnRan false) and the content claims a spawned subagent with an id,
// the claim is replaced with an honest error. Returned unchanged when a
// spawn ran or no claim is present. Moved here from llm/toolargs.go, next
// to its audio twin.
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
