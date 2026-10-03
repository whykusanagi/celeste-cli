// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the main application model and layout logic.
package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/collections"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/commands"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/textutil"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/permissions"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools/mcp"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/venice"
)

// The typing animation ticks at 20fps; typing_speed (see typing_settings.go,
// default 60 chars/sec = 3 per tick) sets how many characters each tick shows.
const typingTickInterval = 50 * time.Millisecond // 20fps

// AppModel is the root model for the Celeste TUI application.
type AppModel struct {
	// Sub-components
	header           HeaderModel
	chat             ChatModel
	input            InputModel
	skills           SkillsModel
	status           StatusModel
	toolProgress     ToolProgressModel
	contextBar       ContextBarModel
	permissionPrompt PermissionPromptModel
	askPrompt        AskPromptModel
	mcpPanel         MCPPanelModel

	// Application state
	width             int
	height            int
	ready             bool
	nsfwMode          bool
	streaming         bool
	endpoint          string // Current endpoint (openai, venice, grok, etc.)
	safeEndpoint      string // Endpoint to return to when leaving NSFW mode
	model             string // Current model name
	imageModel        string // Current image generation model (for NSFW mode)
	provider          string // Current provider (grok, openai, venice, etc.) - detected from endpoint
	skillsEnabled     bool   // Whether skills/function calling is available
	modelPinned       bool   // /set-model --force: resolution leaves the model alone
	modelTrial        string // a /set-model name the provider hasn't confirmed yet
	modelBeforeTrial  string // the model to restore if modelTrial is not found
	modelCheckPending bool   // the restored model needs a catalog load (Init runs it)
	version           string // Application version (e.g., "1.0.1")
	build             string // Build identifier (e.g., "bubbletea-tui")
	grimoireContent   string // project context loaded at session start, for /grimoire
	codeGraphSummary  string // Code graph stats for /index command

	// Simulated typing state
	typingContent string // Full content to type
	typingPos     int    // Current position in content
	animFrame     int    // Animation frame counter
	// tickPending is set while the tick chain's next TickMsg (tickGen) is
	// scheduled and not yet handled: at most one chain runs (2.0 F2e).
	tickPending bool
	tickGen     uint64

	// streamDone is true once StreamDoneMsg has been received for the
	// currently-rendering assistant message. It coordinates the typing
	// animation with the network stream:
	//
	//   - While streamDone == false, the TickMsg handler MUST keep the
	//     ticker alive even after typingPos catches up to len(typingContent),
	//     because more chunks may still be arriving and extending the buffer.
	//   - Once streamDone == true, the TickMsg tick-complete branch can
	//     safely commit the final content to session history and stop.
	//
	// Without this coordination, a short first chunk (1-3 chars) drained
	// the typing animation before the second chunk arrived, committed that
	// single character to session persistence as the "complete" assistant
	// reply, and left every subsequent chunk to pile up in a zombie buffer
	// with no active ticker to render it. The session and the next LLM
	// request would both see content_len=1 — the "O" truncation bug.
	// See the 2026-04-13 session log (~/.celeste/logs/) for a
	// captured reproduction.
	streamDone bool

	// Input submitted while a turn is running (#172). Steers are injected as
	// user messages at the next tool boundary; follow-ups are sent, one at a
	// time, once the turn finishes. Steers still queued when the turn ends
	// are sent first, as one message.
	steerQueue    []string
	followUpQueue []string
	// dispatchPending is set while a queued message is on its way back in as
	// a SendMessageMsg, so the dispatcher doesn't send a second one first.
	dispatchPending bool
	// interrupted is set by Esc: tools already running finish, queued ones
	// are skipped, and no follow-up request is sent for the turn.
	interrupted bool
	// summarizing is set while a compaction summary is being written, so
	// only one runs at a time.
	summarizing bool

	// The running chat turn (2.0 F2d): a loop.Loop run whose events arrive
	// as TurnEventMsg tagged turnRun; nil when idle. loopSteers counts steers
	// handed to it that it has not joined or blocked yet. heldSummary is a
	// background summary that finished while the turn ran; onTurnDone
	// applies it if it still fits.
	turn        TurnHandle
	turnRun     uint64
	turnSeq     uint64
	loopSteers  int
	heldSummary *ContextSummarizedMsg

	// LLM client (injected)
	llmClient LLMClient

	// Session persistence (optional)
	sessionManager SessionManager
	currentSession Session

	// Configuration (for context limits, etc.)
	config *config.Config

	// Segmented bottom status line and its inputs
	statusLine  StatusLineModel      // git / project / model / effort / perms / session
	workDir     string               // process working directory (for git + project)
	permChecker *permissions.Checker // permission checker, for status-line mode display
	effort      string               // mirrored reasoning-effort level for the status line

	// Context tracking (NEW)
	contextTracker *config.ContextTracker

	// Interactive selector
	selector       SelectorModel
	selectorActive bool

	// Collections view
	collectionsModel *CollectionsModel
	menuModel        *MenuModel
	skillsBrowser    *SkillsBrowserModel
	graphModel       *GraphModel
	memoryManager    *MemoryManagerModel
	personaPanel     *PersonaPanelModel
	sessionPanel     *SessionPanelModel
	viewMode         string // "chat", "collections", "menu", "skills", "graph", "memories", "sessions"

	// Code graph indexer (for /graph view)
	codeGraphIndexer *codegraph.Indexer

	// Split panel for orchestrator/agent view
	splitPanel     *SplitPanel
	splitPanelMode bool

	// Running token totals for the current orchestrator run
	orchInputTokens  int
	orchOutputTokens int
	// orchRun tags the /orch run that owns the turn (0 = none); orchSeq
	// numbers them. Events and the StreamStartMsg of any other run (one
	// Esc or Ctrl+C cancelled) are ignored, so a late terminal event can't
	// clear a newer turn's cancel or set streaming again.
	orchRun uint64
	orchSeq uint64

	// Running token totals for the current agent run
	agentInputTokens  int
	agentOutputTokens int
	// agentActive is set while an /agent command runs; agentRun numbers
	// it (agentSeq counts them), tagging its permission and ask requests.
	agentActive bool
	agentRun    uint64
	agentSeq    uint64

	// The run each modal belongs to (none: it stays until answered). When
	// that run ends, its modal is answered (deny, cancelled) and closed, so
	// it never stays up swallowing keys and holding the Gate's lock.
	permissionOwner RunOwner
	askOwner        RunOwner

	// planning keeps /plan's "Planning..." status until the reply streams.
	planning bool
	// stopHookRunning is set while the Stop hook decides whether the turn
	// may end; heldReady is the "Ready" status it shows when the turn ends.
	stopHookRunning bool
	heldReady       string

	// Graceful Ctrl+C handling
	cancelFunc       context.CancelFunc
	interruptPending bool
	lastInterrupt    time.Time
	agentRunStart    time.Time

	// Per-message response timing and token stats (regular chat)
	streamStart   time.Time
	lastMsgInTok  int
	lastMsgOutTok int
}

// LLMClient interface for sending messages to the LLM.
type LLMClient interface {
	TurnRunner
	GetSkills() []SkillDefinition
}

// ContextCompactor is an optional extension that keeps the history inside
// the context window (#174). CompactContext prunes old tool results;
// SummarizeContext replaces older history with a structured summary written
// by the small-model role (it blocks, so the app calls it from a command).
// A context overflow during a turn is the loop's to retry (2.0 F2d).
type ContextCompactor interface {
	CompactContext(msgs []ChatMessage, window, used int, force bool) CompactOutcome
	SummarizeContext(ctx context.Context, msgs []ChatMessage, focus string) (SummaryOutcome, error)
}

// CompactOutcome is the result of a prune.
type CompactOutcome struct {
	// Edits maps tool call IDs to the placeholder that replaces the result.
	Edits       map[string]string
	Summary     string
	SavedTokens int
	// StillOver reports that the history is still over the compaction
	// threshold after pruning, so the summary rung should run.
	StillOver bool
}

// SummaryOutcome is the result of a summary: the first Cut LLM messages are
// replaced by Messages.
type SummaryOutcome struct {
	Cut         int
	Messages    []ChatMessage
	Line        string
	TokensAfter int
}

// AgentCommandRunner is an optional extension for handling /agent from TUI.
type AgentCommandRunner interface {
	// RunAgentCommand starts /agent; run tags the goal run's context
	// (WithRunOwner), so its permission and ask requests name it.
	RunAgentCommand(args []string, run uint64) tea.Cmd
}

// OrchestratorCommandRunner is an optional extension for handling /orchestrate from TUI.
type OrchestratorCommandRunner interface {
	// RunOrchestratorCommand starts the run and tags its StreamStartMsg and
	// every OrchestratorEventMsg with run.
	RunOrchestratorCommand(goal string, run uint64) tea.Cmd
}

// SubagentInfo is a TUI-facing view of a subagent run.
type SubagentInfo struct {
	ID      string
	TaskID  string
	Name    string
	Element string
	Status  string // "waiting", "running", "completed", "failed"
	Turns   int
	Elapsed time.Duration
	Type    string // explore, general or review; "" for an untyped run
	Summary string // a typed run's result summary
}

// agentSummaryLine is a typed subagent's summary as one /agents line: its
// first line, cut to 160 bytes on a character boundary.
func agentSummaryLine(summary string) string {
	line, _, more := strings.Cut(strings.TrimSpace(summary), "\n")
	if cut := textutil.CutBytes(line, 160); cut != line || more {
		return strings.TrimSpace(cut) + "…"
	}
	return line
}

// SubagentLister is an optional extension for /agents command.
type SubagentLister interface {
	ListSubagents() []SubagentInfo
}

// SubagentResumer is an optional extension for /agents resume <id>.
type SubagentResumer interface {
	ResumeSubagent(ctx context.Context, checkpointID string) (string, error)
}

// SubagentKiller is an optional extension for /agents kill <id>. It cancels a
// specific in-flight subagent (task 6ffb5a7c).
type SubagentKiller interface {
	KillSubagent(id string) bool
}

// PromptRefresher is an optional extension to reload the system prompt
// mid-session (after /confirm, /user, or other prompt-affecting changes).
type PromptRefresher interface {
	RefreshSystemPrompt()
}

// Checkpointer undoes and lists the file changes this session made (2.0
// F4). The chat's client adapter implements it over the session's
// checkpoints; UndoLastChange and SessionChanges return the text to show.
// RewindTo (2.0 W4 ruling 5, /rewind) restores, newest first, every change
// from the first one made by any of callIDs onward and returns the files
// it restored; none of them made a change: nothing, no error. With
// checkpoints off it returns an error that is ErrCheckpointsOff.
type Checkpointer interface {
	UndoLastChange() (string, error)
	SessionChanges() (string, error)
	RewindTo(callIDs []string) (RewindResult, error)
}

// RewindResult is what Checkpointer.RewindTo restored.
type RewindResult struct {
	// Restored names the files restored (also on an error: those
	// restored before it).
	Restored []string
	// Partial: the store had already evicted the changes of some of the
	// calls (its cap), so they were not restored.
	Partial bool
}

// ErrCheckpointsOff: this session keeps no file checkpoints (no store, or
// no home directory to keep them in).
var ErrCheckpointsOff = errors.New("file checkpoints are off in this session")

// EndpointSwitcher interface for clients that support dynamic endpoint switching.
type EndpointSwitcher interface {
	SwitchEndpoint(endpoint string) error
	ChangeModel(model string) error
}

// ThinkingConfigSetter interface for clients that support extended thinking / reasoning effort.
type ThinkingConfigSetter interface {
	SetThinkingLevel(level string)
}

// SkillDefinition represents a skill/function that can be called.
type SkillDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// VeniceConfigData holds Venice.ai configuration from skills.json.
type VeniceConfigData struct {
	APIKey     string
	BaseURL    string
	Model      string // Chat model
	ImageModel string // Image generation model
}

// loadVeniceConfig loads Venice configuration from ~/.celeste/skills.json.
func loadVeniceConfig() (VeniceConfigData, error) {
	// Load skills config
	skillsConfig, err := config.LoadSkillsConfig()
	if err != nil {
		return VeniceConfigData{}, fmt.Errorf("failed to load skills config: %w", err)
	}

	// Create config loader
	loader := config.NewConfigLoader(skillsConfig)

	// Get Venice config via loader
	veniceConfig, err := loader.GetVeniceConfig()
	if err != nil {
		return VeniceConfigData{}, err
	}

	return VeniceConfigData{
		APIKey:     veniceConfig.APIKey,
		BaseURL:    veniceConfig.BaseURL,
		Model:      veniceConfig.Model,
		ImageModel: veniceConfig.ImageModel,
	}, nil
}

// NewApp creates a new TUI application model.
func NewApp(llmClient LLMClient) AppModel {
	return AppModel{
		header:           NewHeaderModel(),
		chat:             NewChatModel(),
		input:            NewInputModel(),
		skills:           NewSkillsModel(),
		status:           NewStatusModel(),
		toolProgress:     NewToolProgressModel(),
		contextBar:       NewContextBarModel(),
		permissionPrompt: NewPermissionPromptModel(),
		askPrompt:        NewAskPromptModel(),
		statusLine:       NewStatusLineModel(),
		mcpPanel:         NewMCPPanelModel(),
		llmClient:        llmClient,
		viewMode:         "chat",
	}
}

// Init implements tea.Model.
func (m AppModel) Init() tea.Cmd {
	var check tea.Cmd
	if m.modelCheckPending {
		// The restored session's model needs a catalog or a check the
		// startup didn't do: run it off the UI loop.
		_, check = m.resolveServedModel()
	}
	return tea.Batch(
		m.input.Init(),
		tea.EnterAltScreen,
		gitFetchCmd(m.workDir),
		check,
	)
}

// SetWorkDir sets the working directory used for git polling and the project segment.
func (m AppModel) SetWorkDir(dir string) AppModel { m.workDir = dir; return m }

// SetPermissionChecker injects the permission checker so the status line can
// display the current mode.
func (m AppModel) SetPermissionChecker(c *permissions.Checker) AppModel {
	m.permChecker = c
	return m
}

// SetMCPManager wires the MCP manager and discovered configs into the /mcp
// panel so it can connect/disconnect/toggle servers at runtime.
func (m AppModel) SetMCPManager(manager *mcp.Manager, configs map[string]mcp.ServerConfig) AppModel {
	m.mcpPanel.SetManager(manager, configs)
	return m
}

// syncStatusLine copies non-git AppModel state (project, model, effort,
// permission mode, session) into the status line. Git fields are set separately
// by the GitStatusMsg handler.
func (m AppModel) syncStatusLine() AppModel {
	sl := m.statusLine.
		SetProject(filepath.Base(m.workDir)).
		SetModel(m.model).
		SetEffort(m.effort).
		SetSkills(m.skills.Enabled(), m.skills.Count())
	if m.permChecker != nil {
		sl = sl.SetPermMode(m.permChecker.Mode().String())
	}
	if s, ok := m.currentSession.(*config.Session); ok && s != nil {
		sl = sl.SetSession(s.Name)
	}
	m.statusLine = sl
	return m
}

// Update implements tea.Model.
// Update implements tea.Model. After handling msg it sends the next queued
// message if the turn has finished (#172).
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	model, cmd := m.update(msg)
	am, ok := model.(AppModel)
	if !ok {
		return model, cmd
	}
	am, next := am.dispatchQueued()
	if next == nil {
		return am, cmd
	}
	return am, tea.Batch(cmd, next)
}

