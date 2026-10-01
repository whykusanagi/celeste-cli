package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/commands"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

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

// CompactedMsg: old tool results were pruned before a request. Saved is
// the tokens the prune freed.
type CompactedMsg struct {
	Line  string
	Saved int
}

// StopContinueMsg: a Stop hook asked the chat to continue; Message is the
// hidden instruction that joins the history.
type StopContinueMsg struct {
	Message ChatMessage
	Reason  string
}

// StopHookStartMsg: the reply is in and the Stop hook is deciding whether
// the turn may end. The turn is still running until TurnDoneMsg.
type StopHookStartMsg struct{}

// RuleInterruptMsg: steering cut the reply short (2.0 W3). The partial
// reply is dropped; the turn re-runs with a reminder.
type RuleInterruptMsg struct{}

// RuleReminderMsg: a steering reminder joined the history as a hidden
// message (2.0 W3). Source is "rule:<name>" or "watchdog".
type RuleReminderMsg struct {
	Source  string
	Message ChatMessage
}

// TurnDoneMsg ends the turn.
type TurnDoneMsg struct {
	Stop      string   // the loop's StopReason: done, cap, identical, progress, invalid_args, interrupted, error, blocked
	Err       error    // set for "error"
	Notice    string   // shown as a system line (caps and guards)
	Leftover  []string // steers the loop never joined; sent next, with TurnHandle.Leftover
	Summarize bool     // pruning was not enough: write a summary now
}

// startTurn sends the chat's LLM history to the loop as one turn (2.0 F2d).
// The loop checks new prompts with UserPromptSubmit, prunes before every
// request and retries a context overflow; the chat renders its events.
// Whether tools are offered is fixed here for the whole turn.
func (m AppModel) startTurn() (AppModel, tea.Cmd) {
	if m.llmClient == nil {
		return m, nil
	}
	// Every turn start adds the prompt (SendMessageMsg, /plan) or a fresh
	// bubble before any keepLive sync; a stale empty reply must not be the
	// chat's last message either.
	m.chat = m.chat.DropEmptyLastReply()
	m.turnSeq++
	req := TurnRequest{History: m.chat.GetLLMMessages(), Tools: m.toolsOffered(), Run: m.turnSeq}
	if m.contextTracker != nil && m.contextTracker.MaxTokens > 0 {
		req.Window, req.Used = m.contextTracker.MaxTokens, m.contextTracker.CurrentTokens
	}
	h, cmd := m.llmClient.RunTurn(req)
	m.turn, m.turnRun, m.loopSteers = h, m.turnSeq, 0
	m.streaming = true
	m.streamStart = time.Now()
	m.lastMsgInTok, m.lastMsgOutTok = 0, 0
	m.status = m.status.SetStreaming(true)
	m.status = m.status.SetText(StreamingSpinner(0) + " " + ThinkingAnimation(0))
	return m, cmd
}

// toolsOffered: no tools in NSFW mode or with a provider without function
// calling.
func (m AppModel) toolsOffered() bool { return !m.nsfwMode && m.skillsEnabled }

// tick schedules the tick chain's next TickMsg after d, unless one is
// already pending: at most one chain runs, so the spinner and the typing
// keep their speed however many tool turns start one (2.0 F2e). A
// TickMsg without the chain's generation (a test's) never clears the
// pending tick.
func (m *AppModel) tick(d time.Duration) tea.Cmd {
	if m.tickPending {
		return nil
	}
	m.tickPending = true
	m.tickGen++
	gen := m.tickGen
	return tea.Tick(d, func(t time.Time) tea.Msg { return TickMsg{Time: t, gen: gen} })
}

// restartTick starts a new tick chain, superseding a pending one: its tick
// is dropped when it arrives (update). Enter, /agent and /orchestrate call
// it, so a chain whose tick never came back cannot freeze the spinner for
// the next run.
func (m *AppModel) restartTick(d time.Duration) tea.Cmd {
	m.tickPending = false
	return m.tick(d)
}

