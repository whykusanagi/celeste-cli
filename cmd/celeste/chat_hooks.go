package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// chatHookApprover asks the person at the terminal to trust repo hooks
// before the TUI starts. nil (tests, piped stdin or stderr) is
// non-interactive: untrusted hooks are skipped with a warning.
var chatHookApprover hooks.ApproveFunc

// hookNotify shows hook warnings in the chat once the Bubble Tea program
// exists; runChatTUI sets it. F2 moves this into Setup's Env.
var hookNotify atomic.Pointer[func(string)]

func formatHookLoadWarning(err error) string {
	return hooks.DisabledWarning(err)
}

// loadChatHooks loads the session's hooks, wires the tool hooks into
// registry and runs SessionStart. It returns the runner (nil if loading
// failed), SessionStart's additionalContext, and the warnings raised while
// loading (including a Load error, which disables hooks). The TUI hasn't
// started yet, so the caller shows those warnings in the chat; they also go
// to stderr and the log. Warnings after loading go to the log and, via
// hookNotify, to the chat.
func loadChatHooks(cwd, homeDir, sessionID string, resumed bool, registry *tools.Registry) (*hooks.Runner, string, []string) {
	var (
		mu       sync.Mutex
		loading  = true
		warnings []string
	)
	warn := func(s string) {
		tui.LogInfo(s)
		mu.Lock()
		if loading {
			warnings = append(warnings, s)
			mu.Unlock()
			fmt.Fprintln(os.Stderr, s)
			return
		}
		mu.Unlock()
		if f := hookNotify.Load(); f != nil {
			(*f)(s)
		}
	}
	done := func() []string {
		mu.Lock()
		defer mu.Unlock()
		loading = false
		return warnings
	}
	runner, err := hooks.Load(hooks.Options{
		Workspace: cwd, Home: homeDir, SessionID: sessionID,
		Approve: chatHookApprover, Warn: warn,
	})
	if err != nil {
		warn(formatHookLoadWarning(err))
		return nil, "", done()
	}
	if th := runner.ToolHooks(); th != nil {
		registry.SetHookRunner(th)
	}
	source := "startup"
	if resumed {
		source = "resume"
	}
	start := runner.SessionStart(context.Background(), source)
	return runner, start.AdditionalContext, done()
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

// lifeContext is cancelled when the chat app shuts down.
func (a *TUIClientAdapter) lifeContext() context.Context {
	if a.lifeCtx != nil {
		return a.lifeCtx
	}
	return context.Background()
}