func (m AppModel) update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Always propagate WindowSizeMsg to parent layout regardless of viewMode.
	// Sub-views eat all messages via early return, which means terminal resizes
	// during /graph, /memories, /skills etc. leave the header with stale width.
	if sizeMsg, isResize := msg.(tea.WindowSizeMsg); isResize {
		m.width = sizeMsg.Width
		m.height = sizeMsg.Height
		m.ready = true
		m.header = m.header.SetWidth(m.width)
		m.chat = m.chat.SetSize(m.width, m.height-18)
		m.input = m.input.SetWidth(m.width)
		m.status = m.status.SetWidth(m.width)
		// Don't return — let sub-views also handle the resize
	}

	// The tick chain's bookkeeping runs before a sub-view can eat the tick
	// (2.0 F2e): a superseded chain's tick is dropped, and the live chain's
	// arrival lets the next one be scheduled.
	if tk, ok := msg.(TickMsg); ok && tk.gen != 0 {
		if tk.gen != m.tickGen {
			return m, nil
		}
		m.tickPending = false
		if m.viewMode != "chat" {
			// A sub-view would eat the tick. While a reply streams, types
			// or tools run, keep the chain alive without animating, so the
			// reply types out and commits once the view closes (2.0 F2e).
			var tick tea.Cmd
			if m.streaming || m.typingContent != "" || m.toolProgress.HasActive() {
				tick = m.tick(typingTickInterval * 2)
			}
			return m, tick
		}
	}

	// Chat turn events bypass the sub-view routing below: the turn must keep
	// reading its events whichever view is showing.
	if ev, ok := msg.(TurnEventMsg); ok {
		return m.onTurnEvent(ev)
	}
	// So does a fetched model catalog: it belongs to the endpoint, not a view.
	if ready, ok := msg.(catalogReadyMsg); ok {
		return m.onCatalogReady(ready), nil
	}

	// Route to collections view if in that mode
	if m.viewMode == "collections" {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "q" || msg.String() == "Q" || msg.String() == "esc" {
				// Return to chat mode
				m.viewMode = "chat"
				return m, nil
			}
		}

		// Update collections model
		if m.collectionsModel != nil {
			updated, cmd := m.collectionsModel.Update(msg)
			if updatedModel, ok := updated.(CollectionsModel); ok {
				*m.collectionsModel = updatedModel
			}
			return m, cmd
		}
	}

	// If in menu view, handle all inputs there
	if m.viewMode == "menu" {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "q" || msg.String() == "Q" || msg.String() == "esc" {
				// Return to chat mode
				m.viewMode = "chat"
				return m, nil
			}
		case menuItemSelectedMsg:
			// User selected a menu item, execute it
			m.viewMode = "chat"
			return m, SendMessage("/" + msg.command)
		}

		// Update menu model
		if m.menuModel != nil {
			updated, cmd := m.menuModel.Update(msg)
			if updatedModel, ok := updated.(MenuModel); ok {
				*m.menuModel = updatedModel
			}
			return m, cmd
		}
	}

	// In skills view the browser owns filtering + navigation (letters feed the
	// type-ahead filter); the app only reacts to the messages it emits back.
	if m.viewMode == "skills" {
		switch msg := msg.(type) {
		case skillsBrowserBackMsg:
			m.viewMode = "chat"
			return m, nil
		case skillSelectedMsg:
			m.viewMode = "chat"
			m.input = m.input.SetValue(msg.skillName + " ")
			m.input = m.input.Focus()
			return m, nil
		}

		if m.skillsBrowser != nil {
			updated, cmd := m.skillsBrowser.Update(msg)
			if updatedModel, ok := updated.(SkillsBrowserModel); ok {
				*m.skillsBrowser = updatedModel
			}
			return m, cmd
		}
	} else if m.viewMode == "graph" {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "q" || msg.String() == "Q" || msg.String() == "esc" {
				m.viewMode = "chat"
				return m, nil
			}
		}
		if m.graphModel != nil {
			updated, cmd := m.graphModel.Update(msg)
			if updatedModel, ok := updated.(GraphModel); ok {
				*m.graphModel = updatedModel
			}
			return m, cmd
		}
	} else if m.viewMode == "persona" {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "esc" || msg.String() == "q" {
				// Save on close if modified
				if m.personaPanel != nil && m.personaPanel.Modified() {
					_ = m.personaPanel.Save()
					m.chat = m.chat.AddSystemMessage("✨ Persona sliders saved.")
				}
				m.viewMode = "chat"
				return m, nil
			}
		}
		if m.personaPanel != nil {
			updated, cmd := m.personaPanel.Update(msg)
			*m.personaPanel = updated
			return m, cmd
		}
	} else if m.viewMode == "sessions" {
		if m.sessionPanel != nil {
			// Pass ALL key messages to the session panel first
			updated, cmd := m.sessionPanel.Update(msg)
			*m.sessionPanel = updated

			// Check for exit (esc/q handled inside panel returning empty selected)
			if keyMsg, ok := msg.(tea.KeyMsg); ok {
				if keyMsg.String() == "esc" || keyMsg.String() == "q" {
					m.viewMode = "chat"
					m.sessionPanel = nil
					return m, nil
				}
			}

			// Handle selection
			if sel := m.sessionPanel.Selected(); sel != "" {
				m.viewMode = "chat"
				m = m.handleSessionAction(&commands.SessionAction{
					Action:    "resume",
					SessionID: sel,
				})
				if refresher, ok := m.llmClient.(PromptRefresher); ok {
					refresher.RefreshSystemPrompt()
				}
				m.sessionPanel = nil
				return m, nil
			}

			// Handle deletion
			if del := m.sessionPanel.Deleted(); del != "" {
				m.sessionPanel.deleted = "" // reset
				if m.sessionManager != nil {
					_ = m.sessionManager.Delete(del)
				}
			}

			return m, cmd
		}
	} else if m.viewMode == "memories" {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "q" || msg.String() == "Q" || msg.String() == "esc" {
				if m.memoryManager != nil && m.memoryManager.confirmed >= 0 {
					m.memoryManager.confirmed = -1
					m.memoryManager.message = ""
					return m, nil
				}
				m.viewMode = "chat"
				return m, nil
			}
		}
		if m.memoryManager != nil {
			updated, cmd := m.memoryManager.Update(msg)
			if updatedModel, ok := updated.(MemoryManagerModel); ok {
				*m.memoryManager = updatedModel
			}
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// If permission prompt is active, route keys to it first
		if m.permissionPrompt.Active() {
			var cmd tea.Cmd
			m.permissionPrompt, cmd = m.permissionPrompt.Update(msg)
			return m, cmd
		}

		// If ask prompt is active, route keys to it before normal handling
		if m.askPrompt.Active() {
			var cmd tea.Cmd
			m.askPrompt, cmd = m.askPrompt.Update(msg)
			return m, cmd
		}

		// If MCP panel is active, route keys to it
		if m.mcpPanel.Active() {
			var cmd tea.Cmd
			m.mcpPanel, cmd = m.mcpPanel.Update(msg)
			return m, cmd
		}

		// If selector is active, route all keys to it
		if m.selectorActive {
			var cmd tea.Cmd
			m.selector, cmd = m.selector.Update(msg)
			return m, cmd
		}

		// Esc on an empty input interrupts a running turn (#172). With text in
		// the input, Esc keeps its old meaning (clear the draft).
		if msg.String() == "esc" && m.turnActive() && strings.TrimSpace(m.input.Value()) == "" && !m.input.HasSuggestions() {
			return m.interrupt(), nil
		}

		switch msg.String() {
		case "ctrl+c":
			if m.turn != nil || m.cancelFunc != nil {
				// Active operation running: cancel it. Double Ctrl+C within 3s quits.
				again := m.interruptPending && time.Since(m.lastInterrupt) < 3*time.Second
				if m.turn != nil {
					m = m.interrupt()
				} else {
					m.cancelFunc()
					m.cancelFunc = nil
					m.orchRun = 0
					m = m.endAgentRun() // its late events are dropped (2.0 F2e)
					m.streaming = false
				}
				m.status = m.status.SetText("Cancelled. Press Ctrl+C again to exit")
				if again {
					m.persistSession()
					return m, tea.Quit
				}
				m.interruptPending = true
				m.lastInterrupt = time.Now()
				return m, nil
			}
			// No active operation — check for double tap
			if m.interruptPending && time.Since(m.lastInterrupt) < 3*time.Second {
				m.persistSession()
				return m, tea.Quit
			}
			m.interruptPending = true
			m.lastInterrupt = time.Now()
			m.status = m.status.SetText("Press Ctrl+C again to exit")
			return m, nil
		case "ctrl+k":
			// Toggle skill call logs visibility
			m.chat = m.chat.ToggleSkillCalls()
			if m.chat.ShowingSkillCalls() {
				m.status = m.status.SetText("Skill call logs: visible")
			} else {
				m.status = m.status.SetText("Skill call logs: hidden")
			}
		case "pgup", "shift+up", "home":
			if m.splitPanelMode && m.splitPanel != nil {
				m.splitPanel.ScrollUp(5)
			} else {
				var cmd tea.Cmd
				m.chat, cmd = m.chat.Update(msg)
				cmds = append(cmds, cmd)
			}
		case "pgdown", "shift+down", "end":
			if m.splitPanelMode && m.splitPanel != nil {
				m.splitPanel.ScrollDown(5)
			} else {
				var cmd tea.Cmd
				m.chat, cmd = m.chat.Update(msg)
				cmds = append(cmds, cmd)
			}
		default:
			// Other keys go to input
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			cmds = append(cmds, cmd)

			// Update skills panel with current input for contextual help
			m.skills = m.skills.SetCurrentInput(m.input.Value())
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

		// Calculate component heights - RPG menu layout
		headerHeight := 1
		inputHeight := 3  // 1 border + 1 text + 1 typeahead hint line
		skillsHeight := 1 // collapsed skills panel: active-skill signal only (usually empty)
		statusHeight := 1
		statusLineHeight := 1 // segmented status line
		hintsHeight := 1      // contextual key hints
		chatHeight := m.height - headerHeight - inputHeight - skillsHeight - statusHeight - statusLineHeight - hintsHeight

		// Ensure minimum chat height on very short terminals.
		if chatHeight < 5 {
			chatHeight = 5
		}

		// Update component sizes
		m.header = m.header.SetWidth(m.width)
		m.chat = m.chat.SetSize(m.width, chatHeight)
		m.input = m.input.SetWidth(m.width)
		m.skills = m.skills.SetSize(m.width, skillsHeight)
		m.status = m.status.SetWidth(m.width)
		m.statusLine = m.statusLine.SetWidth(m.width)

		// Resize new components
		if m.sessionPanel != nil {
			*m.sessionPanel = m.sessionPanel.SetWidth(m.width).SetHeight(chatHeight)
		}
		m.toolProgress.SetSize(m.width, 0)
		m.contextBar.SetSize(m.width, 0)
		m.permissionPrompt.SetSize(m.width, 0)
		m.askPrompt.SetSize(m.width, 0)
		m.mcpPanel.SetSize(m.width, m.height-10)

		// Resize split panel if active: available height = total minus header/status/input
		if m.splitPanel != nil {
			panelH := m.height - 5 // header(1) + status(1) + input(3)
			if panelH < 5 {
				panelH = 5
			}
			m.splitPanel.Resize(m.width, panelH)
		}

	case SendMessageMsg:
		content := strings.TrimSpace(msg.Content)
		m.dispatchPending = false

		// A turn is still running: queue instead of starting a concurrent
		// request (#172). Enter steers, Tab queues a follow-up; commands wait
		// for the turn to finish, except /agents so a subagent can be killed.
		if content != "" && m.turnActive() && !runsDuringTurn(content) {
			return m.enqueue(content, msg.FollowUp || strings.HasPrefix(content, "/")), nil
		}
		// Idle again, so an earlier interrupt no longer applies.
		m.interrupted = false

		// Clear completed tool progress from previous turn
		m.toolProgress.ClearCompleted()

		// Dismiss the split panel when user sends next message (unless it's another /orchestrate)
		if m.splitPanelMode && !strings.HasPrefix(content, "/orchestrate") && !strings.HasPrefix(content, "/orch ") {
			m.splitPanelMode = false
			m.splitPanel = nil
		}

		// Check if it's a slash command first
		if cmd := commands.Parse(content); cmd != nil {
			// Handle Phase 4 commands that require app state (contextTracker, currentSession)
			// /session alone opens the picker; with a subcommand
			// (merge, resume, new, ...) it falls through to run the action.
			if cmd.Name == "session" && len(cmd.Args) == 0 {
				m.viewMode = "sessions"
				panel := NewSessionPanelModel(m.workDir)
				panel = panel.SetWidth(m.width).SetHeight(m.height)
				m.sessionPanel = &panel
				return m, nil
			}

			switch cmd.Name {
			case "agent":
				if len(cmd.Args) == 0 {
					m.chat = m.chat.AddSystemMessage("Usage: /agent <goal>\n       /agent list-runs\n       /agent resume <run-id>")
					return m, nil
				}
				agentRunner, ok := m.llmClient.(AgentCommandRunner)
				if !ok {
					m.chat = m.chat.AddSystemMessage("❌ /agent is unavailable for this client.")
					return m, nil
				}

				m.streaming = true
				m.agentActive = true
				m.agentSeq++
				m.agentRun = m.agentSeq
				m.status = m.status.SetStreaming(true)
				m.status = m.status.SetText(StreamingSpinner(0) + " Running agent...")
				m.chat = m.chat.AddSystemMessage("🤖 Agent running: " + strings.Join(cmd.Args, " "))

				agentArgs := append([]string{}, cmd.Args...)
				tick := m.restartTick(typingTickInterval * 2)
				return m, tea.Batch(agentRunner.RunAgentCommand(agentArgs, m.agentRun), tick)

			case "orchestrate", "orch":
				if len(cmd.Args) == 0 {
					m.chat = m.chat.AddSystemMessage("Usage: /orchestrate <goal>")
					return m, nil
				}
				orchRunner, ok := m.llmClient.(OrchestratorCommandRunner)
				if !ok {
					m.chat = m.chat.AddSystemMessage("❌ /orchestrate is unavailable for this client.")
					return m, nil
				}
				goal := expandFileRefs(strings.Join(cmd.Args, " "))
				m.streaming = true
				m.status = m.status.SetStreaming(true)
				m.status = m.status.SetText(StreamingSpinner(0) + " Orchestrating...")
				m.chat = m.chat.AddSystemMessage("🎭 Orchestrator: " + goal)
				m.orchInputTokens = 0
				m.orchOutputTokens = 0
				m.orchSeq++
				m.orchRun = m.orchSeq
				tick := m.restartTick(typingTickInterval * 2)
				return m, tea.Batch(orchRunner.RunOrchestratorCommand(goal, m.orchRun), tick)

			case "stats":
				// Pass animation frame for flickering corruption effects
				argsWithFrame := append([]string{"--frame", fmt.Sprintf("%d", m.animFrame)}, cmd.Args...)
				result := commands.HandleStatsCommand(argsWithFrame, m.contextTracker)
				if result.ShouldRender {
					m.chat = m.chat.AddSystemMessage(result.Message)
				}
				return m, nil

			case "export":
				// Get pointer to current session for export
				var sessionPtr *config.Session
				if sess, ok := m.currentSession.(*config.Session); ok {
					sessionPtr = sess
				}
				result := commands.HandleExportCommand(cmd.Args, sessionPtr)
				if result.ShouldRender {
					m.chat = m.chat.AddSystemMessage(result.Message)
				}
				return m, nil

			case "compact":
				// Summarize older history now, optionally steered (#174).
				return m.startSummary(strings.Join(cmd.Args, " "), true)

			case "handoff":
				// Summarize everything into a fresh session (#174).
				return m.startHandoff(strings.Join(cmd.Args, " "))

			case "context":
				if len(cmd.Args) > 0 && cmd.Args[0] == "compact" {
					var out CompactOutcome
					m, out = m.compactContext(true)
					if len(out.Edits) > 0 {
						m.persistSession() // the pruned results are the session now
					}
					if len(out.Edits) == 0 || out.StillOver {
						// Pruning found too little: go to the summary rung.
						return m.startSummaryAs("", len(out.Edits) == 0, "manual")
					}
					return m, nil
				}
				result := commands.HandleContextCommand(cmd.Args, m.contextTracker)
				if result.ShouldRender {
					m.chat = m.chat.AddSystemMessage(result.Message)
				}
				return m, nil

			case "collections":
				if m.config == nil || m.config.XAIManagementAPIKey == "" {
					m.chat = m.chat.AddSystemMessage("Collections requires an xAI Management API key.\n\n" +
						"Setup:\n" +
						"  celeste config --set-management-key YOUR_XAI_MGMT_KEY\n\n" +
						"Get your management key from https://console.x.ai\n" +
						"This is separate from your chat API key.")
					return m, nil
				}

				m.viewMode = "collections"
				if m.collectionsModel == nil {
					client := collections.NewClient(m.config.XAIManagementAPIKey)
					manager := collections.NewManager(client, m.config)
					model := NewCollectionsModel(manager)
					m.collectionsModel = &model
				}
				return m, m.collectionsModel.Init()

			case "menu":
				// Switch to menu view
				m.viewMode = "menu"

				// Create menu model if not exists
				if m.menuModel == nil {
					model := NewMenuModel()
					m.menuModel = &model
				}

				if m.menuModel != nil {
					return m, m.menuModel.Init()
				}
				return m, nil

			case "tools", "skills":
				// Switch to interactive skills browser
				m.viewMode = "skills"
				skillsList := []SkillDefinition{}
				if m.llmClient != nil {
					skillsList = m.llmClient.GetSkills()
				}
				model := NewSkillsBrowserModel(skillsList)
				model.width, model.height = m.width, m.height
				m.skillsBrowser = &model
				return m, m.skillsBrowser.Init()

			case "mcp":
				m.mcpPanel.Show()
				return m, nil

			case "plan":
				cwd, _ := os.Getwd()
				planPaths := []string{
					cwd + "/.celeste/plan.md",
					cwd + "/CODEBASE_FIX_PLAN.md",
					cwd + "/PLAN.md",
					cwd + "/plan.md",
					cwd + "/FIX_PLAN.md",
				}

				if len(cmd.Args) == 0 {
					// No args: show current plan or say none found
					found := false
					for _, p := range planPaths {
						data, err := os.ReadFile(p)
						if err == nil {
							relPath := strings.TrimPrefix(p, cwd+"/")
							m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Plan (%s):\n\n%s", relPath, string(data)))
							found = true
							break
						}
					}
					if !found {
						m.chat = m.chat.AddSystemMessage("No plan found.\n\nUsage:\n  /plan <goal>    Create a plan\n  /plan show      Show current plan\n  /plan cancel    Delete plan")
					}
					return m, nil
				}

				subCmd := cmd.Args[0]
				switch subCmd {
				case "show":
					found := false
					for _, p := range planPaths {
						data, err := os.ReadFile(p)
						if err == nil {
							relPath := strings.TrimPrefix(p, cwd+"/")
							m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Plan (%s):\n\n%s", relPath, string(data)))
							found = true
							break
						}
					}
					if !found {
						m.chat = m.chat.AddSystemMessage("No plan found.")
					}

				case "cancel":
					deleted := false
					for _, p := range planPaths {
						if err := os.Remove(p); err == nil {
							relPath := strings.TrimPrefix(p, cwd+"/")
							m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Plan cancelled: %s", relPath))
							deleted = true
							break
						}
					}
					if !deleted {
						m.chat = m.chat.AddSystemMessage("No active plan to cancel.")
					}

				default:
					// /plan <goal> — ask Celeste to create a plan
					goal := strings.Join(cmd.Args, " ")
					planPath := cwd + "/.celeste/plan.md"

					// Send as a user message with structured plan prompt
					planPrompt := fmt.Sprintf(
						"Create a structured plan for: %s\n\n"+
							"Write the plan to %s using write_file. Format as a markdown checklist:\n"+
							"```\n"+
							"# Plan: %s\n\n"+
							"- [ ] Step 1: ...\n"+
							"- [ ] Step 2: ...\n"+
							"- [ ] Step 3: ...\n"+
							"```\n\n"+
							"Use `- [ ]` for pending, `- [x]` for done, `- [>]` for in progress.\n"+
							"Keep steps concrete and actionable. After writing the plan, show it to me.",
						goal, planPath, goal,
					)
					m.chat = m.chat.AddUserMessage(planPrompt)

					// Persist the user message
					if m.currentSession != nil {
						if cs, ok := m.currentSession.(*config.Session); ok {
							cs.Messages = append(cs.Messages, config.SessionMessage{
								Role: "user", Content: planPrompt, Timestamp: time.Now(),
							})
						}
					}

					// Send to the loop.
					var turnCmd tea.Cmd
					m, turnCmd = m.startTurn()
					m.status = m.status.SetText("Planning...")
					m.planning = m.turn != nil
					return m, turnCmd
				}
				return m, nil

			case "diff":
				cp, ok := m.llmClient.(Checkpointer)
				if !ok {
					m.chat = m.chat.AddSystemMessage("Diff is unavailable in this session.")
					return m, nil
				}
				text, err := cp.SessionChanges()
				if err != nil {
					text = "Diff: " + err.Error()
				}
				m.chat = m.chat.AddSystemMessage(text)
				return m, nil

			case "undo":
				cp, ok := m.llmClient.(Checkpointer)
				if !ok {
					m.chat = m.chat.AddSystemMessage("Undo is unavailable in this session.")
					return m, nil
				}
				text, err := cp.UndoLastChange()
				if err != nil {
					text = "Undo: " + err.Error()
				}
				m.chat = m.chat.AddSystemMessage(text)
				return m, nil

			case "rewind":
				m = m.rewind(cmd.Args)
				return m, nil

			case "fork":
				m = m.fork()
				return m, nil

			case "memories":
				cwd, _ := os.Getwd()
				m.viewMode = "memories"
				model := NewMemoryManagerModel(cwd)
				m.memoryManager = &model
				return m, nil

			case "costs":
				m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Session Costs:\n  Tokens: %d used / %d limit\n  Turns: %d\n\nFor detailed cost breakdown: `celeste costs`",
					m.contextBar.usedTokens, m.contextBar.maxTokens, m.contextBar.turnCount))
				return m, nil

			case "init":
				// 2.0 W4 (ruling 7): the only writers of .grimoire (and
				// AGENTS.md) besides `celeste init`; nothing is overwritten.
				m.chat = m.chat.AddSystemMessage(runInitCommand(m.projectDir(), cmd.Args))
				return m, nil

			case "grimoire":
				// Re-read from disk so edits are reflected immediately
				// (2.0 W4, ruling 2); celeste grimoire shows the same text.
				// With nothing on disk, the context this session loaded at
				// start still applies, so it is shown under that note.
				text, found := grimoire.Describe(m.projectDir())
				if !found && m.grimoireContent != "" {
					text += "\n\nNo project context on disk now; this session loaded at start:\n\n" + m.grimoireContent
				}
				m.chat = m.chat.AddSystemMessage(text)
				return m, nil

			case "index":
				if m.codeGraphIndexer == nil {
					m.chat = m.chat.AddSystemMessage("No code graph index loaded.\nRun `celeste index` to build one.")
					return m, nil
				}

				subCmd := "status"
				if len(cmd.Args) > 0 {
					subCmd = strings.ToLower(cmd.Args[0])
				}

				switch subCmd {
				case "rebuild":
					m.chat = m.chat.AddSystemMessage("Rebuilding code graph index...")
					m.streaming = true
					m.status = m.status.SetStreaming(true)
					m.status = m.status.SetText("Rebuilding index...")
					indexer := m.codeGraphIndexer
					return m, func() tea.Msg {
						err := indexer.Build()
						if err != nil {
							return StreamErrorMsg{Err: fmt.Errorf("rebuild failed: %w", err)}
						}
						stats, _ := indexer.Stats()
						msg := fmt.Sprintf("Index rebuilt: %d files, %d symbols, %d edges",
							stats.TotalFiles, stats.TotalSymbols, stats.TotalEdges)
						return AgentProgressMsg{Kind: AgentProgressResponse, Text: msg}
					}

				case "update":
					m.chat = m.chat.AddSystemMessage("Updating code graph index (incremental)...")
					m.streaming = true
					m.status = m.status.SetStreaming(true)
					m.status = m.status.SetText("Updating index...")
					indexer := m.codeGraphIndexer
					return m, func() tea.Msg {
						err := indexer.Update()
						if err != nil {
							return StreamErrorMsg{Err: fmt.Errorf("update failed: %w", err)}
						}
						stats, _ := indexer.Stats()
						msg := fmt.Sprintf("Index updated: %d files, %d symbols, %d edges",
							stats.TotalFiles, stats.TotalSymbols, stats.TotalEdges)
						return AgentProgressMsg{Kind: AgentProgressResponse, Text: msg}
					}

				case "snapshot":
					indexer := m.codeGraphIndexer
					snap, err := indexer.TakeSnapshot()
					if err != nil {
						m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Snapshot failed: %v", err))
						return m, nil
					}
					if err := indexer.SaveSnapshot(snap); err != nil {
						m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Save failed: %v", err))
						return m, nil
					}
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Snapshot saved: %s (%d symbols, %d edges)",
						snap.CommitSHA, snap.SymbolCount, snap.EdgeCount))

				case "diff":
					indexer := m.codeGraphIndexer
					current, err := indexer.TakeSnapshot()
					if err != nil {
						m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Snapshot failed: %v", err))
						return m, nil
					}
					previous, err := indexer.LatestSnapshot()
					if err != nil || previous == nil {
						m.chat = m.chat.AddSystemMessage("No previous snapshot to diff against.\nRun `/index snapshot` first, then make changes and rebuild.")
						return m, nil
					}
					diff := codegraph.DiffSnapshots(previous, current)
					var sb strings.Builder
					sb.WriteString(fmt.Sprintf("Graph diff: %s → %s\n", diff.BeforeSHA, diff.AfterSHA))
					sb.WriteString(fmt.Sprintf("  Symbols: +%d / -%d\n", diff.Summary.SymbolsAdded, diff.Summary.SymbolsRemoved))
					sb.WriteString(fmt.Sprintf("  Edges:   +%d / -%d\n", diff.Summary.EdgesAdded, diff.Summary.EdgesRemoved))
					if len(diff.AddedSymbols) > 0 {
						sb.WriteString(fmt.Sprintf("  Added: %s\n", strings.Join(diff.AddedSymbols[:min(len(diff.AddedSymbols), 10)], ", ")))
					}
					if len(diff.RemovedSymbols) > 0 {
						sb.WriteString(fmt.Sprintf("  Removed: %s\n", strings.Join(diff.RemovedSymbols[:min(len(diff.RemovedSymbols), 10)], ", ")))
					}
					m.chat = m.chat.AddSystemMessage(sb.String())

				case "impact":
					base := "HEAD~1"
					if len(cmd.Args) > 1 {
						base = cmd.Args[1]
					}
					indexer := m.codeGraphIndexer
					result, err := indexer.AnalyzeChanges(base)
					if err != nil {
						m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Impact analysis failed: %v", err))
						return m, nil
					}
					var sb strings.Builder
					sb.WriteString(fmt.Sprintf("Change Impact (diff: %s)\n\n", base))
					sb.WriteString(fmt.Sprintf("  Files changed:     %d\n", result.Summary.FilesChanged))
					sb.WriteString(fmt.Sprintf("  Symbols changed:   %d\n", result.Summary.SymbolsChanged))
					sb.WriteString(fmt.Sprintf("  Callers affected:  %d\n", result.Summary.CallersAffected))
					sb.WriteString(fmt.Sprintf("  Test gaps:         %d\n", result.Summary.TestGaps))
					sb.WriteString(fmt.Sprintf("  Max risk score:    %.2f\n", result.Summary.MaxRiskScore))
					if len(result.DirectlyChanged) > 0 {
						sb.WriteString("\nChanged symbols:\n")
						for i, sym := range result.DirectlyChanged {
							if i >= 15 {
								sb.WriteString(fmt.Sprintf("  ... +%d more\n", len(result.DirectlyChanged)-15))
								break
							}
							sb.WriteString(fmt.Sprintf("  [%.2f] %-10s %s\n", sym.RiskScore, sym.Kind, sym.Name))
						}
					}
					if len(result.AffectedCallers) > 0 {
						sb.WriteString("\nAffected callers (blast radius):\n")
						for i, sym := range result.AffectedCallers {
							if i >= 10 {
								sb.WriteString(fmt.Sprintf("  ... +%d more\n", len(result.AffectedCallers)-10))
								break
							}
							sb.WriteString(fmt.Sprintf("  [%.2f] %-10s %s\n", sym.RiskScore, sym.Kind, sym.Name))
						}
					}
					if len(result.UncoveredByTests) > 0 {
						sb.WriteString("\nTest gaps:\n")
						for i, name := range result.UncoveredByTests {
							if i >= 10 {
								sb.WriteString(fmt.Sprintf("  ... +%d more\n", len(result.UncoveredByTests)-10))
								break
							}
							sb.WriteString(fmt.Sprintf("  - %s\n", name))
						}
					}
					m.chat = m.chat.AddSystemMessage(sb.String())

				default: // "status" or no args
					viz := RenderCodeGraphConstellation(m.codeGraphIndexer, m.width)
					if viz != "" {
						m.chat = m.chat.AddSystemMessage(viz)
					} else {
						m.codeGraphSummary = m.codeGraphIndexer.ProjectSummary()
						m.chat = m.chat.AddSystemMessage("Code Graph:\n" + m.codeGraphSummary)
					}
				}
				return m, nil

			case "persona":
				m.viewMode = "persona"
				panel := NewPersonaPanelModel()
				panel = panel.SetWidth(m.width)
				m.personaPanel = &panel
				return m, nil

			case "user":
				user := config.LoadUser()
				if len(cmd.Args) == 0 {
					// Show current user
					status := fmt.Sprintf("Current user: %s", user.DisplayName())
					if user.IsKusanagi() {
						status += " (Kusanagi mode — full sibling dynamic)"
					} else if user.Name == "" {
						status += " (default — set with /user <name>)"
					}
					m.chat = m.chat.AddSystemMessage(status)
					return m, nil
				}
				newName := strings.Join(cmd.Args, " ")
				if newName == "reset" || newName == "default" {
					user = config.DefaultUserIdentity()
				} else {
					user.Name = newName
				}
				if err := user.Save(); err != nil {
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Failed to save user: %v", err))
				} else {
					if refresher, ok := m.llmClient.(PromptRefresher); ok {
						refresher.RefreshSystemPrompt()
					}
					msg := fmt.Sprintf("User set to: %s", user.DisplayName())
					if user.IsKusanagi() {
						msg += "\nKusanagi mode active — full sibling dynamic enabled."
					}
					m.chat = m.chat.AddSystemMessage(msg)
					// Inject an LLM-visible directive so the model sees the identity
					// change in context (system messages are filtered from LLM calls).
					directive := fmt.Sprintf("[IDENTITY CHANGE] The person you are speaking with is now %s. "+
						"Disregard any prior name references in this conversation. Address them as %s from this point forward.",
						user.DisplayName(), user.DisplayName())
					m.chat = m.chat.AddHiddenUserMessage(directive)
				}
				return m, nil

			case "voice":
				cfg, err := config.Load()
				if err != nil {
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Failed to load config: %v", err))
					return m, nil
				}
				if len(cmd.Args) == 0 {
					// Show current voice config
					key := "not set"
					if cfg.ElevenLabsAPIKey != "" {
						if len(cfg.ElevenLabsAPIKey) > 8 {
							key = cfg.ElevenLabsAPIKey[:4] + "..." + cfg.ElevenLabsAPIKey[len(cfg.ElevenLabsAPIKey)-4:]
						} else {
							key = "***"
						}
					}
					voice := cfg.ElevenLabsVoiceID
					if voice == "" {
						voice = "not set"
					}
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("ElevenLabs TTS:\n  API Key: %s\n  Voice ID: %s\n\nUsage: /voice set-key <key> | /voice set-voice <id>", key, voice))
					return m, nil
				}
				sub := cmd.Args[0]
				switch sub {
				case "set-key":
					if len(cmd.Args) < 2 {
						m.chat = m.chat.AddSystemMessage("Usage: /voice set-key <elevenlabs-api-key>")
						return m, nil
					}
					cfg.ElevenLabsAPIKey = cmd.Args[1]
					if err := config.Save(cfg); err != nil {
						m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Failed to save: %v", err))
					} else {
						m.chat = m.chat.AddSystemMessage("ElevenLabs API key saved.")
					}
				case "set-voice":
					if len(cmd.Args) < 2 {
						m.chat = m.chat.AddSystemMessage("Usage: /voice set-voice <voice-id>")
						return m, nil
					}
					cfg.ElevenLabsVoiceID = cmd.Args[1]
					if err := config.Save(cfg); err != nil {
						m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Failed to save: %v", err))
					} else {
						m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Voice ID saved: %s", cmd.Args[1]))
					}
				case "list":
					apiKey := cfg.ElevenLabsAPIKey
					if apiKey == "" {
						apiKey = os.Getenv("ELEVEN_LABS_API_KEY")
					}
					if apiKey == "" {
						apiKey = os.Getenv("ELEVENLABS_API_KEY")
					}
					if apiKey == "" {
						apiKey = os.Getenv("ELEVEN_KEY")
					}
					if apiKey == "" {
						m.chat = m.chat.AddSystemMessage("No ElevenLabs API key configured.\nSet one with: /voice set-key <key>")
						return m, nil
					}
					m.chat = m.chat.AddSystemMessage("Fetching voices from ElevenLabs...")
					m.streaming = true
					m.status = m.status.SetStreaming(true)
					m.status = m.status.SetText("Fetching voices...")
					return m, func() tea.Msg {
						voices, err := fetchElevenLabsVoices(apiKey)
						if err != nil {
							return AgentProgressMsg{Kind: AgentProgressResponse, Text: fmt.Sprintf("Failed: %v", err)}
						}
						return AgentProgressMsg{Kind: AgentProgressResponse, Text: voices}
					}
				default:
					m.chat = m.chat.AddSystemMessage("Usage: /voice list | /voice set-key <key> | /voice set-voice <id>")
				}
				return m, nil

			case "confirm":
				cfg, err := config.Load()
				if err != nil {
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Failed to load config: %v", err))
					return m, nil
				}
				cfg.ConfirmActions = !cfg.ConfirmActions
				if saveErr := config.Save(cfg); saveErr != nil {
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Failed to save config: %v", saveErr))
				} else {
					// View() renders m.config's cached ConfirmActions instead
					// of reloading from disk (#144 W6b review, I1); keep it
					// current so the toggle takes effect immediately.
					m.config = cfg
					if refresher, ok := m.llmClient.(PromptRefresher); ok {
						refresher.RefreshSystemPrompt()
					}
					if cfg.ConfirmActions {
						m.chat = m.chat.AddSystemMessage("Confirm mode: ON — Celeste will propose actions before executing.")
					} else {
						m.chat = m.chat.AddSystemMessage("Confirm mode: OFF — Celeste auto-executes actions.")
					}
				}
				return m, nil

			case "agents":
				// /agents resume <checkpoint-id>
				if len(cmd.Args) >= 2 && strings.ToLower(cmd.Args[0]) == "resume" {
					checkpointID := cmd.Args[1]
					resumer, ok := m.llmClient.(SubagentResumer)
					if !ok {
						m.chat = m.chat.AddSystemMessage("Subagent resume not available.")
						return m, nil
					}
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Resuming subagent from checkpoint %s...", checkpointID))
					return m, func() tea.Msg {
						result, err := resumer.ResumeSubagent(context.Background(), checkpointID)
						if err != nil {
							return AgentProgressMsg{Kind: AgentProgressResponse, Text: fmt.Sprintf("Resume failed: %v", err)}
						}
						return AgentProgressMsg{Kind: AgentProgressResponse, Text: fmt.Sprintf("Resumed subagent completed.\n\n%s", result)}
					}
				}

				// /agents kill <id> — cancel a specific in-flight subagent (task 6ffb5a7c)
				if len(cmd.Args) >= 2 && strings.ToLower(cmd.Args[0]) == "kill" {
					id := cmd.Args[1]
					killer, ok := m.llmClient.(SubagentKiller)
					if !ok {
						m.chat = m.chat.AddSystemMessage("Subagent kill not available.")
						return m, nil
					}
					if killer.KillSubagent(id) {
						m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Killed subagent %s.", id))
					} else {
						m.chat = m.chat.AddSystemMessage(fmt.Sprintf("No in-flight subagent matching %q (already finished or unknown id).", id))
					}
					return m, nil
				}

				lister, ok := m.llmClient.(SubagentLister)
				if !ok {
					m.chat = m.chat.AddSystemMessage("Subagent listing not available.")
					return m, nil
				}
				agents := lister.ListSubagents()
				if len(agents) == 0 {
					m.chat = m.chat.AddSystemMessage("No subagents spawned this session.")
					return m, nil
				}
				var sb strings.Builder
				sb.WriteString("Subagents:\n")
				for _, a := range agents {
					icon := "◇"
					switch a.Status {
					case "running":
						icon = "▶"
					case "completed":
						icon = "✓"
					case "failed":
						icon = "✗"
					case "waiting":
						icon = "◇"
					}
					line := fmt.Sprintf("  %s 〔%s〕 %s  %d turns  %s",
						icon, a.Name, a.Status, a.Turns,
						a.Elapsed.Round(time.Millisecond))
					if a.ID != "" {
						line += fmt.Sprintf("  id:%s", a.ID)
					}
					if a.TaskID != "" {
						line += fmt.Sprintf("  (task: %s)", a.TaskID)
					}
					if a.Type != "" {
						line += "  [" + a.Type + "]"
					}
					sb.WriteString(line + "\n")
					if a.Summary != "" {
						sb.WriteString("      " + agentSummaryLine(a.Summary) + "\n")
					}
				}
				sb.WriteString("\nCancel one with: /agents kill <id|name>  (e.g. the 〔name〕 shown above)\n")
				m.chat = m.chat.AddSystemMessage(sb.String())
				return m, nil

			case "graph":
				if m.codeGraphIndexer == nil {
					m.chat = m.chat.AddSystemMessage("No code graph loaded.\nRun `celeste index` to build one, then relaunch.")
					return m, nil
				}
				m.viewMode = "graph"
				model := NewGraphModel(m.codeGraphIndexer)
				m.graphModel = &model
				return m, nil

			case "effort":
				validLevels := []string{"off", "low", "medium", "high", "max"}
				if len(cmd.Args) == 0 {
					m.chat = m.chat.AddSystemMessage("Usage: /effort <level>\nLevels: off, low, medium, high, max")
					return m, nil
				}
				level := strings.ToLower(cmd.Args[0])
				valid := false
				for _, l := range validLevels {
					if l == level {
						valid = true
						break
					}
				}
				if !valid {
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Invalid effort level: %s\nValid levels: off, low, medium, high, max", level))
					return m, nil
				}
				setter, ok := m.llmClient.(ThinkingConfigSetter)
				if !ok {
					m.chat = m.chat.AddSystemMessage("Extended thinking is not supported by the current client.")
					return m, nil
				}
				setter.SetThinkingLevel(level)
				m.effort = level
				m = m.syncStatusLine()
				if level == "off" {
					m.chat = m.chat.AddSystemMessage("Extended thinking disabled.")
				} else {
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Reasoning effort set to: %s", level))
				}
				return m, nil
			}

			// For other commands, use normal execution flow
			// Create context with current state (needed for model listing/validation)
			// Try to get config from LLMClient (available if it's the adapter from main.go)
			// If not available, commands will fall back to static model lists
			ctx := &commands.CommandContext{
				NSFWMode:      m.nsfwMode,
				Provider:      m.provider,
				CurrentModel:  m.model,
				SkillsEnabled: m.skillsEnabled,
				Version:       m.version,
				Build:         m.build,
			}
			// /set-model validates against the active endpoint's own
			// catalog (memory only), not whichever this provider loaded last.
			if src, ok := m.llmClient.(ActiveEndpointer); ok {
				ep := src.ActiveEndpoint()
				ctx.BaseURL, ctx.APIKey = ep.BaseURL, ep.APIKey
			}
			result := commands.Execute(cmd, ctx)

			// Show command result message if needed
			if result.ShouldRender {
				m.chat = m.chat.AddSystemMessage(result.Message)
			}

			// Apply state changes
			if result.StateChange != nil {
				var catalogCmd tea.Cmd
				if result.StateChange.EndpointChange != nil {
					m, catalogCmd = m.switchEndpoint(*result.StateChange.EndpointChange)
				}
				if result.StateChange.NSFWMode != nil {
					m.nsfwMode = *result.StateChange.NSFWMode
					m.header = m.header.SetNSFWMode(m.nsfwMode)

					// When NSFW mode is enabled, save current endpoint and switch to Venice
					if m.nsfwMode {
						// Save the current "safe" endpoint
						m.safeEndpoint = m.endpoint
						m.endpoint = "venice"
						m.header = m.header.SetEndpoint(m.endpoint)

						// Actually switch the LLM client to Venice
						if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
							if err := switcher.SwitchEndpoint(m.endpoint); err != nil {
								m.status = m.status.SetText(fmt.Sprintf("Error switching to Venice: %v", err))
							}
						}
						m.modelPinned = false // a --force pin belongs to the old endpoint
						m, catalogCmd = m.adoptActiveModel()
					} else {
						// When NSFW mode is disabled, restore the safe endpoint
						if m.safeEndpoint != "" {
							m.endpoint = m.safeEndpoint
						} else {
							// Fallback to default if no safe endpoint saved
							m.endpoint = "openai"
						}
						m.header = m.header.SetEndpoint(m.endpoint)

						// Actually switch the LLM client back
						if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
							if err := switcher.SwitchEndpoint(m.endpoint); err != nil {
								m.status = m.status.SetText(fmt.Sprintf("Error switching endpoint: %v", err))
							}
						}
						m.modelPinned = false // a --force pin belongs to the old endpoint
						m, catalogCmd = m.adoptActiveModel()
					}

					// Persist session state
					m.persistSession()
				}
				if result.StateChange.Model != nil {
					// A typed name is on trial until the provider confirms
					// it: a 404 restores this model instead of swapping in
					// another one.
					m.modelTrial, m.modelBeforeTrial = *result.StateChange.Model, m.model
					m.model = *result.StateChange.Model
					m.modelPinned = result.StateChange.PinModel
					m.header = m.header.SetModel(m.model)
					m.status = m.status.SetText(fmt.Sprintf("Model changed to %s", m.model))

					// A ToolsPerModel provider (Venice) only knows tool
					// support per model (#151 W6b).
					m.skillsEnabled = providers.ToolsEnabledForModel(m.provider, m.model)
					m.header = m.header.SetSkillsEnabled(m.skillsEnabled)

					// Actually change the model
					if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
						if err := switcher.ChangeModel(m.model); err != nil {
							m.status = m.status.SetText(fmt.Sprintf("Error changing model: %v", err))
						}
					}

					// Persist to config so it survives restarts
					if cfg, err := config.Load(); err == nil {
						cfg.Model = m.model
						_ = config.Save(cfg)
					}

					// Persist session state
					m.persistSession()

					// An unlisted name /set-model accepted is checked with
					// the provider off the UI loop (unless --force pinned it).
					var check tea.Cmd
					m, check = m.resolveServedModel()
					catalogCmd = tea.Batch(catalogCmd, check)
				}
				if result.StateChange.ImageModel != nil {
					m.imageModel = *result.StateChange.ImageModel
					m.header = m.header.SetImageModel(m.imageModel)
					m.status = m.status.SetText(fmt.Sprintf("🎨 Image model: %s", m.imageModel))

					// Persist session state
					m.persistSession()
				}
				if result.StateChange.ClearHistory {
					m.chat = m.chat.Clear()
				}
				if result.StateChange.NewSession {
					m = m.handleSessionAction(&commands.SessionAction{Action: "new"})
				}

				if result.StateChange.MenuState != nil {
					m.skills = m.skills.SetMenuState(*result.StateChange.MenuState)
				}

				// Handle session actions
				if result.StateChange.SessionAction != nil {
					m = m.handleSessionAction(result.StateChange.SessionAction)
					// Another session's history is a different conversation:
					// the prompt follows it (the picker does the same).
					if a := result.StateChange.SessionAction.Action; a == "resume" || a == "merge" {
						if refresher, ok := m.llmClient.(PromptRefresher); ok {
							refresher.RefreshSystemPrompt()
						}
					}
				}

				// Handle selector request
				if result.StateChange.ShowSelector != nil {
					// Convert commands.SelectorItem to tui.SelectorItem
					tuiItems := make([]SelectorItem, len(result.StateChange.ShowSelector.Items))
					for i, item := range result.StateChange.ShowSelector.Items {
						tuiItems[i] = SelectorItem{
							ID:          item.ID,
							DisplayName: item.DisplayName,
							Description: item.Description,
							Badge:       item.Badge,
						}
					}

					// Activate selector
					m.selector = NewSelectorModel(result.StateChange.ShowSelector.Title, tuiItems)
					m.selector = m.selector.SetHeight(m.height - 4)
					m.selector = m.selector.SetWidth(m.width)
					m.selectorActive = true
				}
				return m, catalogCmd
			}

			return m, nil
		}

		// Handle legacy text commands (for backward compatibility)
		lowerContent := strings.ToLower(content)
		switch lowerContent {
		case "exit", "quit", "q", ":q", ":quit", ":exit":
			m.persistSession()
			return m, tea.Quit
		case "clear":
			m.chat = m.chat.Clear()
			m.status = m.status.SetText("Chat cleared")
			return m, nil
		case "help":
			// Use context-aware /help command instead of static helpText()
			helpCmd := &commands.Command{Name: "help"}
			ctx := &commands.CommandContext{NSFWMode: m.nsfwMode}
			result := commands.Execute(helpCmd, ctx)
			if result.Success {
				m.chat = m.chat.AddSystemMessage(result.Message)
			}
			return m, nil
		case "tools", "skills":
			// Switch to interactive skills view
			m.viewMode = "skills"

			// Create skills browser with current skills list
			skillsList := []SkillDefinition{}
			if m.llmClient != nil {
				skillsList = m.llmClient.GetSkills()
			}
			model := NewSkillsBrowserModel(skillsList)
			model.width, model.height = m.width, m.height
			m.skillsBrowser = &model

			return m, m.skillsBrowser.Init()

		case "debug":
			// Show tools/skills debug info (old behavior for debug command)
			skills := m.getAvailableSkills()
			debugMsg := fmt.Sprintf("📋 Available Tools (%d):\n", len(skills))
			for _, s := range skills {
				debugMsg += fmt.Sprintf("  • %s: %s\n", s.Name, s.Description)
			}
			debugMsg += "\n⚠️  Note: DigitalOcean GenAI Agents may not support function calling.\n"
			debugMsg += "Tool calls only work with OpenAI-compatible APIs that support the 'tools' parameter.\n"
			debugMsg += fmt.Sprintf("\nLog file: %s", GetLogPath())
			m.chat = m.chat.AddSystemMessage(debugMsg)
			return m, nil
		}

		// Check for routing hints (hashtags or keywords at end)
		suggestedEndpoint := commands.DetectRoutingHints(content)
		if suggestedEndpoint != "" && suggestedEndpoint != m.endpoint {
			// Auto-route based on hints
			m.endpoint = suggestedEndpoint
			m.header = m.header.SetEndpoint(m.endpoint)
			m.header = m.header.SetAutoRouted(true)
			m.status = m.status.SetText(fmt.Sprintf("🔀 Auto-routed to %s", suggestedEndpoint))

			// Actually switch the LLM client endpoint
			if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
				if err := switcher.SwitchEndpoint(m.endpoint); err != nil {
					m.status = m.status.SetText(fmt.Sprintf("Error auto-routing: %v", err))
				}
			}

			// Persist session state
			m.persistSession()
		} else {
			m.header = m.header.SetAutoRouted(false)
		}

		// Check for Venice media commands in NSFW mode
		if m.nsfwMode {
			LogInfo(fmt.Sprintf("Checking for media command in: '%s'", content))
			mediaType, prompt, params, isMediaCmd := venice.ParseMediaCommand(content)
			LogInfo(fmt.Sprintf("ParseMediaCommand result: isMediaCmd=%v, mediaType=%s, prompt='%s'", isMediaCmd, mediaType, prompt))

			if isMediaCmd {
				// Handle media generation directly (bypass LLM)
				LogInfo(fmt.Sprintf("✓ Detected %s media command, bypassing LLM", mediaType))
				m.chat = m.chat.AddUserMessage(content)
				m.chat = m.chat.AddAssistantMessage(fmt.Sprintf("🎨 Generating %s... please wait", mediaType))
				m.status = m.status.SetText(fmt.Sprintf("⏳ Venice.ai %s generation in progress...", mediaType))

				// Add messages to session for persistence
				if m.currentSession != nil {
					if configSession, ok := m.currentSession.(*config.Session); ok {
						configSession.Messages = append(configSession.Messages, config.SessionMessage{
							Role:      "user",
							Content:   content,
							Timestamp: time.Now(),
						})
						configSession.Messages = append(configSession.Messages, config.SessionMessage{
							Role:      "assistant",
							Content:   fmt.Sprintf("🎨 Generating %s... please wait", mediaType),
							Timestamp: time.Now(),
						})
					}
				}

				// Persist media generation request
				m.persistSession()

				// Trigger async media generation
				cmds = append(cmds, func() tea.Msg {
					return GenerateMediaMsg{
						MediaType:  mediaType,
						Prompt:     prompt,
						Params:     params,
						ImageModel: m.imageModel, // Pass current image model from app state
					}
				})
				return m, tea.Batch(cmds...)
			} else {
				LogInfo("No media command detected, sending to LLM chat")
			}
		}

		// Add user message to chat
		m.chat = m.chat.AddUserMessage(content)
		m.streaming = true
		m.status = m.status.SetStreaming(true)
		m.status = m.status.SetText(StreamingSpinner(0) + " " + ThinkingAnimation(0))

		// Add user message to session for persistence
		if m.currentSession != nil {
			if configSession, ok := m.currentSession.(*config.Session); ok {
				configSession.Messages = append(configSession.Messages, config.SessionMessage{
					Role:      "user",
					Content:   content,
					Timestamp: time.Now(),
				})
			}
		}

		// Persist user message immediately (in case of crash before response)
		m.persistSession()

		// Record when we started waiting for this response.
		m.streamStart = time.Now()
		m.lastMsgInTok = 0
		m.lastMsgOutTok = 0

		// Send to the loop (2.0 F2d). It checks the prompt with
		// UserPromptSubmit, prunes before every request and answers a
		// context overflow itself.
		var turnCmd tea.Cmd
		m, turnCmd = m.startTurn()
		cmds = append(cmds, turnCmd)
		if m.turn != nil {
			// The spinner animates from Enter, also while a
			// UserPromptSubmit hook runs before the first request.
			cmds = append(cmds, m.restartTick(typingTickInterval*2))
		}

	case GenerateMediaMsg:
		// Generate media asynchronously via Venice.ai
		LogInfo(fmt.Sprintf("→ Starting %s generation with prompt: '%s'", msg.MediaType, msg.Prompt))
		cmds = append(cmds, func() tea.Msg {
			// Load Venice config from skills.json
			LogInfo("Loading Venice config from skills.json")
			veniceConfig, err := loadVeniceConfig()
			if err != nil {
				LogInfo(fmt.Sprintf("❌ Failed to load Venice config: %v", err))
				return MediaResultMsg{
					Success:   false,
					Error:     fmt.Sprintf("Failed to load Venice config: %v", err),
					MediaType: msg.MediaType,
				}
			}
			LogInfo(fmt.Sprintf("✓ Loaded Venice config: baseURL=%s, imageModel=%s", veniceConfig.BaseURL, veniceConfig.ImageModel))

			// Create config with appropriate model for the media type
			modelToUse := veniceConfig.Model // Default to chat model
			if msg.MediaType == "image" {
				// Use app's image model if set, otherwise fall back to config
				if msg.ImageModel != "" {
					modelToUse = msg.ImageModel
					LogInfo(fmt.Sprintf("Using app image model: %s", modelToUse))
				} else {
					modelToUse = veniceConfig.ImageModel
					LogInfo(fmt.Sprintf("Using config image model: %s", modelToUse))
				}
			}
			LogInfo(fmt.Sprintf("Using model: %s for %s generation", modelToUse, msg.MediaType))

			config := venice.Config{
				APIKey:  veniceConfig.APIKey,
				BaseURL: veniceConfig.BaseURL,
				Model:   modelToUse,
			}

			var response *venice.MediaResponse
			var genErr error

			LogInfo(fmt.Sprintf("Calling Venice.ai API for %s generation", msg.MediaType))
			switch msg.MediaType {
			case "image":
				response, genErr = venice.GenerateImage(config, msg.Prompt, msg.Params)
			case "video":
				response, genErr = venice.GenerateVideo(config, msg.Prompt, msg.Params)
			case "image-to-video":
				if path, ok := msg.Params["path"].(string); ok {
					response, genErr = venice.ImageToVideo(config, path, msg.Params)
				} else {
					genErr = fmt.Errorf("no image path provided for image-to-video")
				}
			default:
				genErr = fmt.Errorf("unknown media type: %s", msg.MediaType)
			}

			if genErr != nil {
				LogInfo(fmt.Sprintf("❌ Media generation error: %v", genErr))
				return MediaResultMsg{
					Success:   false,
					Error:     genErr.Error(),
					MediaType: msg.MediaType,
				}
			}

			if response == nil {
				LogInfo("❌ Response is nil")
				return MediaResultMsg{
					Success:   false,
					Error:     "No response from Venice API",
					MediaType: msg.MediaType,
				}
			}

			if !response.Success {
				LogInfo(fmt.Sprintf("❌ Response failed: %s", response.Error))
				return MediaResultMsg{
					Success:   false,
					Error:     response.Error,
					MediaType: msg.MediaType,
				}
			}

			LogInfo(fmt.Sprintf("✓ Media generation successful: URL=%s, Path=%s", response.URL, response.Path))
			return MediaResultMsg{
				Success:   true,
				URL:       response.URL,
				Path:      response.Path,
				MediaType: msg.MediaType,
			}
		})

	case MediaResultMsg:
		// Handle media generation result
		LogInfo(fmt.Sprintf("Received MediaResultMsg: success=%v, mediaType=%s", msg.Success, msg.MediaType))
		if msg.Success {
			var resultText string
			if msg.URL != "" {
				LogInfo(fmt.Sprintf("✓ Media generation SUCCESS: URL=%s", msg.URL))
				resultText = fmt.Sprintf("✅ %s generated successfully!\n\n🔗 URL: %s", msg.MediaType, msg.URL)
			} else if msg.Path != "" {
				LogInfo(fmt.Sprintf("✓ Media generation SUCCESS: Path=%s", msg.Path))
				resultText = fmt.Sprintf("✅ %s generated successfully!\n\n💾 Saved to: %s", msg.MediaType, msg.Path)
			} else {
				LogInfo("✓ Media generation SUCCESS (no URL/Path)")
				resultText = fmt.Sprintf("✅ %s generated successfully!", msg.MediaType)
			}

			// Update the last assistant message with the result
			m.chat = m.chat.SetLastAssistantContent(resultText)
			m.status = m.status.SetText(fmt.Sprintf("✓ %s complete", msg.MediaType))

			// Persist media generation result
			m.persistSession()
		} else {
			LogInfo(fmt.Sprintf("✗ Media generation FAILED: %s", msg.Error))
			errorText := fmt.Sprintf("❌ %s generation failed: %s", msg.MediaType, msg.Error)
			m.chat = m.chat.SetLastAssistantContent(errorText)
			m.status = m.status.SetText(fmt.Sprintf("✗ %s failed", msg.MediaType))

			// Persist error message
			m.persistSession()
		}
		m.streaming = false
		m.status = m.status.SetStreaming(false)

	case StreamChunkMsg:
		var more []tea.Cmd
		m, more = m.onStreamChunk(msg)
		cmds = append(cmds, more...)

	case StreamStartMsg:
		if msg.AgentRun != 0 && !m.agentRunCurrent(msg.AgentRun) {
			// A cancelled /agent run's late start: stop it, and leave the
			// current run's cancel alone (2.0 F2e).
			if msg.Cancel != nil {
				msg.Cancel()
			}
			return m, nil
		}
		if msg.Run != 0 && msg.Run != m.orchRun {
			// An /orch run cancelled (Esc, Ctrl+C) before its cancel
			// arrived: stop it, and leave the current turn's cancel alone.
			if msg.Cancel != nil {
				msg.Cancel()
			}
			return m, nil
		}
		// Store the cancel function so Ctrl+C can cancel the active request
		m.cancelFunc = msg.Cancel
		return m, nil

	case StreamDoneMsg:
		var more []tea.Cmd
		m, more = m.onStreamDone(msg)
		cmds = append(cmds, more...)

	case StreamErrorMsg:
		m.cancelFunc = nil
		m.interruptPending = false
		m.streaming = false
		m.status = m.status.SetStreaming(false)
		m.status = m.status.SetText(fmt.Sprintf("Error: %v", msg.Err))
		m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Error: %v", msg.Err))

	case HookWarningMsg:
		m.chat = m.chat.AddSystemMessage("⚠ " + msg.Text)
		return m, nil

	case PersonaNoticeMsg:
		m.chat = m.chat.AddSystemMessage(msg.Text)
		return m, nil

	case ToolProgressMsg:
		var cmd tea.Cmd
		m.toolProgress, cmd = m.toolProgress.Update(msg)
		cmds = append(cmds, cmd)

	case ContextSummarizedMsg:
		if m.turn != nil {
			// The loop's snapshots line up with the chat position by
			// position; applying a summary now would shift them. It waits
			// for the turn to end (onTurnDone re-validates it).
			held := msg
			m.heldSummary = &held
			break
		}
		m, _ = m.applySummary(msg)

	case HandoffReadyMsg:
		m = m.applyHandoff(msg)

	case ContextBudgetMsg:
		var cmd tea.Cmd
		m.contextBar, cmd = m.contextBar.Update(msg)
		cmds = append(cmds, cmd)

	case PermissionRequestMsg:
		owner, live := m.requestOwner(msg.Owner, msg.Done)
		if !live {
			// Its run ended before the request arrived: deny, never show.
			PermissionPromptModel{response: msg.Response}.Dismiss()
			break
		}
		var cmd tea.Cmd
		m.permissionPrompt, cmd = m.permissionPrompt.Update(msg)
		m.permissionOwner = owner
		cmds = append(cmds, cmd)

	case AskRequestMsg:
		owner, live := m.requestOwner(msg.Owner, msg.Done)
		if !live {
			AskPromptModel{response: msg.Response}.Dismiss()
			break
		}
		var cmd tea.Cmd
		m.askPrompt, cmd = m.askPrompt.Update(msg)
		m.askOwner = owner
		cmds = append(cmds, cmd)

	case GitStatusMsg:
		if msg.Repo {
			m.statusLine = m.statusLine.SetGit(msg.Branch, msg.Dirty, msg.Ahead, msg.Behind)
		} else {
			m.statusLine = m.statusLine.SetGit("", 0, 0, 0)
		}
		m = m.syncStatusLine()
		return m, gitPollCmd(m.workDir) // re-arm the poll

	case MCPConnectResultMsg:
		if msg.Err != nil {
			m.chat = m.chat.AddSystemMessage(fmt.Sprintf("MCP %s: %v", msg.Name, msg.Err))
		}
		m.mcpPanel = m.mcpPanel.RefreshServers()
		return m, nil

	case AgentProgressMsg:
		if msg.AgentRun != 0 && !m.agentRunCurrent(msg.AgentRun) {
			// A cancelled /agent run still winding down: its events belong
			// to no run on screen. Its sender never blocks, so its chain is
			// simply not read further (2.0 F2e).
			return m, nil
		}
		var cmds []tea.Cmd

		switch msg.Kind {
		case AgentProgressTurnStart:
			m.streaming = true
			m.status = m.status.SetStreaming(true)
			// Reset per-turn timing so the TickMsg "typing complete" handler
			// measures only this turn's API latency.
			m.streamStart = time.Now()
			m.lastMsgInTok = 0
			m.lastMsgOutTok = 0
			// Initialise run-level accumulators on the first turn.
			if msg.Turn <= 1 {
				m.agentInputTokens = 0
				m.agentOutputTokens = 0
				m.agentRunStart = time.Now()
			}
			turnLabel := "Agent"
			if msg.MaxTurns > 0 {
				turnLabel = fmt.Sprintf("Agent: turn %d/%d", msg.Turn, msg.MaxTurns)
			}
			if msg.Text != "" {
				turnLabel += " · " + msg.Text
			}
			m.status = m.status.SetText(turnLabel)
			// Visible turn separator in the chat history for full traceability.
			if msg.Turn > 0 {
				sep := fmt.Sprintf("── turn %d/%d ──", msg.Turn, msg.MaxTurns)
				m.chat = m.chat.AddSystemMessage(sep)
			}

		case AgentProgressToolCall:
			entry := fmt.Sprintf("⚙  %s", msg.Text)
			// First tool call of each turn carries per-turn stats.
			if msg.InputTokens > 0 || msg.Duration > 0 {
				entry += " " + formatOrchestratorStats(msg.Duration, msg.InputTokens, msg.OutputTokens)
				m.lastMsgInTok = msg.InputTokens
				m.lastMsgOutTok = msg.OutputTokens
				m.agentInputTokens += msg.InputTokens
				m.agentOutputTokens += msg.OutputTokens
			}
			m.status = m.status.SetText(fmt.Sprintf("Agent: calling %s", msg.Text))
			m.chat = m.chat.AddSystemMessage(entry)

		case AgentProgressStepDone:
			m.chat = m.chat.AddSystemMessage(fmt.Sprintf("✓ %s", msg.Text))

		case AgentProgressResponse:
			// Capture per-turn stats so the TickMsg "typing complete" path
			// displays timing + tokens in the status bar — same as regular chat.
			if msg.InputTokens > 0 || msg.OutputTokens > 0 {
				m.lastMsgInTok = msg.InputTokens
				m.lastMsgOutTok = msg.OutputTokens
				m.agentInputTokens += msg.InputTokens
				m.agentOutputTokens += msg.OutputTokens
			}
			if msg.Duration > 0 {
				// Back-date streamStart so elapsed == API latency, not wall-clock
				// time since turn start (which includes tool execution time).
				m.streamStart = time.Now().Add(-msg.Duration)
			}
			// Feed the response through the typing animation — same path as regular chat.
			// Agent responses are not streamed (the whole text arrives in one
			// msg.Text), so mark streamDone=true up front: the TickMsg
			// tick-complete branch will commit as soon as typing catches up
			// instead of idling forever waiting for a StreamDoneMsg that the
			// agent path never sends.
			if strings.TrimSpace(msg.Text) != "" {
				m.typingContent = msg.Text
				m.typingPos = 0
				m.streamDone = true
				m.chat = m.chat.AddAssistantMessage("")
				m.status = m.status.SetText("Agent: typing response...")
				cmds = append(cmds, m.tick(typingTickInterval))
			}

		case AgentProgressComplete:
			m = m.endAgentRun()
			m.cancelFunc = nil
			m.streaming = false
			m.status = m.status.SetStreaming(false)
			// Show run-level summary in status bar — total time + total tokens.
			totalElapsed := time.Since(m.agentRunStart)
			summary := formatOrchestratorStats(totalElapsed, m.agentInputTokens, m.agentOutputTokens)
			if summary != "" {
				m.status = m.status.SetText("Agent complete " + summary)
			} else {
				m.status = m.status.SetText("Agent complete")
			}
			m.persistSession()

		case AgentProgressError:
			m = m.endAgentRun()
			m.cancelFunc = nil
			m.streaming = false
			m.status = m.status.SetStreaming(false)
			m.status = m.status.SetText(fmt.Sprintf("Agent error: %s", msg.Text))
			if strings.TrimSpace(msg.Text) != "" {
				m.chat = m.chat.AddSystemMessage(fmt.Sprintf("❌ Agent error: %s", msg.Text))
			}
			m.persistSession()
		}

		// If there are more messages in the channel, schedule reading the next one.
		if next := msg.ReadNext(); next != nil {
			cmds = append(cmds, next)
		}

		return m, tea.Batch(cmds...)

	case OrchestratorEventMsg:
		if msg.Run != m.orchRun {
			// From a run that is no longer current (cancelled): keep
			// draining it so its goroutine can finish, change nothing.
			return m, msg.ReadNext()
		}
		var cmds []tea.Cmd

		if m.splitPanel == nil {
			panelH := m.height - 5 // header(1) + status(1) + input(3)
			if panelH < 5 {
				panelH = 5
			}
			m.splitPanel = NewSplitPanel(m.width, panelH)
		}
		m.splitPanelMode = true

		// EventKind constants (mirror orchestrator package without import cycle):
		// 0=Classified 1=Action 2=ToolCall 3=FileDiff 4=ReviewDraft 5=Defense 6=Verdict 7=Complete 8=Error 9=DebateStart
		switch msg.Kind {
		case 0: // EventClassified
			m.splitPanel.AddAction(fmt.Sprintf("── %s · %s ──", msg.Lane, msg.Text))
			m.status = m.status.SetText(fmt.Sprintf("Orchestrator: [%s] %s", msg.Lane, msg.Text))
			m.streaming = true
			m.status = m.status.SetStreaming(true)
		case 1: // EventAction
			m.orchInputTokens += msg.InputTokens
			m.orchOutputTokens += msg.OutputTokens
			entry := msg.Text
			if msg.Model != "" {
				entry = fmt.Sprintf("[%s] %s", msg.Model, entry)
			}
			if msg.Duration > 0 || msg.InputTokens > 0 {
				entry += " " + formatOrchestratorStats(msg.Duration, msg.InputTokens, msg.OutputTokens)
			}
			m.splitPanel.AddAction(entry)
			if msg.Response != "" {
				m.splitPanel.SetOutput(msg.Response)
			}
			statusText := fmt.Sprintf("Orchestrator: %s", msg.Text)
			if m.orchInputTokens > 0 {
				statusText += fmt.Sprintf(" · ↑%s ↓%s total", formatOrchestratorTokens(m.orchInputTokens), formatOrchestratorTokens(m.orchOutputTokens))
			}
			m.status = m.status.SetText(statusText)
		case 2: // EventToolCall
			m.splitPanel.AddAction(msg.Text)
			m.splitPanel.AppendOutput(msg.Text + "\n")
			m.status = m.status.SetText(fmt.Sprintf("Orchestrator: %s", msg.Text))
		case 3: // EventFileDiff
			m.splitPanel.AddAction(fmt.Sprintf("wrote %s", msg.FilePath))
			if msg.FilePath != "" {
				m.splitPanel.SetDiff(msg.FilePath, msg.Diff)
			}
		case 4: // EventReviewDraft
			m.orchInputTokens += msg.InputTokens
			m.orchOutputTokens += msg.OutputTokens
			reviewer := "reviewer"
			if msg.Model != "" {
				reviewer = msg.Model
			}
			entry := fmt.Sprintf("🔍 [%s] %s", reviewer, truncateText(msg.Text, 60))
			if msg.Duration > 0 || msg.InputTokens > 0 {
				entry += " " + formatOrchestratorStats(msg.Duration, msg.InputTokens, msg.OutputTokens)
			}
			m.splitPanel.AddAction(entry)
			if msg.Response != "" {
				m.splitPanel.SetOutput("=== REVIEW: " + reviewer + " ===\n\n" + msg.Response)
			}
		case 5: // EventDefense
			m.orchInputTokens += msg.InputTokens
			m.orchOutputTokens += msg.OutputTokens
			model := "primary"
			if msg.Model != "" {
				model = msg.Model
			}
			entry := fmt.Sprintf("🛡 [%s] %s", model, truncateText(msg.Text, 60))
			if msg.Duration > 0 || msg.InputTokens > 0 {
				entry += " " + formatOrchestratorStats(msg.Duration, msg.InputTokens, msg.OutputTokens)
			}
			m.splitPanel.AddAction(entry)
			if msg.Response != "" {
				m.splitPanel.SetOutput("=== DEFENSE: " + model + " ===\n\n" + msg.Response)
			}
		case 6: // EventVerdict
			icon := "✓"
			if msg.Text == "contested" || msg.Text == "needs_work" {
				icon = "✗"
			}
			m.splitPanel.AddAction(fmt.Sprintf("%s verdict: %s (%.2f)", icon, msg.Text, msg.Score))
			m.splitPanel.SetVerdict(fmt.Sprintf("%s %s\nscore: %.2f", icon, msg.Text, msg.Score))
		case 9: // EventDebateStart
			label := fmt.Sprintf("── debate %s", msg.Text)
			reviewer := msg.Model
			if reviewer == "" {
				reviewer = "reviewer"
			}
			if msg.Model != "" {
				label = fmt.Sprintf("── debate %s · [%s] reviewing ──", msg.Text, msg.Model)
			}
			m.splitPanel.AddAction(label)
			m.splitPanel.SetOutput("=== REVIEWING: " + reviewer + " ===\n\n")
		case 7: // EventComplete
			m = m.closeModalsOf(RunOwner{Kind: OwnerOrch, Run: msg.Run})
			m.orchRun = 0
			m.cancelFunc = nil
			m.streaming = false
			// Keep splitPanelMode = true so results stay visible; user closes by sending next message
			m.status = m.status.SetStreaming(false)
			m.status = m.status.SetText("Orchestrator: complete — send a message to return to chat")
			if msg.Text != "" && m.splitPanel != nil {
				// Show primary response in right panel (lane label as header)
				label := "agent output"
				if msg.Lane != "" {
					label = msg.Lane + " output"
				}
				m.splitPanel.SetDiff(label, msg.Text)
			}
			m.persistSession()
		case 8: // EventError
			m = m.closeModalsOf(RunOwner{Kind: OwnerOrch, Run: msg.Run})
			m.orchRun = 0
			m.cancelFunc = nil
			m.streaming = false
			m.splitPanelMode = false
			m.status = m.status.SetStreaming(false)
			m.status = m.status.SetText(fmt.Sprintf("Orchestrator error: %s", msg.Text))
			m.chat = m.chat.AddSystemMessage(fmt.Sprintf("❌ %s", msg.Text))
			m.persistSession()
		}

		if next := msg.ReadNext(); next != nil {
			cmds = append(cmds, next)
		}
		return m, tea.Batch(cmds...)

	case AgentCommandResultMsg:
		if msg.AgentRun != 0 && !m.agentRunCurrent(msg.AgentRun) {
			return m, nil // an interrupted or older /agent run's result (2.0 F2e)
		}
		m = m.endAgentRun()
		m.streaming = false
		m.status = m.status.SetStreaming(false)

		if strings.TrimSpace(msg.Output) != "" {
			m.chat = m.chat.AddSystemMessage(msg.Output)
		}

		if msg.Err != nil {
			m.status = m.status.SetText(fmt.Sprintf("Agent error: %v", msg.Err))
			if strings.TrimSpace(msg.Output) == "" {
				m.chat = m.chat.AddSystemMessage(fmt.Sprintf("❌ Agent error: %v", msg.Err))
			}
		} else {
			m.status = m.status.SetText("Agent run complete")
		}

		m.persistSession()

	case SelectorResultMsg:
		// Handle selector result
		m.selectorActive = false

		if msg.Cancelled {
			// User cancelled - show cancellation message
			m.chat = m.chat.AddSystemMessage("Selection cancelled")
			m.status = m.status.SetText("Selection cancelled")
		} else if msg.Selected != nil {
			// User selected an item - trigger model change
			modelName := msg.Selected.ID

			// Use the switcher interface to change model
			if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
				if err := switcher.ChangeModel(modelName); err != nil {
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("❌ Failed to change model: %v", err))
					m.status = m.status.SetText(fmt.Sprintf("Error: %v", err))
				} else {
					m.model = modelName
					m.header = m.header.SetModel(modelName)

					// Check provider capabilities for the current provider.
					// A ToolsPerModel provider (Venice) decides per model, so
					// this can turn skills on as well as off (#151 W6b).
					if m.provider != "" {
						if _, ok := providers.GetProvider(m.provider); ok {
							enabled := providers.ToolsEnabledForModel(m.provider, modelName)
							if !enabled && m.skillsEnabled {
								m.chat = m.chat.AddSystemMessage(fmt.Sprintf("⚠️ Warning: model '%s' does not support function calling. Skills will be unavailable.", modelName))
							}
							m.skillsEnabled = enabled
							m.header = m.header.SetSkillsEnabled(enabled)
						}
					}

					m.modelPinned = false // picked from the provider's list
					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("🤖 Model changed to: %s", modelName))
					m.status = m.status.SetText(fmt.Sprintf("Model changed to: %s", modelName))

					// Persist to config so it survives restarts
					if cfg, err := config.Load(); err == nil {
						cfg.Model = modelName
						_ = config.Save(cfg)
					}

					// Persist the change to session
					m.persistSession()
				}
			}
		}

	case TickMsg:
		m.animFrame++

		// Handle simulated typing. Once the stream is done, enter even when
		// typing has caught up, so a reply typed out before the stream
		// closed still reaches the commit branch below.
		if m.typingContent != "" && (m.typingPos < len(m.typingContent) || m.streamDone) {
			// Advance typing position
			m.typingPos += m.typingStep()
			if m.typingPos > len(m.typingContent) {
				m.typingPos = len(m.typingContent)
			}

			// Update chat with typed content + fixed-width corruption buffer.
			// Pattern from celeste-tts-bot TypingTextReveal: revealed text
			// on the left, flickering corruption buffer on the right, buffer
			// always padded to a fixed width so the viewport never reflows.
			// Glamour is skipped for this message (typingActive flag) so the
			// ANSI styling in the buffer doesn't break markdown rendering.
			displayed := m.typingContent[:m.typingPos]
			if m.typingPos < len(m.typingContent) {
				displayed += " " + GetFixedWidthCorruption(16)
			}
			m.chat = m.chat.SetLastAssistantContent(displayed)

			// Show corruption phrases in the status bar instead of in the content
			m.status = m.status.SetText(StreamingSpinner(m.animFrame) + " " + ThinkingAnimation(m.animFrame))

			if m.typingPos < len(m.typingContent) {
				// More content to display — reschedule the typing tick.
				cmds = append(cmds, m.tick(typingTickInterval))
			} else if !m.streamDone {
				// Typing caught up to the end of the current buffer, but the
				// network stream is still in flight — a late chunk may extend
				// typingContent at any moment. Keep the ticker alive in an
				// idle state so the next tick picks up any extension. Do NOT
				// commit the content to session history yet; that's what
				// caused the v1.9.0 "O" truncation bug. See the streamDone
				// field doc on AppModel for the full story.
				cmds = append(cmds, m.tick(typingTickInterval))
			} else {
				// Typing complete and stream is done — show final content
				// without corruption and commit to session history.
				// Re-enable Glamour BEFORE setting final content so
				// updateContent() renders markdown on this pass.
				m.chat = m.chat.SetTypingActive(false)
				m.chat = m.chat.SetLastAssistantContent(m.typingContent)

				// Add assistant message to session for persistence
				if m.currentSession != nil {
					if configSession, ok := m.currentSession.(*config.Session); ok {
						configSession.Messages = append(configSession.Messages, config.SessionMessage{
							Role:      "assistant",
							Content:   m.typingContent,
							Timestamp: time.Now(),
						})
					}
				}

				typedContent := m.typingContent
				m.typingContent = ""
				m.typingPos = 0
				m.streaming = false
				m.streamDone = false
				m.status = m.status.SetStreaming(false)
				elapsed := time.Since(m.streamStart)
				inTok, outTok := m.lastMsgInTok, m.lastMsgOutTok
				isInferred := inTok == 0 && outTok == 0
				if isInferred {
					// API did not return token counts — estimate from response length.
					outTok = config.EstimateTokens(typedContent)
					if m.contextTracker != nil && m.contextTracker.CurrentTokens > 0 {
						inTok = m.contextTracker.CurrentTokens
					}
				}
				var statsStr string
				if isInferred && (inTok > 0 || outTok > 0) {
					statsStr = fmt.Sprintf("(%.1fs · ~↑%s ~↓%s)",
						elapsed.Seconds(),
						formatOrchestratorTokens(inTok),
						formatOrchestratorTokens(outTok))
				} else {
					statsStr = formatOrchestratorStats(elapsed, inTok, outTok)
				}
				if statsStr != "" {
					m.status = m.status.SetText("Ready " + statsStr)
				} else {
					m.status = m.status.SetText("Ready")
				}
				if m.stopHookRunning {
					// The turn is not over yet: shown when it ends.
					m.heldReady = m.status.text
					m.status = m.status.SetText(stopHookStatus)
				}

				// Clear completed tool progress entries now that the
				// response is fully rendered — no need to keep them
				// cluttering the bottom of the screen.
				m.toolProgress.ClearCompleted()

				// Persist session now that the message is complete
				m.persistSession()
			}
		} else if m.streaming {
			// Just streaming (waiting for response) - show animated status
			if !m.planning {
				m.status = m.status.SetText(StreamingSpinner(m.animFrame) + " " + ThinkingAnimation(m.animFrame))
			}
			cmds = append(cmds, m.tick(typingTickInterval*2))
		}

	case ErrorMsg:
		m.status = m.status.SetText(fmt.Sprintf("Error: %v", msg.Err))
	}

	// Keep ticks alive while tool progress entries are active so spinners
	// animate and elapsed timers update without a keypress. m.tick schedules
	// nothing while the chain's next tick is pending, so this never starts
	// a second chain (2.0 F2e).
	if m.toolProgress.HasActive() {
		cmds = append(cmds, m.tick(typingTickInterval*2))
	}

	return m, tea.Batch(cmds...)
}

