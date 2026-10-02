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
	var restored []string
	if ids := callIDsAfter(msgs, idx); len(ids) > 0 {
		if cp, ok := m.llmClient.(Checkpointer); ok {
			if restored, err = cp.RewindTo(ids); err != nil {
				line := "Rewind: restoring files failed: " + err.Error()
				if len(restored) > 0 {
					line += fmt.Sprintf(" (already restored: %s; the chat was not changed)", strings.Join(restored, ", "))
				}
				m.chat = m.chat.AddSystemMessage(line)
				return m
			}
		}
	}
	rewound := promptsFrom(msgs, idx)
	promptText := msgs[idx].Content
	m.chat = m.chat.Clear().RestoreMessages(msgs[:idx])
	m.input = m.input.SetValue(promptText)
	m.persistSession()
	line := fmt.Sprintf("Rewound %d prompt(s); ", rewound)
	if len(restored) == 0 {
		line += "no files were changed by those turns."
	} else {
		line += fmt.Sprintf("restored %d file change(s): %s.", len(restored), strings.Join(restored, ", "))
	}
	m.chat = m.chat.AddSystemMessage(line)
	return m
}
