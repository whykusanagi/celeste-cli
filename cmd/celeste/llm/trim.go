package llm

import (
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// maxToolMsgBytes bounds a single tool-result message inside an outbound request.
// It matches read_file's returned-content budget so a request never carries a tool
// blob big enough to blow the history cap or time out the stream.
const maxToolMsgBytes = 48 * 1024

// trimToolResults returns a copy of msgs in which every *text* tool-result larger
// than budget is truncated on a line boundary with a notice appended. It is
// copy-on-write: the caller's slice is never mutated (that would corrupt session
// history), and when nothing needs trimming the original slice is returned. Non-
// tool messages and image tool-results are left untouched — truncating either
// would corrupt the conversation or the image payload. The bool reports whether
// anything was trimmed.
func trimToolResults(msgs []tui.ChatMessage, budget int) ([]tui.ChatMessage, bool) {
	if budget <= 0 {
		budget = maxToolMsgBytes
	}
	out := msgs
	trimmed := false
	for i := range msgs {
		m := msgs[i]
		if m.Role != "tool" || len(m.Content) <= budget || isImageMsg(m) {
			continue
		}
		if !trimmed { // copy-on-write on first hit only
			out = make([]tui.ChatMessage, len(msgs))
			copy(out, msgs)
			trimmed = true
		}
		out[i].Content = truncateWithNotice(m.Content, budget)
	}
	return out, trimmed
}

// isImageMsg reports whether a tool message carries base64 image data (which must
// not be text-truncated).
func isImageMsg(m tui.ChatMessage) bool {
	if m.Metadata == nil {
		return false
	}
	t, _ := m.Metadata["type"].(string)
	return t == "image"
}

// truncateWithNotice cuts s to at most budget bytes on a line boundary and appends
// a notice telling the model how to recover the rest.
func truncateWithNotice(s string, budget int) string {
	notice := fmt.Sprintf("\n\n[celeste: tool result truncated to ~%d bytes for transport; original was %d bytes. Re-read specific lines with read_file start_line/end_line, or use search for a targeted lookup.]", budget, len(s))
	if len(notice) >= budget { // pathological tiny budget: hard cut
		return s[:budget]
	}
	head := lineBoundaryCut(s, budget-len(notice))
	return s[:head] + notice
}

// lineBoundaryCut returns the largest index <= limit ending on a newline, so a cut
// never splits a line. Falls back to limit when the head has no newline.
func lineBoundaryCut(s string, limit int) int {
	if limit >= len(s) {
		return len(s)
	}
	if nl := strings.LastIndexByte(s[:limit], '\n'); nl >= 0 {
		return nl + 1
	}
	return limit
}
