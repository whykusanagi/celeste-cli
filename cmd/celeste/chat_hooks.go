package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"

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

// checkPrompt is the chat's loop.PromptCheck: UserPromptSubmit for every new
// prompt, and for each steer when it joins (2.0 F2d). It reads a.hooks at
// call time. Hook context rides in the message's metadata
// (tui.MetaHookContext); the loop appends it to the copy it sends.
func (a *TUIClientAdapter) checkPrompt(ctx context.Context, msg tui.ChatMessage) (tui.ChatMessage, loop.PromptVerdict, error) {
	return loop.UserPromptSubmit(ctx, a.hooks, msg)
}

// lifeContext is cancelled when the chat app shuts down.
func (a *TUIClientAdapter) lifeContext() context.Context {
	if a.lifeCtx != nil {
		return a.lifeCtx
	}
	return context.Background()
}
