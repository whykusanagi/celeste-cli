package tui

import tea "github.com/charmbracelet/bubbletea"

// The chat turn protocol (2.0 F2d). A turn is one loop.Loop run, started by
// TurnRunner.RunTurn off the Update goroutine. Its events arrive as
// TurnEventMsg, one at a time; each carries the command that reads the
// next, until TurnDoneMsg.

// TurnRequest starts one turn. Its fields are fixed for the whole turn: a
// tool that switches NSFW mode changes Tools from the next turn on.
type TurnRequest struct {
	History []ChatMessage // the chat's LLM messages
	Tools   bool          // offer tools (off in NSFW mode and without function calling)
	Window  int           // context window in tokens; 0 turns compaction off
	Used    int           // tokens the last request used, from the tracker
	Run     uint64        // tags every TurnEventMsg of this turn
}

// TurnHandle controls a running turn. All three methods are safe from the
// Update goroutine at any time, including after the run has ended.
type TurnHandle interface {
	Steer(text string)  // joins at the next tool boundary (loop.Loop.Steer)
	Cancel()            // interrupt: cancels the run's context
	Leftover() []string // takes the steers the loop has not joined (loop.Loop.TakeSteers)
}

// TurnRunner runs chat turns. RunTurn must not block: the returned command
// starts the run and delivers its first event.
type TurnRunner interface {
	RunTurn(req TurnRequest) (TurnHandle, tea.Cmd)
}

// TurnEventMsg is one message of a running turn. Next reads the one after
// it; it is nil after TurnDoneMsg.
type TurnEventMsg struct {
	Run  uint64
	Msg  tea.Msg
	Next tea.Cmd
}

// TurnStartMsg: a request is about to go out.
type TurnStartMsg struct{ Turn int }

// ToolTurnMsg: the model's reply asked for tools. Its text, if any, has
// already arrived as StreamChunkMsg.
type ToolTurnMsg struct {
	Text  string
	Usage *TokenUsage
}

// ToolStartMsg and ToolResultMsg bracket one tool call.
type ToolStartMsg struct {
	ID, Name string
	Args     map[string]any
}

type ToolResultMsg struct {
	ID, Name, Content string // Content is what the model receives
	IsError           bool
	Metadata          map[string]any // spawn_agent's subagent name and element; images
}

// HistoryMsg is a consistent snapshot of the loop's history: after the new
// prompts were checked, after the assistant's tool calls are recorded, and
// at the end of every model turn. Empty text-only replies are left out.
type HistoryMsg struct{ History []ChatMessage }

// SteeredMsg: a steer joined the conversation, as recorded.
type SteeredMsg struct{ Message ChatMessage }

// CompactedMsg: old tool results were pruned before a request.
type CompactedMsg struct{ Line string }

// StopContinueMsg: a Stop hook asked the chat to continue; Message is the
// hidden instruction that joins the history.
type StopContinueMsg struct {
	Message ChatMessage
	Reason  string
}

// TurnDoneMsg ends the turn.
type TurnDoneMsg struct {
	Stop      string   // the loop's StopReason: done, cap, identical, progress, invalid_args, interrupted, error, blocked
	Err       error    // set for "error"
	Notice    string   // shown as a system line (caps and guards)
	Leftover  []string // steers the loop never joined; sent next, with TurnHandle.Leftover
	Summarize bool     // pruning was not enough: write a summary now
}
