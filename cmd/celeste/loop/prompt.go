package loop

import (
	"context"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// UserPromptSubmit runs h's UserPromptSubmit hooks on msg. The chat, MCP
// chat and agent goals share it (2.0 F2e). A deny, or a hook that failed,
// blocks msg with the hook's reason ("no reason given" when it has none).
// Additional context rides in msg.Metadata[tui.MetaHookContext]; the loop
// appends it to the copy it sends (HookContextBlock). A hook cut short by
// ctx ending returns ctx's error: an interrupt, not a verdict. With no
// UserPromptSubmit hooks (h may be nil) msg is allowed unchanged.
func UserPromptSubmit(ctx context.Context, h *hooks.Runner, msg Message) (Message, PromptVerdict, error) {
	if !h.Has(hooks.EventUserPromptSubmit) { // Has is nil-safe
		return msg, PromptVerdict{}, nil
	}
	out := h.UserPromptSubmit(ctx, msg.Content)
	if err := ctx.Err(); err != nil {
		return msg, PromptVerdict{}, err
	}
	if out.Decision != hooks.Allow {
		reason := out.Reason
		if reason == "" {
			reason = "no reason given"
		}
		return msg, PromptVerdict{Blocked: true, Reason: reason}, nil
	}
	if out.AdditionalContext != "" {
		meta := make(map[string]any, len(msg.Metadata)+1)
		for k, v := range msg.Metadata {
			meta[k] = v
		}
		meta[tui.MetaHookContext] = out.AdditionalContext
		msg.Metadata = meta
	}
	return msg, PromptVerdict{}, nil
}

// HookPromptCheck is h's UserPromptSubmit as a Loop.CheckPrompt, or nil when
// h has no UserPromptSubmit hooks, so such a run takes the unchecked path.
func HookPromptCheck(h *hooks.Runner) PromptCheck {
	if !h.Has(hooks.EventUserPromptSubmit) {
		return nil
	}
	return func(ctx context.Context, msg Message) (Message, PromptVerdict, error) {
		return UserPromptSubmit(ctx, h, msg)
	}
}

// HookContextBlock is how UserPromptSubmit context joins the prompt the
// model receives: after the prompt, in a <hook-context> block.
func HookContextBlock(context string) string {
	return "\n\n<hook-context>\n" + context + "\n</hook-context>"
}

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
		m.Content += HookContextBlock(c)
		out = append(out, m)
	}
	if out == nil {
		return msgs
	}
	return out
}
