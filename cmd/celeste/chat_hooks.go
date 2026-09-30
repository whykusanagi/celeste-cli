package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// chatHookApprover asks the person to trust repo hooks before the TUI
// starts; newChatApp passes it to loop.Setup. nil lets Setup prompt on the
// terminal when stdin and stderr are both terminals; otherwise (tests, piped
// consoles) untrusted hooks are skipped with a warning.
var chatHookApprover hooks.ApproveFunc

// hookNotify shows warnings in the chat once the Bubble Tea program exists; runChatTUI sets it.
var hookNotify atomic.Pointer[func(string)]

// chatWarnSink routes loop.Setup's warnings for the chat. While newChatApp
// runs they are collected, and printed to stderr, so the chat can show them
// once it starts (the alt screen hides stderr). After done, warnings go to
// the log and, through hookNotify, to the chat.
type chatWarnSink struct {
	mu       sync.Mutex
	loading  bool
	warnings []string
}

func newChatWarnSink() *chatWarnSink { return &chatWarnSink{loading: true} }

func (s *chatWarnSink) warn(msg string) {
	tui.LogInfo(msg)
	s.mu.Lock()
	if s.loading {
		s.warnings = append(s.warnings, msg)
		s.mu.Unlock()
		fmt.Fprintln(os.Stderr, msg)
		return
	}
	s.mu.Unlock()
	if f := hookNotify.Load(); f != nil {
		(*f)(msg)
	}
}

// done ends loading and returns the warnings collected so far.
func (s *chatWarnSink) done() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loading = false
	return s.warnings
}

// applyPromptHooks runs UserPromptSubmit, in order, for every user message
// not yet checked (MetaPromptHookDone unset) and returns the copy of
// messages to send. More than one can be pending: queued steers join a turn
// as separate messages, and a prompt whose hook an interrupt cut short is
// kept unchecked. Hidden messages are the app's own directives and
// summaries, not prompts, and are not checked.
//   - Allowed: it sends tui.PromptHookMsg, so the TUI records the result on
//     that message; the context rides on it in every request from then on.
//   - Blocked: the message leaves this request, and tui.PromptBlockedMsg
//     tells the TUI to remove it from the chat and session. If no new
//     prompt was allowed this round, the send stops (stop=true) and the
//     last PromptBlockedMsg has a nil Next, whatever the history ends with
//     (after Esc interrupts a tool loop it ends with tool results, and
//     sending would resume that turn). Otherwise the request goes out
//     without the blocked ones.
//   - Interrupted (cancelled context): it sends PromptBlockedMsg{Cancelled}
//     and stops; the unchecked prompts are kept and checked on the next send.
func (a *TUIClientAdapter) applyPromptHooks(ctx context.Context, messages []tui.ChatMessage, ch chan tea.Msg) ([]tui.ChatMessage, bool) {
	sent := make([]tui.ChatMessage, 0, len(messages))
	var pending *tui.PromptBlockedMsg // the newest block, sent once we know whether to stop
	flush := func(next tea.Cmd) {
		if pending != nil {
			pending.Next = next
			ch <- *pending
			pending = nil
		}
	}
	blocked, allowed := false, 0
	for _, msg := range messages {
		if msg.Role != "user" || !a.hooks.Has(hooks.EventUserPromptSubmit) {
			sent = append(sent, msg)
			continue
		}
		done, _ := msg.Metadata[tui.MetaPromptHookDone].(bool)
		hidden, _ := msg.Metadata["hidden"].(bool)
		if done || hidden {
			sent = append(sent, msg)
			continue
		}
		out := a.hooks.UserPromptSubmit(ctx, msg.Content)
		if ctx.Err() != nil {
			// An interrupt (Esc/Ctrl+C), not a verdict: the TUI keeps the
			// prompt and it is checked again on the next send.
			flush(readStreamCh(ch))
			ch <- tui.PromptBlockedMsg{Cancelled: true, Content: msg.Content, Timestamp: msg.Timestamp}
			return nil, true
		}
		if out.Decision != hooks.Allow {
			reason := out.Reason
			if reason == "" {
				reason = "no reason given"
			}
			flush(readStreamCh(ch))
			pending = &tui.PromptBlockedMsg{Reason: reason, Content: msg.Content, Timestamp: msg.Timestamp}
			blocked = true
			continue
		}
		ch <- tui.PromptHookMsg{Context: out.AdditionalContext, Content: msg.Content, Timestamp: msg.Timestamp, Next: readStreamCh(ch)}
		meta := map[string]any{tui.MetaPromptHookDone: true}
		for k, v := range msg.Metadata {
			meta[k] = v
		}
		if out.AdditionalContext != "" {
			meta[tui.MetaHookContext] = out.AdditionalContext
		}
		msg.Metadata = meta
		sent = append(sent, msg)
		allowed++
	}
	if blocked && allowed == 0 {
		flush(nil)
		return nil, true
	}
	flush(readStreamCh(ch))
	for i := range sent {
		if sent[i].Role != "user" {
			continue
		}
		if c, _ := sent[i].Metadata[tui.MetaHookContext].(string); c != "" {
			sent[i].Content += "\n\n<hook-context>\n" + c + "\n</hook-context>"
		}
	}
	return sent, false
}

// checkPrompt is the chat's loop.PromptCheck: UserPromptSubmit for every new
// prompt, and for each steer when it joins (2.0 F2d; replaces
// applyPromptHooks). Hook context rides in metadata; the loop appends it to
// the copy it sends.
func (a *TUIClientAdapter) checkPrompt(ctx context.Context, msg tui.ChatMessage) (tui.ChatMessage, loop.PromptVerdict, error) {
	if !a.hooks.Has(hooks.EventUserPromptSubmit) { // Has is nil-safe
		return msg, loop.PromptVerdict{}, nil
	}
	out := a.hooks.UserPromptSubmit(ctx, msg.Content)
	if err := ctx.Err(); err != nil {
		return msg, loop.PromptVerdict{}, err // an interrupt, not a verdict
	}
	if out.Decision != hooks.Allow {
		reason := out.Reason
		if reason == "" {
			reason = "no reason given"
		}
		return msg, loop.PromptVerdict{Blocked: true, Reason: reason}, nil
	}
	if out.AdditionalContext != "" {
		meta := make(map[string]any, len(msg.Metadata)+1)
		for k, v := range msg.Metadata {
			meta[k] = v
		}
		meta[tui.MetaHookContext] = out.AdditionalContext
		msg.Metadata = meta
	}
	return msg, loop.PromptVerdict{}, nil
}

// lifeContext is cancelled when the chat app shuts down.
func (a *TUIClientAdapter) lifeContext() context.Context {
	if a.lifeCtx != nil {
		return a.lifeCtx
	}
	return context.Background()
}