// View implements tea.Model.
func (m AppModel) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	// If selector is active, show it full-screen
	if m.selectorActive {
		return m.selector.View()
	}

	// Show collections view if in that mode
	if m.viewMode == "collections" && m.collectionsModel != nil {
		return m.collectionsModel.View()
	}

	// Show menu view if in that mode
	if m.viewMode == "menu" && m.menuModel != nil {
		return m.menuModel.View()
	}

	// Show skills view if in that mode
	if m.viewMode == "skills" && m.skillsBrowser != nil {
		return m.skillsBrowser.View()
	}

	// Show persona panel if in that mode
	if m.viewMode == "persona" && m.personaPanel != nil {
		return m.personaPanel.View()
	}

	// Show graph view if in that mode
	if m.viewMode == "graph" && m.graphModel != nil {
		return m.graphModel.View()
	}

	// Show memory manager if in that mode
	if m.viewMode == "memories" && m.memoryManager != nil {
		return m.memoryManager.View()
	}

	if m.splitPanelMode && m.splitPanel != nil {
		// A /orch lane can be waiting on the permission modal or the ask
		// tool, and Update routes keys to them in this mode too: render them
		// here, or the run waits on a prompt nobody can see. The panel gives
		// up the rows they take.
		var modals []string
		if m.permissionPrompt.Active() {
			modals = append(modals, m.permissionPrompt.View())
		}
		if m.askPrompt.Active() {
			modals = append(modals, m.askPrompt.View())
		}
		panel := *m.splitPanel
		for _, v := range modals {
			panel.height -= lipgloss.Height(v)
		}
		if panel.height < 5 {
			panel.height = 5
		}
		sections := append([]string{m.header.View(), panel.View()}, modals...)
		sections = append(sections, m.status.View(), m.input.View())
		return lipgloss.JoinVertical(lipgloss.Left, sections...)
	}

	// Build the layout vertically
	var sections []string

	// Header (fixed, 1 line)
	sections = append(sections, m.header.View())

	// Chat panel (flexible height) — or session picker when in sessions mode
	if m.viewMode == "sessions" && m.sessionPanel != nil {
		sections = append(sections, m.sessionPanel.View())
	} else {
		sections = append(sections, m.chat.View())
	}

	// Tool progress cards (if any tools are executing)
	if m.toolProgress.HasActive() {
		sections = append(sections, m.toolProgress.View())
	}

	// Context budget bar (if token budget is known)
	if m.contextBar.maxTokens > 0 {
		sections = append(sections, m.contextBar.View())
	}

	// Permission prompt overlay (if waiting for user approval)
	if m.permissionPrompt.Active() {
		sections = append(sections, m.permissionPrompt.View())
	}

	// Ask prompt overlay (if waiting for the user to answer a question)
	if m.askPrompt.Active() {
		sections = append(sections, m.askPrompt.View())
	}

	// MCP server status panel (if active via /mcp)
	if m.mcpPanel.Active() {
		sections = append(sections, m.mcpPanel.View())
	}

	// Input panel (fixed, 3 lines)
	sections = append(sections, m.input.View())

	// Skills panel (fixed, 5 lines) - update config before rendering
	// Calculate skills count and disabled reason
	skillsCount := len(m.getAvailableSkills())
	disabledReason := ""
	if !m.skillsEnabled {
		if m.nsfwMode {
			disabledReason = "NSFW Mode - Venice doesn't support tools"
		} else {
			disabledReason = "Current model doesn't support function calling"
		}
	}

	m.skills = m.skills.SetConfig(m.endpoint, m.model, m.skillsEnabled, m.nsfwMode, skillsCount, disabledReason)
	// The model already caches the active config (m.config); reading it here
	// avoids a config.Load() disk read on every render (#144 W6b review, I1).
	if m.config != nil {
		m.skills.confirmMode = m.config.ConfirmActions
	}
	// Collapsed skills panel renders only active-skill signal; skip when empty
	// so the chat area reclaims the row.
	if sv := m.skills.View(); sv != "" {
		sections = append(sections, sv)
	}

	// Segmented status line (git / project / model / effort / perms / session / skills)
	sections = append(sections, m.statusLine.View())

	// Contextual key hints
	hints := hintsFor(m.viewMode, m.mcpPanel.Active())
	if m.viewMode == "chat" && !m.mcpPanel.Active() && m.turnActive() {
		hints = turnHints
	}
	sections = append(sections, HeaderInfoStyle.Render(" "+hints))

	// Status bar (fixed, 1 line)
	sections = append(sections, m.status.View())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (m AppModel) getAvailableSkills() []SkillDefinition {
	if m.llmClient == nil {
		return nil
	}
	return m.llmClient.GetSkills()
}

