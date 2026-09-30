package loop

import (
	"context"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// needsCheck reports a user prompt CheckPrompt has not seen: not marked
// checked, and not hidden (the app's own directives and summaries).
func needsCheck(m Message) bool {
	if m.Role != "user" {
		return false
	}
	done, _ := m.Metadata[tui.MetaPromptHookDone].(bool)
	hidden, _ := m.Metadata["hidden"].(bool)
	return !done && !hidden
}

// markChecked returns m marked as past CheckPrompt, with its metadata copied.
func markChecked(m Message) Message {
	meta := make(map[string]any, len(m.Metadata)+1)
	for k, v := range m.Metadata {
		meta[k] = v
	}
	meta[tui.MetaPromptHookDone] = true
	m.Metadata = meta
	return m
}

// checkPrompts runs CheckPrompt over msgs, in order. Blocked prompts leave
// the result (EventPromptBlocked); allowed ones are marked checked. On an
// error it returns msgs unchanged with the error.
func (l *Loop) checkPrompts(ctx context.Context, msgs []Message) (out []Message, allowed, blocked int, err error) {
	out = make([]Message, 0, len(msgs))
	for _, m := range msgs {
		if !needsCheck(m) {
			out = append(out, m)
			continue
		}
		checked, v, cerr := l.CheckPrompt(ctx, m)
		if cerr != nil {
			return msgs, allowed, blocked, cerr
		}
		if v.Blocked {
			blocked++
			l.emit(Event{Kind: EventPromptBlocked, Msg: m, Text: v.Reason})
			continue
		}
		allowed++
		out = append(out, markChecked(checked))
	}
	return out, allowed, blocked, nil
}

// withHookContext returns msgs as the provider sees them: each user
// message's UserPromptSubmit context appended to its content. The history
// keeps the context in metadata, so the chat and the saved session show the
// prompt as typed. msgs is never modified.
func withHookContext(msgs []Message) []Message {
	var out []Message
	for i, m := range msgs {
		c, _ := m.Metadata[tui.MetaHookContext].(string)
		if m.Role != "user" || c == "" {
			if out != nil {
				out = append(out, m)
			}
			continue
		}
		if out == nil {
			out = append(make([]Message, 0, len(msgs)), msgs[:i]...)
		}
		m.Content += "\n\n<hook-context>\n" + c + "\n</hook-context>"
		out = append(out, m)
	}
	if out == nil {
		return msgs
	}
	return out
}