// onTurnEvent renders one event of the running turn and reads the next. An
// event of a turn that ended or was replaced is dropped and its chain stops;
// the mailbox never blocks the run, so nothing waits on it.
func (m AppModel) onTurnEvent(ev TurnEventMsg) (tea.Model, tea.Cmd) {
	if m.turn == nil || ev.Run != m.turnRun {
		return m, nil
	}
	var cmds []tea.Cmd
	switch msg := ev.Msg.(type) {
	case TurnStartMsg:
		m = m.finishTyping()
		m.toolProgress.ClearCompleted()
		if !m.interrupted {
			m.streaming = true
			m.streamStart = time.Now()
			m.lastMsgInTok, m.lastMsgOutTok = 0, 0
			m.status = m.status.SetStreaming(true)
			if !m.planning {
				m.status = m.status.SetText(StreamingSpinner(0) + " " + ThinkingAnimation(0))
			}
			cmds = append(cmds, m.tick(typingTickInterval*2))
		}
	case StreamChunkMsg:
		var more []tea.Cmd
		m, more = m.onStreamChunk(msg)
		cmds = append(cmds, more...)
	case StreamDoneMsg:
		var more []tea.Cmd
		m, more = m.onStreamDone(msg)
		cmds = append(cmds, more...)
	case ToolTurnMsg:
		m.planning = false
		if !m.interrupted {
			m = m.recordUsage(msg.Usage, "")
			m = m.finishTyping()
			m.streaming = false
			m.status = m.status.SetStreaming(false)
		}
	case HistoryMsg:
		m.chat = m.chat.SyncLLM(msg.History, m.typingContent != "")
		if m.typingContent == "" {
			// A reply still being typed shows its typed prefix and glitch
			// glyphs; the typing commit saves the session instead.
			m.persistSession()
		}
	case ToolStartMsg:
		var cmd tea.Cmd
		m, cmd = m.onToolStart(msg)
		cmds = append(cmds, cmd)
	case ToolResultMsg:
		m = m.onToolResult(msg)
	case SteeredMsg:
		if m.loopSteers > 0 {
			m.loopSteers--
		}
		m.chat = m.chat.AppendLLM(msg.Message)
	case PromptBlockedMsg:
		if msg.Steer && m.loopSteers > 0 {
			m.loopSteers--
		}
		m = m.onPromptBlocked(msg)
	case CompactedMsg:
		if m.contextTracker != nil {
			m.contextTracker.CompactionCount++
			// As /context compact does: the next request's count replaces
			// this, but a turn that ends first must not keep the old one.
			if m.contextTracker.CurrentTokens > msg.Saved {
				m.contextTracker.CurrentTokens -= msg.Saved
			}
			m.header = m.header.SetContextUsage(m.contextTracker.CurrentTokens, m.contextTracker.MaxTokens)
		}
		m.chat = m.chat.AddSystemMessage("🗜 Context compacted: " + msg.Line)
		LogInfo("context compacted: " + msg.Line)
	case StopContinueMsg:
		// The reply the hook saw is shown in full before the continuation
		// joins after it, so it is never an empty bubble in the middle of
		// the history when the next run's snapshots arrive.
		m = m.finishTyping()
		if m.stopHookRunning && !m.interrupted {
			// The continuation's run is starting: the spinner, not the
			// Stop hook status, until its first TurnStartMsg.
			m.status = m.status.SetText(StreamingSpinner(0) + " " + ThinkingAnimation(0))
		}
		m.stopHookRunning, m.heldReady = false, ""
		m.chat = m.chat.AddSystemMessage("↻ A Stop hook asked to continue: " + msg.Reason)
		m.chat = m.chat.AppendLLM(msg.Message)
	case StopHookStartMsg:
		// Input typed now waits for the turn; say why instead of "Ready".
		// A reply still typing shows this when it commits (TickMsg).
		m.stopHookRunning = true
		if m.typingContent == "" {
			m.heldReady = m.status.text
			m.status = m.status.SetText(stopHookStatus)
		}
	case HookWarningMsg:
		m.chat = m.chat.AddSystemMessage("⚠ " + msg.Text)
	case RuleInterruptMsg:
		if !m.interrupted {
			if m.typingContent != "" {
				// The streamed part of the reply is the live bubble: drop it.
				m.typingContent, m.typingPos, m.streamDone = "", 0, false
				m.chat = m.chat.SetTypingActive(false)
				m.chat = m.chat.SetLastAssistantContent("")
				m.chat = m.chat.DropEmptyLastReply()
			}
			m.chat = m.chat.AddSystemMessage("↺ A stream rule stopped the reply; retrying with a reminder.")
		}
	case RuleReminderMsg:
		// Hidden, at the loop's position, so later snapshots line up.
		m.chat = m.chat.AppendLLM(msg.Message)
		LogInfo("steering reminder: " + msg.Source)
	case TurnDoneMsg:
		return m.onTurnDone(msg)
	}
	return m, tea.Batch(append(cmds, ev.Next)...)
}