// SessionManager interface for session persistence (avoid circular import).
// Uses interface{} for return types to avoid circular dependencies.
type SessionManager interface {
	NewSession() interface{}
	Save(session interface{}) error
	Load(id string) (interface{}, error)
	List() ([]interface{}, error)
	Delete(id string) error
	MergeSessions(session1, session2 interface{}) interface{}
}

// Session interface for session data (avoid circular import).
// Uses interface{} for complex types to avoid circular dependencies.
type Session interface {
	SetEndpoint(endpoint string)
	GetEndpoint() string
	SetModel(model string)
	GetModel() string
	SetNSFWMode(enabled bool)
	GetNSFWMode() bool
	SetModelPinned(pinned bool)
	GetModelPinned() bool
	SetName(name string)
	ClearMessages()
	GetMessagesRaw() interface{}     // Returns []config.SessionMessage
	SetMessagesRaw(msgs interface{}) // Accepts only []config.SessionMessage
	SummarizeRaw() interface{}       // Returns SessionSummary
	SetCommandHistory(history []string)
	GetCommandHistory() []string
	SetWorkspace(ws string) // the directory the chat runs in (2.0 W4)
	GetWorkspace() string
}

// SessionSummary is a session's metadata, as SummarizeRaw returns it.
type SessionSummary = config.SessionSummary

