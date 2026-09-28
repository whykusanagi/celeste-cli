package main

import (
	"context"
	"fmt"
	"os"
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
	if err == nil {
		return ""
	}
	return fmt.Sprintf("hooks disabled: %v", err)
}

// loadChatHooks loads the session's hooks, wires the tool hooks into
// registry and runs SessionStart. It returns the runner (nil if loading
// failed), SessionStart's additionalContext, and a warning to show in the
// chat when loading failed. Warnings print to stderr while loading, because
// the TUI hasn't started; after that they go to the log and, via hookNotify,
// to the chat.
func loadChatHooks(cwd, homeDir, sessionID string, resumed bool, registry *tools.Registry) (*hooks.Runner, string, string) {
	var loading atomic.Bool
	loading.Store(true)
	warn := func(s string) {
		tui.LogInfo(s)
		if loading.Load() {
			fmt.Fprintln(os.Stderr, s)
			return
		}
		if f := hookNotify.Load(); f != nil {
			(*f)(s)
		}
	}
	runner, err := hooks.Load(hooks.Options{
		Workspace: cwd, Home: homeDir, SessionID: sessionID,
		Approve: chatHookApprover, Warn: warn,
	})
	if err != nil {
		warning := formatHookLoadWarning(err)
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warning)
		return nil, "", warning
	}
	if th := runner.ToolHooks(); th != nil {
		registry.SetHookRunner(th)
	}
	source := "startup"
	if resumed {
		source = "resume"
	}
	start := runner.SessionStart(context.Background(), source)
	loading.Store(false)
	return runner, start.AdditionalContext, ""
}

// applyPromptHooks runs UserPromptSubmit for the newest user message the
// first time it is sent, and returns the copy of messages to send.
//   - Blocked: it sends tui.PromptBlockedMsg (the TUI removes the prompt
//     from the chat and session) and returns stop=true.
//   - Allowed: it sends tui.PromptHookMsg first, so the TUI records the
//     result on the message; the context rides on that prompt in every
//     request from then on.
func (a *TUIClientAdapter) applyPromptHooks(ctx context.Context, messages []tui.ChatMessage, ch chan tea.Msg) ([]tui.ChatMessage, bool) {
	sent := append([]tui.ChatMessage(nil), messages...)
	if n := len(sent); n > 0 && sent[n-1].Role == "user" && a.hooks.Has(hooks.EventUserPromptSubmit) {
		if done, _ := sent[n-1].Metadata[tui.MetaPromptHookDone].(bool); !done {
			out := a.hooks.UserPromptSubmit(ctx, sent[n-1].Content)
			if out.Decision != hooks.Allow {
				reason := out.Reason
				if reason == "" {
					reason = "no reason given"
				}
				ch <- tui.PromptBlockedMsg{Reason: reason}
				return nil, true
			}
			ch <- tui.PromptHookMsg{Context: out.AdditionalContext, Next: readStreamCh(ch)}
			meta := map[string]any{tui.MetaPromptHookDone: true}
			for k, v := range sent[n-1].Metadata {
				meta[k] = v
			}
			if out.AdditionalContext != "" {
				meta[tui.MetaHookContext] = out.AdditionalContext
			}
			sent[n-1].Metadata = meta
		}
	}
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
