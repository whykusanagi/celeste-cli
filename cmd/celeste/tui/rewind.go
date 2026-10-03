package tui

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// /rewind [n] (2.0 W4 ruling 4): take back the last n prompts, restoring
// the files their turns changed from the session's checkpoints.

var errRewindCompacted = errors.New("cannot rewind past a compaction summary: those turns were summarized (use /session to go back to an older saved session instead)")

// isSummary is a message standing in for compacted history.
func isSummary(m ChatMessage) bool {
	return m.Role == "user" && strings.HasPrefix(m.Content, "<compacted-context>")
}

// isPrompt is a real user prompt: LLM-visible, not hidden, not compacted,
// not a tool result and not a summary (ruling 4).
func isPrompt(m ChatMessage) bool {
	if m.Role != "user" || m.ToolCallID != "" || isCompacted(m) || isSummary(m) {
		return false
	}
	hidden, _ := m.Metadata["hidden"].(bool)
	return !hidden
}

// rewindTarget is the index of the n-th last prompt in msgs, or of the
// first prompt when there are fewer than n. A rewind that would need
// history a compaction summary replaced is refused.
func rewindTarget(msgs []ChatMessage, n int) (int, error) {
	if n < 1 {
		n = 1
	}
	idx, seen := -1, 0
	for i := len(msgs) - 1; i >= 0 && seen < n; i-- {
		if isPrompt(msgs[i]) {
			idx, seen = i, seen+1
		}
	}
	if idx < 0 {
		return 0, errors.New("nothing to rewind")
	}
	// The plan-mode instruction belongs to the prompt it precedes (2.0 W4e).
	if idx > 0 && isPlanInstruction(msgs[idx-1]) {
		idx--
	}
	for _, m := range msgs[idx:] {
		if isCompacted(m) || isSummary(m) {
			return 0, errRewindCompacted
		}
	}
	if seen < n {
		// Fewer prompts than asked: the rest may have been summarized away.
		for _, m := range msgs[:idx] {
			if isCompacted(m) || isSummary(m) {
				return 0, errRewindCompacted
			}
		}
	}
	return idx, nil
}

// promptsFrom counts the prompts at idx and after.
func promptsFrom(msgs []ChatMessage, idx int) int {
	n := 0
	for _, m := range msgs[idx:] {
		if isPrompt(m) {
			n++
		}
	}
	return n
}

// callIDsAfter lists the tool call IDs of the assistant messages after idx,
// in order.
func callIDsAfter(msgs []ChatMessage, idx int) []string {
	var ids []string
	for _, m := range msgs[idx+1:] {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				if tc.ID != "" {
					ids = append(ids, tc.ID)
				}
			}
		}
	}
	return ids
}

// reusedCallID reports whether one of ids (the rewound turns' calls) is
// also the ID of a call before idx. Some backends reuse IDs (Gemini's were
// "call_<tool>" before 2.0, local OpenAI-compatible servers may count
// from 0 each turn), and older saved sessions keep them.
func reusedCallID(msgs []ChatMessage, idx int, ids []string) bool {
	before := map[string]bool{}
	for _, id := range callIDsAfter(msgs[:idx], -1) {
		before[id] = true
	}
	for _, id := range ids {
		if before[id] {
			return true
		}
	}
	return false
}

// rewind runs /rewind [n] (ruling 4): restore the files, truncate the chat
// before the n-th last prompt, put that prompt back in the input box and
// save. Nothing changes when the files cannot be restored.
func (m AppModel) rewind(args []string) AppModel {
	n := 1
	if len(args) > 0 {
		v, err := strconv.Atoi(args[0])
		if err != nil || v < 1 || len(args) > 1 {
			m.chat = m.chat.AddSystemMessage("Usage: /rewind [n] — take back the last n prompts (default 1).")
			return m
		}
		n = v
	}
	if m.summarizing {
		m.chat = m.chat.AddSystemMessage("Rewind: a context summary is being written; try again when it is done.")
		return m
	}
	msgs := m.chat.GetMessages()
	idx, err := rewindTarget(msgs, n)
	if err != nil {
		m.chat = m.chat.AddSystemMessage("Rewind: " + err.Error())
		return m
	}
	var res RewindResult
	checkpointsOff := false
	if ids := callIDsAfter(msgs, idx); len(ids) > 0 {
		if cp, ok := m.llmClient.(Checkpointer); ok {
			if reusedCallID(msgs, idx, ids) {
				// The store's first entry for a reused ID may be a kept
				// turn's: restoring from it would undo that turn's changes
				// too (W4 review 1).
				m.chat = m.chat.AddSystemMessage("Rewind: this provider reuses tool call IDs, so celeste cannot tell which file changes belong to those turns; nothing was changed. Use /undo to take back file changes one at a time.")
				return m
			}
			res, err = cp.RewindTo(ids)
			switch {
			case errors.Is(err, ErrCheckpointsOff):
				checkpointsOff = true
			case err != nil:
				line := "Rewind: restoring files failed: " + err.Error()
				if len(res.Restored) > 0 {
					line += fmt.Sprintf(" (already restored: %s; the chat was not changed)", strings.Join(res.Restored, ", "))
				}
				m.chat = m.chat.AddSystemMessage(line)
				return m
			}
		}
	}
	restored := res.Restored
	rewound := promptsFrom(msgs, idx)
	promptText := msgs[idx].Content
	m.chat = m.chat.Clear().RestoreMessages(msgs[:idx])
	m.input = m.input.SetValue(promptText)
	m.persistSession()
	line := fmt.Sprintf("Rewound %d prompt(s); ", rewound)
	switch {
	case checkpointsOff:
		line += "file checkpoints are off; files were not restored."
	case len(restored) == 0:
		line += "no checkpointed file changes found for those turns."
	default:
		line += fmt.Sprintf("restored %d file change(s): %s.", len(restored), strings.Join(restored, ", "))
	}
	if res.Partial {
		line += " Some changes were too old to restore; check /diff."
	}
	if spawnedAfter(msgs, idx) {
		// A subagent's writes are filed under its own call IDs, which
		// the chat never sees: they are restored only when a change of
		// the rewound turns came before them.
		line += " Subagents ran in those turns; check /diff for changes of theirs that were not restored."
	}
	m.chat = m.chat.AddSystemMessage(line)
	return m
}

// spawnedAfter reports whether a subagent was spawned after idx.
func spawnedAfter(msgs []ChatMessage, idx int) bool {
	for _, m := range msgs[idx+1:] {
		for _, tc := range m.ToolCalls {
			if tc.Name == "spawn_agent" {
				return true
			}
		}
	}
	return false
}