// SetSessionManager sets the session manager for persistence.
func (m AppModel) SetSessionManager(sm SessionManager, session Session) AppModel {
	m.sessionManager = sm
	m.currentSession = session

	// Restore endpoint/model from session if available
	if session != nil {
		if endpoint := session.GetEndpoint(); endpoint != "" {
			m.endpoint = endpoint
			m.header = m.header.SetEndpoint(endpoint)
		}
		if model := session.GetModel(); model != "" {
			m.model = model
			// A /set-model --force pin outlives the resume.
			m.modelPinned = session.GetModelPinned()
			m.header = m.header.SetModel(model)

			// A ToolsPerModel provider (Venice) only knows whether tools are
			// available once the model is known (#151 W6b); recompute now
			// that the session's model has replaced WithEndpoint's guess.
			if m.provider != "" {
				m.skillsEnabled = providers.ToolsEnabledForModel(m.provider, m.model)
				m.header = m.header.SetSkillsEnabled(m.skillsEnabled)
			}
			// A session saved on a model the provider has since retired
			// resumes on the served one, from the catalog the startup
			// loaded. Only when the client is on the session's provider:
			// another provider's catalog would call every model retired.
			if src, ok := m.llmClient.(ActiveEndpointer); ok && src.ActiveEndpoint().Provider == m.provider {
				m, m.modelCheckPending = m.resolveFromMemory()
				model = m.model
			}

			// Initialize context tracker with session and model
			// Convert Session interface to *config.Session for ContextTracker
			if configSession, ok := session.(*config.Session); ok {
				// Pass config's ContextLimit as override if available
				if m.config != nil {
					override := m.config.ContextLimit
					resolved, known := config.ResolveContextLimit(m.config.BaseURL, model, override)
					m.contextTracker = config.NewContextTracker(configSession, model, resolved)
					if !known {
						if notice := config.UnknownContextNotice(model, resolved); notice != "" {
							m.chat = m.chat.AddSystemMessage("⚠️ " + notice)
						}
					}
				} else {
					m.contextTracker = config.NewContextTracker(configSession, model)
				}
				// Update header with initial context usage
				if m.contextTracker.MaxTokens > 0 {
					m.header = m.header.SetContextUsage(m.contextTracker.CurrentTokens, m.contextTracker.MaxTokens)
				}
			}
		}
		m.nsfwMode = session.GetNSFWMode()
		m.header = m.header.SetNSFWMode(m.nsfwMode)
	}

	return m
}