// finishTyping shows a reply still being typed out in full: tools start or
// another model turn begins.
func (m AppModel) finishTyping() AppModel {
	if m.typingContent == "" {
		return m
	}
	m.chat = m.chat.SetTypingActive(false)
	m.chat = m.chat.SetLastAssistantContent(m.typingContent)
	m.typingContent = ""
	m.typingPos = 0
	m.streamDone = false
	return m
}

func (m AppModel) onToolStart(msg ToolStartMsg) (AppModel, tea.Cmd) {
	LogSkillCall(msg.Name, msg.Args)
	m = m.finishTyping()
	m.chat = m.chat.AddFunctionCall(FunctionCall{ID: msg.ID, Name: msg.Name, Arguments: msg.Args, Status: "executing", Timestamp: time.Now()})
	m.skills = m.skills.SetExecuting(msg.Name)
	m.toolProgress, _ = m.toolProgress.Update(ToolProgressMsg{ToolCallID: msg.ID, ToolName: msg.Name, State: "executing"})
	m.status = m.status.SetText(fmt.Sprintf("⚡ Executing: %s", msg.Name))
	tick := m.tick(typingTickInterval * 2) // spinners; update()'s tail keeps it going
	return m, tick
}

// stopHookStatus is the status while the Stop hook runs.
const stopHookStatus = "Running Stop hook…"