// SetVersion sets the application version and build information.
func (m AppModel) SetVersion(version, build string) AppModel {
	m.version = version
	m.build = build
	return m
}

// SetConfig sets the configuration for accessing context limits and other settings.
// WithGrimoireContent sets the grimoire content for /grimoire display.
func (m AppModel) WithGrimoireContent(content string) AppModel {
	m.grimoireContent = content
	return m
}

// WithCodeGraphSummary sets the code graph summary for /index display.
func (m AppModel) WithCodeGraphSummary(summary string) AppModel {
	m.codeGraphSummary = summary
	return m
}

// WithCodeGraphIndexer sets the code graph indexer for /graph visualization.
func (m AppModel) WithCodeGraphIndexer(indexer *codegraph.Indexer) AppModel {
	m.codeGraphIndexer = indexer
	return m
}

func (m AppModel) SetConfig(cfg *config.Config) AppModel {
	m.config = cfg
	return m
}

// WithMessages restores chat history from session messages.
func (m AppModel) WithMessages(messages []ChatMessage) AppModel {
	m.chat = m.chat.RestoreMessages(messages)

	// Add a system message at the end indicating session was resumed
	if len(messages) > 0 {
		m.chat = m.chat.AddSystemMessage(fmt.Sprintf("📂 Resumed session (%d messages)", len(messages)))
	}

	return m
}

// WithSystemMessage appends a system notice to the chat.
func (m AppModel) WithSystemMessage(text string) AppModel {
	m.chat = m.chat.AddSystemMessage(text)
	return m
}

// WithEndpoint restores the endpoint/provider from a loaded session.
func (m AppModel) WithEndpoint(endpoint string) AppModel {
	if endpoint != "" {
		m.endpoint = endpoint
		m.provider = endpoint // Provider matches endpoint name
		m.header = m.header.SetEndpoint(endpoint)
		LogInfo(fmt.Sprintf("✓ Restored endpoint from session: %s", endpoint))

		// Check provider capabilities
		if caps, ok := providers.GetProvider(m.provider); ok {
			// The model isn't chosen yet at this point (a fresh session's
			// AppModel.model is still ""); gate on the model that will
			// actually be in effect — the provider's own default — so a
			// ToolsPerModel provider (Venice) doesn't default to "tools
			// enabled" for a model (venice-uncensored) that has none (#151
			// W6b review). SetSessionManager and EndpointChange correct
			// this again once the real model is known.
			modelForGate := m.model
			if modelForGate == "" {
				modelForGate = caps.DefaultModel
			}
			m.skillsEnabled = providers.ToolsEnabledForModel(m.provider, modelForGate)
			m.header = m.header.SetSkillsEnabled(m.skillsEnabled)
			LogInfo(fmt.Sprintf("✓ Provider '%s' function calling support: %v", m.provider, m.skillsEnabled))

			// Auto-select best tool model if available
			if m.skillsEnabled && caps.PreferredToolModel != "" && m.model == "" {
				m.model = caps.PreferredToolModel
				m.header = m.header.SetModel(m.model)
				LogInfo(fmt.Sprintf("✓ Auto-selected preferred tool model: %s", m.model))
			}
		} else {
			LogInfo(fmt.Sprintf("⚠️ Provider '%s' not found in registry", m.provider))
		}
	}
	return m
}

// formatOrchestratorStats formats per-turn timing and token counts for the action feed.
func formatOrchestratorStats(d time.Duration, inputTok, outputTok int) string {
	var parts []string
	if d >= time.Millisecond {
		if d < time.Second {
			parts = append(parts, fmt.Sprintf("%.0fms", float64(d.Milliseconds())))
		} else if d < time.Minute {
			parts = append(parts, fmt.Sprintf("%.1fs", d.Seconds()))
		} else {
			parts = append(parts, fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60))
		}
	}
	if inputTok > 0 || outputTok > 0 {
		parts = append(parts, fmt.Sprintf("↑%s ↓%s", formatOrchestratorTokens(inputTok), formatOrchestratorTokens(outputTok)))
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " · ") + ")"
}

// formatOrchestratorTokens formats a token count compactly (e.g. 1234 → "1.2k").
func formatOrchestratorTokens(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return strconv.Itoa(n)
}

// expandFileRefs replaces @filename tokens in text with the contents of the referenced file.
// Filenames are resolved relative to the current working directory.
// Unknown or unreadable files are left as-is.
func expandFileRefs(text string) string {
	// Find all @word tokens
	words := strings.Fields(text)
	replacements := map[string]string{}
	for _, w := range words {
		if !strings.HasPrefix(w, "@") || len(w) < 2 {
			continue
		}
		filename := w[1:]
		if _, already := replacements[filename]; already {
			continue
		}
		data, err := os.ReadFile(filename)
		if err != nil {
			continue
		}
		replacements[filename] = string(data)
	}
	if len(replacements) == 0 {
		return text
	}
	// Replace each @filename with its contents inline
	result := text
	for name, contents := range replacements {
		result = strings.ReplaceAll(result, "@"+name, "\n```\n"+contents+"```\n")
	}
	return result
}

// WithCommandHistory restores the command history from a saved session.
func (m AppModel) WithCommandHistory(history []string) AppModel {
	if len(history) > 0 {
		m.input = m.input.SetHistory(history)
	}
	return m
}

// persistSession saves the current session state.
func (m *AppModel) persistSession() {
	if m.sessionManager == nil || m.currentSession == nil {
		return
	}

	m.claimWorkspace(m.currentSession)
	m.currentSession.SetEndpoint(m.endpoint)
	m.currentSession.SetModel(m.model)
	m.currentSession.SetModelPinned(m.modelPinned)
	m.currentSession.SetNSFWMode(m.nsfwMode)
	if hist := m.input.GetHistory(); len(hist) > 0 {
		m.currentSession.SetCommandHistory(hist)
	}

	// Must be []config.SessionMessage: SetMessagesRaw ignores any other type,
	// which is how sessions used to silently stop saving history.
	m.currentSession.SetMessagesRaw(SessionMessagesFromChat(m.savedMessages()))

	// Save synchronously: Save mutates and marshals the session, and Update
	// keeps mutating it, so a goroutine here races.
	_ = m.sessionManager.Save(m.currentSession)
}

// claimWorkspace records the chat's workspace on a session that has none:
// a new one, or one an older celeste saved (2.0 W4 ruling 1).
func (m AppModel) claimWorkspace(s Session) {
	if s == nil || m.workDir == "" || s.GetWorkspace() != "" {
		return
	}
	if abs, err := filepath.Abs(m.workDir); err == nil {
		s.SetWorkspace(abs)
	}
}

// savedMessages is the chat as the session saves it. A reply still being
// typed shows its typed prefix plus glitch glyphs; the session gets the
// whole reply received so far instead (typingContent: the loop's text once
// the reply is done). Turn events never save while typing, the typing
// commit does, but a quit or an interrupt can land mid-typing.
func (m AppModel) savedMessages() []ChatMessage {
	msgs := m.chat.GetMessages()
	if m.typingContent == "" {
		return msgs
	}
	msgs = append([]ChatMessage(nil), msgs...)
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "assistant" {
			msgs[i].Content = m.typingContent
			break
		}
	}
	return msgs
}