// toolErrorText is the message of a failed call's result: the loop's JSON
// envelope ({"error": true, "message": ...}) or, failing that, the content.
func toolErrorText(content string) string {
	var env struct {
		Error   bool   `json:"error"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(content), &env) == nil && env.Error && env.Message != "" {
		return env.Message
	}
	return content
}

func (m AppModel) onToolResult(msg ToolResultMsg) AppModel {
	var err error
	card := msg.Content
	if msg.IsError {
		// The card says "Error: <message>", as before the loop; the model
		// gets the envelope through the loop's history.
		err = errors.New(toolErrorText(msg.Content))
		card = "Error: " + err.Error()
	}
	LogSkillResult(msg.Name, msg.Content, err)
	prog := ToolProgressMsg{ToolCallID: msg.ID, ToolName: msg.Name, State: "done"}
	if msg.IsError {
		prog.State = "failed"
	}
	if msg.Name == "spawn_agent" && msg.Metadata != nil {
		if name, ok := msg.Metadata["subagent_name"].(string); ok {
			prog.DisplayName = "〔" + name + "〕"
		}
		if elem, ok := msg.Metadata["element"].(string); ok {
			prog.Element = elem
		}
	}
	m.toolProgress, _ = m.toolProgress.Update(prog)
	m.chat = m.chat.UpdateFunctionResult(msg.ID, msg.Name, card)
	if msg.IsError {
		m.skills = m.skills.SetError(msg.Name, err)
		return m
	}
	m.skills = m.skills.SetCompleted(msg.Name)
	if msg.Name == "nsfw_mode" {
		// Takes effect from the next turn: TurnRequest.Tools is fixed.
		switch {
		case strings.Contains(msg.Content, "enabled"):
			m.nsfwMode = true
			m.header = m.header.SetNSFWMode(true)
			m.persistSession()
		case strings.Contains(msg.Content, "disabled"):
			m.nsfwMode = false
			m.header = m.header.SetNSFWMode(false)
			m.persistSession()
		}
	}
	return m
}

// onPromptBlocked reports a prompt or steer a UserPromptSubmit hook blocked
// and removes the prompt from the chat and, through persistSession (which
// rewrites the session from the chat), from the session. A hook cut short by
// an interrupt (Cancelled) is not a block: the prompt stays, unchecked.
func (m AppModel) onPromptBlocked(msg PromptBlockedMsg) AppModel {
	if msg.Cancelled {
		return m
	}
	if !msg.Steer {
		m.chat = m.chat.DropUser(msg.Content, msg.Timestamp)
		m.persistSession()
	}
	m.chat = m.chat.AddSystemMessage("Prompt blocked by a UserPromptSubmit hook: " + msg.Reason)
	return m
}

// onTurnDone ends the turn. Steers the loop never joined go to the front of
// the queue and are sent next (dispatchQueued), once the reply is typed out:
// those the run handed back, then any typed after it took them. A summary
// held during the turn is applied now if it still fits.
func (m AppModel) onTurnDone(msg TurnDoneMsg) (AppModel, tea.Cmd) {
	leftover := msg.Leftover
	if m.turn != nil {
		// An Enter between the run taking its leftovers and this message
		// is still in the loop's queue.
		leftover = append(leftover, m.turn.Leftover()...)
	}
	m.turn = nil
	m.loopSteers = 0
	m.planning = false
	// A modal this turn opened would outlive it: answer it and close it.
	m = m.closeModalsOf(RunOwner{Kind: OwnerTurn, Run: m.turnRun})
	if m.stopHookRunning {
		m.stopHookRunning = false
		if m.heldReady != "" {
			m.status = m.status.SetText(m.heldReady)
		}
		m.heldReady = ""
	}
	if msg.Stop != "interrupted" && !m.interrupted {
		// Kept after an interrupt, so a second Ctrl+C within 3s still quits,
		// also when the turn finished before the cancel reached it.
		m.interruptPending = false
	}
	if len(leftover) > 0 {
		m.steerQueue = append(append([]string(nil), leftover...), m.steerQueue...)
	}
	if m.typingContent != "" {
		m.streamDone = true // no more text is coming: let the typing finish and commit
	} else {
		m.streaming = false
		m.status = m.status.SetStreaming(false)
	}
	switch {
	case msg.Notice != "":
		m.chat = m.chat.AddSystemMessage(msg.Notice)
		m.status = m.status.SetText("Stopped")
	case msg.Stop == "error" && msg.Err != nil:
		m.status = m.status.SetText(fmt.Sprintf("Error: %v", msg.Err))
		m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Error: %v", msg.Err))
	case msg.Stop == "interrupted":
		m.status = m.status.SetText("Interrupted")
	case msg.Stop == "blocked":
		m.status = m.status.SetText("Prompt blocked by a hook")
	}
	switch {
	case m.interrupted && m.interruptPending:
		// Ctrl+C ended this turn: keep telling the user a second one quits.
		m.status = m.status.SetText("Cancelled. Press Ctrl+C again to exit")
	case m.interrupted:
		// Esc landed after the reply (the loop ended "done"), or during the
		// Stop hook: the turn still ends interrupted, not "Interrupting…".
		m.status = m.status.SetText("Interrupted")
	}
	applied := false
	if m.heldSummary != nil {
		held := *m.heldSummary
		m.heldSummary = nil
		m, applied = m.applySummary(held)
	}
	var cmd tea.Cmd
	if msg.Summarize && !applied {
		// A held summary that no longer fit was discarded (summarizing is
		// off again); this one is written from the history as it is now.
		m, cmd = m.startSummary("", false)
	}
	if m.typingContent == "" {
		m.persistSession() // otherwise the typing commit saves it
	}
	return m, cmd
}

// onStreamChunk types out streamed text. A first chunk starts the assistant
// bubble and the typing animation (see streamDone on AppModel).
func (m AppModel) onStreamChunk(msg StreamChunkMsg) (AppModel, []tea.Cmd) {
	if m.interrupted {
		return m, nil // late output from a turn Esc cancelled
	}
	if !msg.Chunk.IsFirst {
		m.typingContent += msg.Chunk.Content // the running ticker picks it up
		return m, nil
	}
	m.planning = false
	m.chat = m.chat.AddAssistantMessage("")
	m.chat = m.chat.SetTypingActive(true) // skip Glamour for the corruption buffer
	m.typingContent = msg.Chunk.Content
	m.typingPos = 0
	m.streaming = true
	m.streamDone = false
	m.status = m.status.SetStreaming(true)
	m.status = m.status.SetText(StreamingSpinner(m.animFrame) + " " + ThinkingAnimation(m.animFrame))
	tick := m.tick(typingTickInterval)
	return m, []tea.Cmd{tick}
}

// onStreamDone ends a reply without tool calls: the typing animation may now
// commit it once it catches up. An empty reply is a system line, never an
// empty assistant message.
func (m AppModel) onStreamDone(msg StreamDoneMsg) (AppModel, []tea.Cmd) {
	if m.interrupted {
		return m, nil // the request Esc cancelled finished anyway; its reply is dropped
	}
	m.interruptPending = false
	m.planning = false
	m.streamDone = true
	m = m.recordUsage(msg.Usage, msg.FullContent)
	var cmds []tea.Cmd
	if msg.FullContent != "" {
		if commands.IsContentPolicyRefusal(msg.FullContent) && m.endpoint != "venice" {
			m.chat = m.chat.AddSystemMessage(
				"⚠️  Content policy refusal detected.\n\n" +
					"💡 Tip: Use /nsfw to switch to Venice.ai for uncensored responses,\n" +
					"or add 'nsfw' at the end of your message for auto-routing.",
			)
		}
		if m.typingContent != "" {
			// Streaming was active: make sure the whole reply is in the buffer.
			m.typingContent = msg.FullContent
		} else {
			// No chunks arrived: simulate typing on the full reply.
			m.typingContent = msg.FullContent
			m.typingPos = 0
			m.streaming = true
			m.status = m.status.SetStreaming(true)
			m.chat = m.chat.AddAssistantMessage("")
			m.chat = m.chat.SetTypingActive(true)
			m.status = m.status.SetText("Typing...")
			cmds = append(cmds, m.tick(typingTickInterval))
		}
	} else if m.typingContent == "" {
		// An empty reply: tell the user to re-prompt.
		m.streaming = false
		m.status = m.status.SetStreaming(false)
		m.status = m.status.SetText("Ready (empty response)")
		m.chat = m.chat.AddSystemMessage("(No response — try rephrasing or say 'go' to execute)")
	}
	return m, cmds
}

// recordUsage updates the token counts and the context bar from a reply's
// usage, or estimates them from content when the API sent none.
func (m AppModel) recordUsage(u *TokenUsage, content string) AppModel {
	if u != nil && (u.PromptTokens > 0 || u.CompletionTokens > 0) {
		m.lastMsgInTok = u.PromptTokens
		m.lastMsgOutTok = u.CompletionTokens
		if m.contextTracker != nil {
			m.contextTracker.UpdateTokens(
				u.PromptTokens,
				u.CompletionTokens,
				u.TotalTokens,
			)
			m.header = m.header.SetContextUsage(m.contextTracker.CurrentTokens, m.contextTracker.MaxTokens)

			// Update context bar
			budgetMsg := ContextBudgetMsg{
				UsedTokens:   m.contextTracker.CurrentTokens,
				MaxTokens:    m.contextTracker.MaxTokens,
				UsagePercent: float64(m.contextTracker.CurrentTokens) / float64(m.contextTracker.MaxTokens) * 100,
			}
			if m.contextTracker.Budget != nil {
				budgetMsg.CompactCount = m.contextTracker.Budget.CompactCount
				budgetMsg.TurnCount = m.contextTracker.Budget.TurnCount
			}
			m.contextBar, _ = m.contextBar.Update(budgetMsg)
		}
	} else if content != "" {
		// API didn't return token usage — estimate from response length and
		// update the context tracker so the header counter keeps moving.
		estOut := config.EstimateTokens(content)
		if m.contextTracker != nil && estOut > 0 {
			cur := m.contextTracker.CurrentTokens + estOut
			m.contextTracker.UpdateTokens(0, estOut, cur)
			m.header = m.header.SetContextUsage(m.contextTracker.CurrentTokens, m.contextTracker.MaxTokens)
		}
		// Leave lastMsgInTok/lastMsgOutTok at 0 so the TickMsg inferred path runs.
	}
	return m
}

// Run owners: the run a permission or ask request came from (2.0 F2e).
const (
	OwnerTurn  = "turn"
	OwnerOrch  = "orch"
	OwnerAgent = "agent"
)

// RunOwner names one chat turn, /orch run or /agent run.
type RunOwner struct {
	Kind string // "" = no run: the modal stays until answered
	Run  uint64 // TurnRequest.Run, the /orch run or the /agent run
}

type runOwnerKey struct{}

// WithRunOwner tags ctx with the run it belongs to. The adapter tags each
// chat turn, /orch run and /agent run; a permission or ask request made
// under it carries the tag to the chat.
func WithRunOwner(ctx context.Context, o RunOwner) context.Context {
	return context.WithValue(ctx, runOwnerKey{}, o)
}

// RunOwnerFrom returns ctx's run tag; the zero RunOwner when ctx is nil or
// untagged.
func RunOwnerFrom(ctx context.Context) RunOwner {
	if ctx == nil {
		return RunOwner{}
	}
	o, _ := ctx.Value(runOwnerKey{}).(RunOwner)
	return o
}

// currentRun is the run a modal opened now belongs to. A chat turn, an /orch
// run and an /agent run never overlap (commands wait for a running turn).
func (m AppModel) currentRun() RunOwner {
	switch {
	case m.turn != nil:
		return RunOwner{Kind: OwnerTurn, Run: m.turnRun}
	case m.orchRun != 0:
		return RunOwner{Kind: OwnerOrch, Run: m.orchRun}
	case m.agentActive:
		return RunOwner{Kind: OwnerAgent, Run: m.agentRun}
	}
	return RunOwner{}
}

// agentRunCurrent reports whether run is the /agent run in progress.
func (m AppModel) agentRunCurrent(run uint64) bool {
	return m.agentActive && m.agentRun == run
}

// runActive reports whether o is still running. No run (Kind "") always is.
func (m AppModel) runActive(o RunOwner) bool {
	switch o.Kind {
	case OwnerTurn:
		return m.turn != nil && m.turnRun == o.Run
	case OwnerOrch:
		return m.orchRun != 0 && m.orchRun == o.Run
	case OwnerAgent:
		return m.agentRunCurrent(o.Run)
	}
	return true
}

// requestOwner is the run a permission or ask request belongs to: its tag,
// else the run active now (an untagged request). live is false when that
// run has ended (done is closed, or the chat has moved past it): the request
// is then answered at once and never shown (2.0 F2e).
func (m AppModel) requestOwner(tag RunOwner, done <-chan struct{}) (owner RunOwner, live bool) {
	if done != nil {
		select {
		case <-done:
			return tag, false
		default:
		}
	}
	if tag.Kind == "" {
		return m.currentRun(), true
	}
	return tag, m.runActive(tag)
}

// closeModalsOf answers and closes the modals of a run that has ended: the
// permission modal denies, the ask modal cancels.
func (m AppModel) closeModalsOf(o RunOwner) AppModel {
	if o.Kind == "" {
		return m
	}
	if m.permissionPrompt.Active() && m.permissionOwner == o {
		m.permissionPrompt = m.permissionPrompt.Dismiss()
		m.permissionOwner = RunOwner{}
	}
	if m.askPrompt.Active() && m.askOwner == o {
		m.askPrompt = m.askPrompt.Dismiss()
		m.askOwner = RunOwner{}
	}
	return m
}

// endAgentRun closes the modals of the /agent run that just ended.
func (m AppModel) endAgentRun() AppModel {
	if m.agentActive {
		m = m.closeModalsOf(RunOwner{Kind: OwnerAgent, Run: m.agentRun})
	}
	m.agentActive = false
	return m
}