// handleSessionAction handles session management actions.
func (m AppModel) handleSessionAction(action *commands.SessionAction) AppModel {
	if m.sessionManager == nil {
		m.chat = m.chat.AddSystemMessage("❌ Session manager not available")
		return m
	}

	switch action.Action {
	case "new":
		// Save current session first
		m.persistSession()

		// Create new session
		newSession := m.sessionManager.NewSession()
		if s, ok := newSession.(Session); ok {
			if action.Name != "" {
				s.SetName(action.Name)
			}
			m.claimWorkspace(s)
			m.currentSession = s

			// Clear chat
			m.chat = m.chat.Clear()

			// Show success with short ID
			if summary := s.SummarizeRaw(); summary != nil {
				m.chat = m.chat.AddSystemMessage("📝 New session created")
			}
		}

	case "resume":
		// Save current session first
		m.persistSession()

		// Try to load by ID first
		loaded, err := m.sessionManager.Load(action.SessionID)

		// If not found by ID, search by name
		if err != nil {
			if sessions, listErr := m.sessionManager.List(); listErr == nil {
				for _, sessionRaw := range sessions {
					if s, ok := sessionRaw.(Session); ok {
						if summaryRaw := s.SummarizeRaw(); summaryRaw != nil {
							if summary, ok := summaryRaw.(SessionSummary); ok {
								if strings.EqualFold(summary.Name, action.SessionID) {
									loaded = s
									err = nil
									break
								}
							}
						}
					}
				}
			}
		}

		// Load requested session
		if err == nil {
			if s, ok := loaded.(Session); ok {
				m.claimWorkspace(s)
				m.currentSession = s

				// Clear current chat
				m.chat = m.chat.Clear()

				// Restore messages
				if messagesRaw := s.GetMessagesRaw(); messagesRaw != nil {
					if sessionMsgs, ok := messagesRaw.([]config.SessionMessage); ok {
						m.chat = m.chat.RestoreMessages(ChatMessagesFromSession(sessionMsgs))
					}
				}

				// Restore state
				if endpoint := s.GetEndpoint(); endpoint != "" {
					m.endpoint = endpoint
					m.header = m.header.SetEndpoint(m.endpoint)
				}
				m.nsfwMode = s.GetNSFWMode()
				m.header = m.header.SetNSFWMode(m.nsfwMode)

				msgCount := 0
				if msgs := s.GetMessagesRaw(); msgs != nil {
					if sm, ok := msgs.([]config.SessionMessage); ok {
						msgCount = len(sm)
					}
				}
				m.chat = m.chat.AddSystemMessage(
					fmt.Sprintf("📂 Resumed session (%d messages)", msgCount))
			}
		} else {
			m.chat = m.chat.AddSystemMessage(
				fmt.Sprintf("❌ Failed to load session: %v", err))
		}

	case "list":
		if sessions, err := m.sessionManager.List(); err == nil {
			if len(sessions) == 0 {
				m.chat = m.chat.AddSystemMessage("No saved sessions")
			} else {
				// Convert to SessionSummary slice for sorting
				summaries := make([]SessionSummary, 0, len(sessions))
				for _, sessionRaw := range sessions {
					if s, ok := sessionRaw.(Session); ok {
						if summaryRaw := s.SummarizeRaw(); summaryRaw != nil {
							if summary, ok := summaryRaw.(SessionSummary); ok {
								summaries = append(summaries, summary)
							}
						}
					}
				}

				// This project's sessions first, then the rest; each newest
				// first (2.0 W4 ruling 2).
				sort.SliceStable(summaries, func(i, j int) bool {
					return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
				})
				thisProject := func(sum SessionSummary) bool { return config.SameProject(sum.Workspace, m.workDir) }
				sort.SliceStable(summaries, func(i, j int) bool {
					return thisProject(summaries[i]) && !thisProject(summaries[j])
				})

				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("\n📋 Saved Sessions (%d):\n\n", len(summaries)))

				// Get current session ID for comparison
				var currentSessionID string
				if m.currentSession != nil {
					if currentSummaryRaw := m.currentSession.SummarizeRaw(); currentSummaryRaw != nil {
						if summary, ok := currentSummaryRaw.(SessionSummary); ok {
							currentSessionID = summary.ID
						}
					}
				}

				for _, summary := range summaries {
					// Display name if set, otherwise "Untitled"
					displayName := summary.Name
					if displayName == "" {
						displayName = "Untitled Session"
					}

					// Mark current session with ★
					currentMarker := ""
					if currentSessionID != "" && currentSessionID == summary.ID {
						currentMarker = "★ "
					}

					// Relative timestamp
					relativeTime := humanizeTime(summary.UpdatedAt)

					// Get endpoint/model from metadata
					endpoint := ""
					model := ""
					if summary.Metadata != nil {
						if e, ok := summary.Metadata["endpoint"].(string); ok {
							endpoint = e
						}
						if m, ok := summary.Metadata["model"].(string); ok {
							model = m
						}
					}

					// Format output
					projectMark := ""
					if thisProject(summary) {
						projectMark = " (this project)"
					}
					sb.WriteString(fmt.Sprintf("• %s%s (%s)%s\n", currentMarker, displayName, summary.ID, projectMark))
					if model != "" && endpoint != "" {
						sb.WriteString(fmt.Sprintf("  %s • %d msgs • %s @ %s\n",
							relativeTime, summary.MessageCount, model, endpoint))
					} else {
						sb.WriteString(fmt.Sprintf("  %s • %d msgs\n",
							relativeTime, summary.MessageCount))
					}
					if summary.FirstMessage != "" {
						sb.WriteString(fmt.Sprintf("  \"%s\"\n", summary.FirstMessage))
					}
					sb.WriteString("\n")
				}

				sb.WriteString("Commands:\n")
				sb.WriteString("  /session resume <id>       - Load session by ID\n")
				sb.WriteString("  /session resume \"<name>\"   - Load session by name\n")
				sb.WriteString("  /session rename <id> <name> - Rename a session\n")
				sb.WriteString("  /session delete <id>       - Delete a session\n")
				m.chat = m.chat.AddSystemMessage(sb.String())
			}
		} else {
			m.chat = m.chat.AddSystemMessage(
				fmt.Sprintf("❌ Failed to list sessions: %v", err))
		}

	case "clear":
		// Create new session automatically
		newSession := m.sessionManager.NewSession()
		if s, ok := newSession.(Session); ok {
			m.currentSession = s
		}
		// Refresh system prompt so /user and /confirm changes take effect
		if refresher, ok := m.llmClient.(PromptRefresher); ok {
			refresher.RefreshSystemPrompt()
		}
		m.chat = m.chat.AddSystemMessage("🗑️  Session cleared, new session started")

	case "merge":
		m.persistSession() // the merge reads the current session's saved messages
		if toMerge, err := m.sessionManager.Load(action.SessionID); err == nil {
			merged := m.sessionManager.MergeSessions(m.currentSession, toMerge)
			if s, ok := merged.(Session); ok {
				m.currentSession = s

				// Clear and reload with merged messages
				m.chat = m.chat.Clear()
				if messagesRaw := s.GetMessagesRaw(); messagesRaw != nil {
					if sessionMsgs, ok := messagesRaw.([]config.SessionMessage); ok {
						m.chat = m.chat.RestoreMessages(ChatMessagesFromSession(sessionMsgs))

						m.chat = m.chat.AddSystemMessage(
							fmt.Sprintf("🔀 Merged sessions (%d total messages)", len(sessionMsgs)))
					}
				}

				// Save merged session
				m.persistSession()
			}
		} else {
			m.chat = m.chat.AddSystemMessage(
				fmt.Sprintf("❌ Failed to merge session: %v", err))
		}

	case "rename":
		if loaded, err := m.sessionManager.Load(action.SessionID); err == nil {
			if s, ok := loaded.(Session); ok {
				// Update the name
				s.SetName(action.Name)

				// Save the session
				if saveErr := m.sessionManager.Save(s); saveErr == nil {
					m.chat = m.chat.AddSystemMessage(
						fmt.Sprintf("✓ Renamed session to: %s", action.Name))
				} else {
					m.chat = m.chat.AddSystemMessage(
						fmt.Sprintf("❌ Failed to save renamed session: %v", saveErr))
				}
			}
		} else {
			m.chat = m.chat.AddSystemMessage(
				fmt.Sprintf("❌ Failed to load session: %v", err))
		}

	case "delete", "rm":
		// Prevent deleting current session
		currentID := ""
		if m.currentSession != nil {
			if summaryRaw := m.currentSession.SummarizeRaw(); summaryRaw != nil {
				if summary, ok := summaryRaw.(SessionSummary); ok {
					currentID = summary.ID
				}
			}
		}

		if currentID == action.SessionID {
			m.chat = m.chat.AddSystemMessage(
				"❌ Cannot delete current session. Switch to another session first.")
		} else {
			if err := m.sessionManager.Delete(action.SessionID); err == nil {
				m.chat = m.chat.AddSystemMessage(
					fmt.Sprintf("✓ Deleted session: %s", action.SessionID))
			} else {
				m.chat = m.chat.AddSystemMessage(
					fmt.Sprintf("❌ Failed to delete session: %v", err))
			}
		}

	case "info":
		if m.currentSession != nil {
			msgCount := 0
			if msgs := m.currentSession.GetMessagesRaw(); msgs != nil {
				if sm, ok := msgs.([]config.SessionMessage); ok {
					msgCount = len(sm)
				}
			}

			var sb strings.Builder
			sb.WriteString("\n📊 Current Session Info:\n\n")
			sb.WriteString(fmt.Sprintf("• Messages: %d\n", msgCount))
			sb.WriteString(fmt.Sprintf("• Model: %s\n", m.model))
			sb.WriteString(fmt.Sprintf("• Endpoint: %s\n", m.endpoint))
			if m.nsfwMode {
				sb.WriteString("• Mode: NSFW\n")
			}

			m.chat = m.chat.AddSystemMessage(sb.String())
		}
	}

	return m
}

// --- Header Model ---

// HeaderModel represents the header bar.
type HeaderModel struct {
	width            int
	nsfwMode         bool
	endpoint         string
	model            string
	imageModel       string           // Image generation model (NSFW mode)
	autoRouted       bool             // Whether the last message was auto-routed
	skillsEnabled    bool             // Whether skills/function calling is available
	contextIndicator ContextIndicator // Token usage display
	showContext      bool             // Whether to show context usage
}

// NewHeaderModel creates a new header model.
func NewHeaderModel() HeaderModel {
	return HeaderModel{endpoint: "openai"} // Default endpoint
}

// SetWidth sets the header width.
func (m HeaderModel) SetWidth(width int) HeaderModel {
	m.width = width
	return m
}

// SetNSFWMode sets the NSFW mode indicator.
func (m HeaderModel) SetNSFWMode(enabled bool) HeaderModel {
	m.nsfwMode = enabled
	return m
}

// SetEndpoint sets the current endpoint.
func (m HeaderModel) SetEndpoint(endpoint string) HeaderModel {
	m.endpoint = endpoint
	return m
}

// SetModel sets the current model.
func (m HeaderModel) SetModel(model string) HeaderModel {
	m.model = model
	return m
}

// SetImageModel sets the current image generation model.
func (m HeaderModel) SetImageModel(model string) HeaderModel {
	m.imageModel = model
	return m
}

// SetSkillsEnabled sets whether skills/function calling is available.
func (m HeaderModel) SetSkillsEnabled(enabled bool) HeaderModel {
	m.skillsEnabled = enabled
	return m
}

// SetAutoRouted sets whether auto-routing occurred.
func (m HeaderModel) SetAutoRouted(routed bool) HeaderModel {
	m.autoRouted = routed
	return m
}

// SetContextUsage updates the context usage display.
func (m HeaderModel) SetContextUsage(current, max int) HeaderModel {
	m.contextIndicator = m.contextIndicator.SetUsage(current, max)
	m.showContext = true
	return m
}

// View renders the header.
func (m HeaderModel) View() string {
	title := HeaderTitleStyle.Render("✨ Celeste CLI")

	// Build endpoint/mode indicator
	var endpointInfo string
	if m.nsfwMode {
		endpointInfo = NSFWStyle.Render("🔥 NSFW")
		// Show image model if set
		if m.imageModel != "" {
			endpointInfo += " • " + ModelStyle.Render("img:"+m.imageModel)
		}
	} else if m.endpoint != "" && m.endpoint != "openai" {
		// Show non-default endpoint
		endpointDisplay := map[string]string{
			"venice":     "Venice.ai",
			"grok":       "Grok",
			"elevenlabs": "ElevenLabs",
			"google":     "Google",
		}
		display := endpointDisplay[m.endpoint]
		if display == "" {
			display = m.endpoint
		}
		endpointInfo = EndpointStyle.Render(display)
		if m.autoRouted {
			endpointInfo = "🔀 " + endpointInfo
		}
	}

	// Add model info if set (and not in NSFW mode, as it shows chat model separately)
	if m.model != "" && !m.nsfwMode {
		if endpointInfo != "" {
			endpointInfo += " • "
		}
		// Add capability indicator
		modelDisplay := m.model
		if m.skillsEnabled {
			modelDisplay += " ✓" // Checkmark for skills enabled
		} else {
			modelDisplay += " ⚠" // Warning for no skills
		}
		endpointInfo += ModelStyle.Render(modelDisplay)
	}

	// Add context usage indicator if available
	var contextInfo string
	if m.showContext {
		contextInfo = m.contextIndicator.ViewCompact()
	}

	info := HeaderInfoStyle.Render("Press Ctrl+C to exit")
	if endpointInfo != "" {
		info = endpointInfo + " • " + info
	}
	if contextInfo != "" {
		info = info + " • " + contextInfo
	}

	// Calculate gap
	gap := m.width - lipgloss.Width(title) - lipgloss.Width(info) - 2
	if gap < 1 {
		gap = 1
	}
	spacer := strings.Repeat("─", gap)

	return HeaderStyle.Width(m.width).Render(
		title + spacer + info,
	)
}

// --- Status Model ---

// StatusModel represents the status bar.
type StatusModel struct {
	width          int
	text           string
	streaming      bool
	frame          int
	warningMessage string // Context warning message
	warningLevel   string // "warn", "caution", "critical"
	showWarning    bool   // Whether to show warning
}

// NewStatusModel creates a new status model.
func NewStatusModel() StatusModel {
	return StatusModel{text: "Ready"}
}

// SetWidth sets the status bar width.
func (m StatusModel) SetWidth(width int) StatusModel {
	m.width = width
	return m
}

// SetText sets the status text.
func (m StatusModel) SetText(text string) StatusModel {
	m.text = text
	return m
}

// SetStreaming sets the streaming indicator.
func (m StatusModel) SetStreaming(streaming bool) StatusModel {
	m.streaming = streaming
	return m
}

// Update handles tick messages for animation.
func (m StatusModel) Update(msg tea.Msg) (StatusModel, tea.Cmd) {
	if _, ok := msg.(TickMsg); ok {
		m.frame++
	}
	return m, nil
}

// View renders the status bar.
func (m StatusModel) View() string {
	var status string

	// Priority: warnings > streaming > normal text
	if m.showWarning {
		// Show context warning with appropriate color
		warningStyle := m.getWarningStyle()
		status = warningStyle.Render(m.warningMessage)
	} else if m.streaming {
		// Show the text set by the typing animation (thinking phrases + spinner)
		// Falls back to "Streaming..." if no text was set
		if m.text != "" && m.text != "Ready" {
			status = StatusStreamingStyle.Render(m.text)
		} else {
			frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
			spinner := StatusStreamingStyle.Render(frames[m.frame%len(frames)])
			status = spinner + " " + StatusStreamingStyle.Render("Streaming...")
		}
	} else {
		status = StatusActiveStyle.Render("●") + " " + m.text
	}

	return StatusBarStyle.Width(m.width).Render(status)
}

// getWarningStyle returns the appropriate style for the warning level.
func (m StatusModel) getWarningStyle() lipgloss.Style {
	switch m.warningLevel {
	case "critical":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true) // Bright red, bold
	case "caution":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true) // Orange, bold
	case "warn":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("226")) // Yellow
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("82")) // Green
	}
}

// --- Helper functions ---

// humanizeTime converts a timestamp to a human-readable relative time.
func humanizeTime(t time.Time) string {
	duration := time.Since(t)

	if duration < time.Minute {
		return "just now"
	} else if duration < time.Hour {
		mins := int(duration.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	} else if duration < 24*time.Hour {
		hours := int(duration.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else if duration < 7*24*time.Hour {
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	} else {
		return t.Format("Jan 2, 2006")
	}
}

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}

// fetchElevenLabsVoices calls the ElevenLabs API and returns a formatted voice list.
func fetchElevenLabsVoices(apiKey string) (string, error) {
	req, err := http.NewRequest("GET", "https://api.elevenlabs.io/v1/voices", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("xi-api-key", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("ElevenLabs API returned %d", resp.StatusCode)
	}

	var result struct {
		Voices []struct {
			VoiceID string            `json:"voice_id"`
			Name    string            `json:"name"`
			Labels  map[string]string `json:"labels"`
		} `json:"voices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ElevenLabs Voices (%d):\n\n", len(result.Voices)))
	for _, v := range result.Voices {
		sb.WriteString(fmt.Sprintf("  %s  %s", v.VoiceID, v.Name))
		if len(v.Labels) > 0 {
			parts := make([]string, 0, len(v.Labels))
			for k, val := range v.Labels {
				parts = append(parts, k+": "+val)
			}
			sb.WriteString("  [" + strings.Join(parts, ", ") + "]")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\nSet a voice: /voice set-voice <voice-id>")
	return sb.String(), nil
}
