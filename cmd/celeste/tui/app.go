// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains the main application model and layout logic.
package tui

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/collections"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/commands"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/venice"
)

// Typing speed: ~25 chars/sec for smooth, visible corruption effects
const charsPerTick = 2
const typingTickInterval = 80 * time.Millisecond

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
	mcpPanel         MCPPanelModel

	// Application state
	width         int
	height        int
	ready         bool
	nsfwMode      bool
	streaming     bool
	endpoint      string // Current endpoint (openai, venice, grok, etc.)
	safeEndpoint  string // Endpoint to return to when leaving NSFW mode
	model         string // Current model name
	imageModel    string // Current image generation model (for NSFW mode)
	provider      string // Current provider (grok, openai, venice, etc.) - detected from endpoint
	skillsEnabled bool   // Whether skills/function calling is available
	version       string // Application version (e.g., "1.0.1")
	build         string // Build identifier (e.g., "bubbletea-tui")
	runtimeMode   string // Runtime orchestration mode (classic or claw)

	// Simulated typing state
	typingContent string // Full content to type
	typingPos     int    // Current position in content
	animFrame     int    // Animation frame counter

	// Pending tool call tracking
	pendingToolCallID  string // Track tool call ID for sending result back to LLM
	pendingToolCalls   []pendingToolCall
	toolBatchActive    bool
	clawToolIterations int // Assistant tool-call turns in the current user turn
	clawMaxIterations  int // Safety cap for claw mode tool loops

	// LLM client (injected)
	llmClient LLMClient

	// Session persistence (optional)
	sessionManager SessionManager
	currentSession Session

	// Configuration (for context limits, etc.)
	config *config.Config

	// Context tracking (NEW)
	contextTracker *config.ContextTracker

	// Interactive selector
	selector       SelectorModel
	selectorActive bool

	// Collections view
	collectionsModel *CollectionsModel
	menuModel        *MenuModel
	skillsBrowser    *SkillsBrowserModel
	viewMode         string // "chat", "collections", "menu", "skills"

	// Split panel for orchestrator/agent view
	splitPanel     *SplitPanel
	splitPanelMode bool

	// Running token totals for the current orchestrator run
	orchInputTokens  int
	orchOutputTokens int

	// Running token totals for the current agent run
	agentInputTokens  int
	agentOutputTokens int
	agentRunStart     time.Time

	// Per-message response timing and token stats (regular chat)
	streamStart   time.Time
	lastMsgInTok  int
	lastMsgOutTok int
}

type pendingToolCall struct {
	name       string
	args       map[string]any
	toolCallID string
	parseError string
}

// LLMClient interface for sending messages to the LLM.
type LLMClient interface {
	SendMessage(messages []ChatMessage, tools []SkillDefinition) tea.Cmd
	GetSkills() []SkillDefinition
	ExecuteSkill(name string, args map[string]any, toolCallID string) tea.Cmd
}

// AgentCommandRunner is an optional extension for handling /agent from TUI.
type AgentCommandRunner interface {
	RunAgentCommand(args []string) tea.Cmd
}

// OrchestratorCommandRunner is an optional extension for handling /orchestrate from TUI.
type OrchestratorCommandRunner interface {
	RunOrchestratorCommand(goal string) tea.Cmd
}

// EndpointSwitcher interface for clients that support dynamic endpoint switching.
type EndpointSwitcher interface {
	SwitchEndpoint(endpoint string) error
	ChangeModel(model string) error
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
		header:            NewHeaderModel(),
		chat:              NewChatModel(),
		input:             NewInputModel(),
		skills:            NewSkillsModel(),
		status:            NewStatusModel(),
		toolProgress:      NewToolProgressModel(),
		contextBar:        NewContextBarModel(),
		permissionPrompt:  NewPermissionPromptModel(),
		mcpPanel:          NewMCPPanelModel(),
		llmClient:         llmClient,
		viewMode:          "chat",
		runtimeMode:       config.RuntimeModeClassic,
		clawMaxIterations: config.DefaultClawMaxToolIterations,
	}
}

// Init implements tea.Model.
func (m AppModel) Init() tea.Cmd {
	return tea.Batch(
		m.input.Init(),
		tea.EnterAltScreen,
	)
}

// Update implements tea.Model.
func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

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

	// If in skills view, handle all inputs there
	if m.viewMode == "skills" {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			if msg.String() == "q" || msg.String() == "Q" || msg.String() == "esc" {
				// Return to chat mode
				m.viewMode = "chat"
				return m, nil
			}
		case skillSelectedMsg:
			// User selected a skill, show it in input for them to add parameters
			m.viewMode = "chat"
			m.input = m.input.SetValue(msg.skillName + " ")
			m.input = m.input.Focus()
			return m, nil
		}

		// Update skills browser
		if m.skillsBrowser != nil {
			updated, cmd := m.skillsBrowser.Update(msg)
			if updatedModel, ok := updated.(SkillsBrowserModel); ok {
				*m.skillsBrowser = updatedModel
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

		switch msg.String() {
		case "ctrl+c":
			m.persistSession()
			return m, tea.Quit
		case "ctrl+k":
			// Toggle skill call logs visibility
			m.chat = m.chat.ToggleSkillCalls()
			m.status = m.status.SetText("Skill calls toggled")
		case "pgup", "shift+up":
			if m.splitPanelMode && m.splitPanel != nil {
				m.splitPanel.ScrollUp(5)
			} else {
				var cmd tea.Cmd
				m.chat, cmd = m.chat.Update(msg)
				cmds = append(cmds, cmd)
			}
		case "pgdown", "shift+down":
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
		inputHeight := 3   // 1 border + 1 text + 1 typeahead hint line
		skillsHeight := 12 // Increased for RPG-style menu with contextual help
		statusHeight := 1
		chatHeight := m.height - headerHeight - inputHeight - skillsHeight - statusHeight

		// Ensure minimum chat height
		if chatHeight < 5 {
			chatHeight = 5
			skillsHeight = m.height - headerHeight - inputHeight - statusHeight - chatHeight
			if skillsHeight < 6 {
				skillsHeight = 6 // Minimum for RPG menu
			}
		}

		// Update component sizes
		m.header = m.header.SetWidth(m.width)
		m.chat = m.chat.SetSize(m.width, chatHeight)
		m.input = m.input.SetWidth(m.width)
		m.skills = m.skills.SetSize(m.width, skillsHeight)
		m.status = m.status.SetWidth(m.width)

		// Resize new components
		m.toolProgress.SetSize(m.width, 0)
		m.contextBar.SetSize(m.width, 0)
		m.permissionPrompt.SetSize(m.width, 0)
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

		// Dismiss the split panel when user sends next message (unless it's another /orchestrate)
		if m.splitPanelMode && !strings.HasPrefix(content, "/orchestrate") && !strings.HasPrefix(content, "/orch ") {
			m.splitPanelMode = false
			m.splitPanel = nil
		}

		// Check if it's a slash command first
		if cmd := commands.Parse(content); cmd != nil {
			// Handle Phase 4 commands that require app state (contextTracker, currentSession)
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
				m.status = m.status.SetStreaming(true)
				m.status = m.status.SetText(StreamingSpinner(0) + " Running agent...")
				m.chat = m.chat.AddSystemMessage("🤖 Agent running: " + strings.Join(cmd.Args, " "))

				agentArgs := append([]string{}, cmd.Args...)
				return m, tea.Batch(
					agentRunner.RunAgentCommand(agentArgs),
					tea.Tick(typingTickInterval*2, func(t time.Time) tea.Msg {
						return TickMsg{Time: t}
					}),
				)

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
				return m, tea.Batch(
					orchRunner.RunOrchestratorCommand(goal),
					tea.Tick(typingTickInterval*2, func(t time.Time) tea.Msg {
						return TickMsg{Time: t}
					}),
				)

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

			case "context":
				result := commands.HandleContextCommand(cmd.Args, m.contextTracker)
				if result.ShouldRender {
					m.chat = m.chat.AddSystemMessage(result.Message)
				}
				return m, nil

			case "collections":
				// Switch to collections view
				m.viewMode = "collections"

				// Create collections manager if not exists
				if m.collectionsModel == nil && m.config != nil {
					client := collections.NewClient(m.config.XAIManagementAPIKey)
					manager := collections.NewManager(client, m.config)
					model := NewCollectionsModel(manager)
					m.collectionsModel = &model
				}

				if m.collectionsModel != nil {
					return m, m.collectionsModel.Init()
				}
				return m, nil

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
				m.skillsBrowser = &model
				return m, m.skillsBrowser.Init()

			case "mcp":
				m.mcpPanel.Show()
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
				APIKey:        "", // Will be populated if config accessible
				BaseURL:       "", // Will be populated if config accessible
				SkillsEnabled: m.skillsEnabled,
				Version:       m.version,
				Build:         m.build,
			}
			result := commands.Execute(cmd, ctx)

			// Show command result message if needed
			if result.ShouldRender {
				m.chat = m.chat.AddSystemMessage(result.Message)
			}

			// Apply state changes
			if result.StateChange != nil {
				if result.StateChange.EndpointChange != nil {
					m.endpoint = *result.StateChange.EndpointChange

					// Detect provider from endpoint name
					// Provider detection will use endpoint name mapping
					m.provider = m.endpoint

					// Update skills availability and auto-select best model
					if caps, ok := providers.GetProvider(m.provider); ok {
						m.skillsEnabled = caps.SupportsFunctionCalling

						// AUTO-SELECT: Choose best tool-calling model for this provider
						if caps.PreferredToolModel != "" {
							m.model = caps.PreferredToolModel
							m.header = m.header.SetModel(m.model)
							LogInfo(fmt.Sprintf("Auto-selected model: %s (optimized for tool calling)", m.model))

							// Update LLM client model
							if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
								if err := switcher.ChangeModel(m.model); err != nil {
									LogInfo(fmt.Sprintf("Error changing model: %v", err))
								}
							}
						} else if caps.DefaultModel != "" {
							m.model = caps.DefaultModel
							m.header = m.header.SetModel(m.model)
							LogInfo(fmt.Sprintf("Using default model: %s", m.model))

							// Update LLM client model
							if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
								if err := switcher.ChangeModel(m.model); err != nil {
									LogInfo(fmt.Sprintf("Error changing model: %v", err))
								}
							}
						}

						LogInfo(fmt.Sprintf("Provider detected: %s, skills enabled: %v", m.provider, m.skillsEnabled))
					}

					m.header = m.header.SetEndpoint(m.endpoint)
					m.header = m.header.SetSkillsEnabled(m.skillsEnabled) // Update UI indicator
					m.status = m.status.SetText(fmt.Sprintf("Switched to %s", m.endpoint))

					// FIX: When switching endpoints, disable NSFW mode unless switching TO venice
					if m.endpoint != "venice" && m.nsfwMode {
						m.nsfwMode = false
						m.header = m.header.SetNSFWMode(false)
						LogInfo("NSFW mode disabled when switching away from Venice")
					}

					// Actually switch the LLM client endpoint
					if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
						if err := switcher.SwitchEndpoint(m.endpoint); err != nil {
							m.status = m.status.SetText(fmt.Sprintf("Error switching endpoint: %v", err))
						}
					}

					// Persist session state
					m.persistSession()
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
					}

					// Persist session state
					m.persistSession()
				}
				if result.StateChange.Model != nil {
					m.model = *result.StateChange.Model
					m.header = m.header.SetModel(m.model)
					m.status = m.status.SetText(fmt.Sprintf("Model changed to %s", m.model))

					// Actually change the model
					if switcher, ok := m.llmClient.(EndpointSwitcher); ok {
						if err := switcher.ChangeModel(m.model); err != nil {
							m.status = m.status.SetText(fmt.Sprintf("Error changing model: %v", err))
						}
					}

					// Persist session state
					m.persistSession()
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
		m.clawToolIterations = 0
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

		// Send to LLM and start animation
		if m.llmClient != nil {
			toolsToSend := m.getToolsForDispatch()
			cmds = append(cmds, m.llmClient.SendMessage(m.chat.GetLLMMessages(), toolsToSend))
			// Start animation tick for waiting state
			cmds = append(cmds, tea.Tick(typingTickInterval*2, func(t time.Time) tea.Msg {
				return TickMsg{Time: t}
			}))
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
		m.chat = m.chat.AppendToLastAssistant(msg.Chunk.Content)
		if msg.Chunk.IsFirst {
			m.chat = m.chat.AddAssistantMessage("")
		}
		cmds = append(cmds, nil) // Keep processing

	case StreamDoneMsg:
		// Update token counts from API response
		if msg.Usage != nil && (msg.Usage.PromptTokens > 0 || msg.Usage.CompletionTokens > 0) {
			m.lastMsgInTok = msg.Usage.PromptTokens
			m.lastMsgOutTok = msg.Usage.CompletionTokens
			if m.contextTracker != nil {
				m.contextTracker.UpdateTokens(
					msg.Usage.PromptTokens,
					msg.Usage.CompletionTokens,
					msg.Usage.TotalTokens,
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
		} else if msg.FullContent != "" {
			// API didn't return token usage — estimate from response length and
			// update the context tracker so the header counter keeps moving.
			estOut := config.EstimateTokens(msg.FullContent)
			if m.contextTracker != nil && estOut > 0 {
				cur := m.contextTracker.CurrentTokens + estOut
				m.contextTracker.UpdateTokens(0, estOut, cur)
				m.header = m.header.SetContextUsage(m.contextTracker.CurrentTokens, m.contextTracker.MaxTokens)
			}
			// Leave lastMsgInTok/lastMsgOutTok at 0 so the TickMsg inferred path runs.
		}

		if msg.FullContent != "" {
			// Check for content policy refusal
			if commands.IsContentPolicyRefusal(msg.FullContent) && m.endpoint != "venice" {
				// Detected refusal - offer to switch to Venice
				m.chat = m.chat.AddSystemMessage(
					"⚠️  Content policy refusal detected.\n\n" +
						"💡 Tip: Use /nsfw to switch to Venice.ai for uncensored responses,\n" +
						"or add 'nsfw' at the end of your message for auto-routing.",
				)
				m.streaming = false
				m.status = m.status.SetStreaming(false)
				m.status = m.status.SetText("Content policy refusal - use /nsfw")

				// Still show the original response
				m.typingContent = msg.FullContent
				m.typingPos = 0
				m.chat = m.chat.AddAssistantMessage("")
				cmds = append(cmds, tea.Tick(typingTickInterval, func(t time.Time) tea.Msg {
					return TickMsg{Time: t}
				}))
			} else {
				// Normal response - start simulated typing
				m.typingContent = msg.FullContent
				m.typingPos = 0
				m.chat = m.chat.AddAssistantMessage("") // Start with empty message
				m.status = m.status.SetText("Typing...")
				// Schedule first typing tick
				cmds = append(cmds, tea.Tick(typingTickInterval, func(t time.Time) tea.Msg {
					return TickMsg{Time: t}
				}))
			}
		} else {
			m.streaming = false
			m.status = m.status.SetStreaming(false)
			m.status = m.status.SetText(fmt.Sprintf("Done (%s)", msg.FinishReason))
		}

	case StreamErrorMsg:
		m.streaming = false
		m.status = m.status.SetStreaming(false)
		m.status = m.status.SetText(fmt.Sprintf("Error: %v", msg.Err))
		m.chat = m.chat.AddSystemMessage(fmt.Sprintf("Error: %v", msg.Err))

	case ToolProgressMsg:
		var cmd tea.Cmd
		m.toolProgress, cmd = m.toolProgress.Update(msg)
		cmds = append(cmds, cmd)

	case ContextBudgetMsg:
		var cmd tea.Cmd
		m.contextBar, cmd = m.contextBar.Update(msg)
		cmds = append(cmds, cmd)

	case PermissionRequestMsg:
		var cmd tea.Cmd
		m.permissionPrompt, cmd = m.permissionPrompt.Update(msg)
		cmds = append(cmds, cmd)

	case MCPStatusMsg:
		var cmd tea.Cmd
		m.mcpPanel, cmd = m.mcpPanel.Update(msg)
		cmds = append(cmds, cmd)

	case AgentProgressMsg:
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
			// Feed the response through SimulatedTyping — same path as regular chat.
			if strings.TrimSpace(msg.Text) != "" {
				m.typingContent = msg.Text
				m.typingPos = 0
				m.chat = m.chat.AddAssistantMessage("")
				m.status = m.status.SetText("Agent: typing response...")
				cmds = append(cmds, tea.Tick(typingTickInterval, func(t time.Time) tea.Msg {
					return TickMsg{Time: t}
				}))
			}

		case AgentProgressComplete:
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

	case SkillCallMsg:
		batchMsg := SkillCallBatchMsg{
			Calls: []SkillCallRequest{
				{
					Call:       msg.Call,
					ToolCallID: msg.ToolCallID,
				},
			},
			AssistantContent: msg.AssistantContent,
			ToolCalls:        msg.ToolCalls,
		}
		var batchCmds []tea.Cmd
		m, batchCmds = m.handleSkillCallBatch(batchMsg)
		cmds = append(cmds, batchCmds...)

	case SkillCallBatchMsg:
		var batchCmds []tea.Cmd
		m, batchCmds = m.handleSkillCallBatch(msg)
		cmds = append(cmds, batchCmds...)

	case SkillResultMsg:
		// Log the skill result
		LogSkillResult(msg.Name, msg.Result, msg.Err)

		isBatchResult := m.toolBatchActive
		shouldFollowUp := msg.ToolCallID != ""
		if isBatchResult {
			m.popPendingToolCall(msg.ToolCallID)
		}

		resultForLLM := msg.Result
		if msg.Err != nil {
			m.skills = m.skills.SetError(msg.Name, msg.Err)
			m.chat = m.chat.UpdateFunctionResult(msg.Name, fmt.Sprintf("Error: %v", msg.Err))

			// Format error as JSON for LLM to interpret
			errorMsg := strings.ReplaceAll(msg.Err.Error(), `"`, `\"`)
			errorMsg = strings.ReplaceAll(errorMsg, "\n", "\\n")
			resultForLLM = fmt.Sprintf(`{"error": true, "message": "%s", "skill": "%s"}`, errorMsg, msg.Name)
		} else {
			m.skills = m.skills.SetCompleted(msg.Name)
			m.chat = m.chat.UpdateFunctionResult(msg.Name, msg.Result)

			// Handle NSFW mode toggle
			if msg.Name == "nsfw_mode" && strings.Contains(msg.Result, "enabled") {
				m.nsfwMode = true
				m.header = m.header.SetNSFWMode(true)
				m.persistSession()
			} else if msg.Name == "nsfw_mode" && strings.Contains(msg.Result, "disabled") {
				m.nsfwMode = false
				m.header = m.header.SetNSFWMode(false)
				m.persistSession()
			}
		}

		if m.llmClient != nil && msg.ToolCallID != "" {
			m.chat = m.chat.AddToolResult(msg.ToolCallID, msg.Name, resultForLLM)
		}

		if isBatchResult {
			if len(m.pendingToolCalls) > 0 {
				nextCall := m.pendingToolCalls[0]
				m.skills = m.skills.SetExecuting(nextCall.name)
				m.status = m.status.SetText(fmt.Sprintf("⚡ Executing: %s", nextCall.name))
				nextCmd := m.executePendingToolCall(nextCall)
				if nextCmd != nil {
					cmds = append(cmds, nextCmd)
				} else {
					m.pendingToolCalls = nil
					m.toolBatchActive = false
				}
			} else {
				m.pendingToolCallID = ""
				m.toolBatchActive = false
				if shouldFollowUp {
					var followCmds []tea.Cmd
					m, followCmds = m.buildToolFollowUpCmds()
					cmds = append(cmds, followCmds...)
				}
			}
		} else {
			m.pendingToolCallID = ""
			if shouldFollowUp {
				var followCmds []tea.Cmd
				m, followCmds = m.buildToolFollowUpCmds()
				cmds = append(cmds, followCmds...)
			}
		}

	case ShowSelectorMsg:
		// Activate the selector
		m.selector = NewSelectorModel(msg.Title, msg.Items)
		m.selector = m.selector.SetHeight(m.height - 4) // Leave room for borders/footer
		m.selector = m.selector.SetWidth(m.width)
		m.selectorActive = true

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

					// Check provider capabilities for the current provider
					if m.provider != "" {
						if caps, ok := providers.GetProvider(m.provider); ok {
							// Check if provider supports function calling
							if !caps.SupportsFunctionCalling {
								m.chat = m.chat.AddSystemMessage(fmt.Sprintf("⚠️ Warning: Provider '%s' does not support function calling. Skills will be unavailable.", m.provider))
								m.skillsEnabled = false
								m.header = m.header.SetSkillsEnabled(false)
							}
						}
					}

					m.chat = m.chat.AddSystemMessage(fmt.Sprintf("🤖 Model changed to: %s", modelName))
					m.status = m.status.SetText(fmt.Sprintf("Model changed to: %s", modelName))

					// Persist the change
					m.persistSession()
				}
			}
		}

	case SimulateTypingMsg:
		// For simulated streaming (when endpoint dumps all at once)
		displayed := msg.Content[:msg.CharsToShow]
		m.chat = m.chat.SetLastAssistantContent(displayed)
		if msg.CharsToShow < len(msg.Content) {
			// Schedule next typing tick
			cmds = append(cmds, Tick(typingDelay))
		} else {
			m.streaming = false
			m.status = m.status.SetStreaming(false)
		}

	case TickMsg:
		m.animFrame++

		// Handle simulated typing
		if m.typingContent != "" && m.typingPos < len(m.typingContent) {
			// Advance typing position
			m.typingPos += charsPerTick
			if m.typingPos > len(m.typingContent) {
				m.typingPos = len(m.typingContent)
			}

			// Update chat with current typed content + corruption at cursor
			displayed := m.typingContent[:m.typingPos]

			// Check if content contains code blocks and apply corrupted-typing effect
			if strings.Contains(m.typingContent, "```") {
				// Calculate corruption intensity based on typing position (fade out as we type)
				progressRatio := float64(m.typingPos) / float64(len(m.typingContent))
				corruptionIntensity := 0.15 * (1 - progressRatio) // Start at 15%, fade to 0%

				// Apply code block corruption with fading intensity
				displayed = ApplyCodeBlockCorruption(displayed, m.typingPos, corruptionIntensity)
			}

			if m.typingPos < len(m.typingContent) {
				// Add corruption effect at typing cursor
				displayed += GetRandomCorruption()
			}
			m.chat = m.chat.SetLastAssistantContent(displayed)

			// Update status with corrupted animation
			m.status = m.status.SetText(StreamingSpinner(m.animFrame) + " " + ThinkingAnimation(m.animFrame))

			if m.typingPos < len(m.typingContent) {
				// Schedule next typing tick
				cmds = append(cmds, tea.Tick(typingTickInterval, func(t time.Time) tea.Msg {
					return TickMsg{Time: t}
				}))
			} else {
				// Typing complete - show final content without corruption
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

				// Persist session now that the message is complete
				m.persistSession()
			}
		} else if m.streaming {
			// Just streaming (waiting for response) - show animated status
			m.status = m.status.SetText(StreamingSpinner(m.animFrame) + " " + ThinkingAnimation(m.animFrame))
			cmds = append(cmds, tea.Tick(typingTickInterval*2, func(t time.Time) tea.Msg {
				return TickMsg{Time: t}
			}))
		}

	case NSFWToggleMsg:
		m.nsfwMode = msg.Enabled
		m.header = m.header.SetNSFWMode(msg.Enabled)
		m.persistSession()

	case ErrorMsg:
		m.status = m.status.SetText(fmt.Sprintf("Error: %v", msg.Err))
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

	if m.splitPanelMode && m.splitPanel != nil {
		return lipgloss.JoinVertical(lipgloss.Left,
			m.header.View(),
			m.splitPanel.View(),
			m.status.View(),
			m.input.View(),
		)
	}

	// Build the layout vertically
	var sections []string

	// Header (fixed, 1 line)
	sections = append(sections, m.header.View())

	// Chat panel (flexible height)
	sections = append(sections, m.chat.View())

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
	sections = append(sections, m.skills.View())

	// Status bar (fixed, 1 line)
	sections = append(sections, m.status.View())

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// SetLLMClient sets the LLM client.
func (m AppModel) SetLLMClient(client LLMClient) AppModel {
	m.llmClient = client
	// Reset skills panel runtime state when swapping clients.
	m.skills = NewSkillsModel()
	return m
}

func (m AppModel) getAvailableSkills() []SkillDefinition {
	if m.llmClient == nil {
		return nil
	}
	return m.llmClient.GetSkills()
}

func (m AppModel) getToolsForDispatch() []SkillDefinition {
	if m.nsfwMode || !m.skillsEnabled {
		return nil
	}
	return m.getAvailableSkills()
}

func (m AppModel) isClawMode() bool {
	return m.runtimeMode == config.RuntimeModeClaw
}

func (m AppModel) handleSkillCallBatch(msg SkillCallBatchMsg) (AppModel, []tea.Cmd) {
	if len(msg.Calls) == 0 {
		return m, nil
	}
	if m.isClawMode() && m.clawToolIterations >= m.clawMaxIterations {
		LogInfo(fmt.Sprintf("Claw safety stop reached (%d tool-call turns)", m.clawMaxIterations))
		m.streaming = false
		m.status = m.status.SetStreaming(false)
		m.status = m.status.SetText(fmt.Sprintf("Claw safety stop reached (%d)", m.clawMaxIterations))
		m.chat = m.chat.AddSystemMessage(
			fmt.Sprintf("⚠️ Claw mode stopped repeated tool calls after %d turn(s). Start a new prompt or raise --set-claw-max-iterations.", m.clawMaxIterations),
		)
		return m, nil
	}
	m.clawToolIterations++

	m.chat = m.chat.AddAssistantMessageWithToolCalls(msg.AssistantContent, msg.ToolCalls)
	m.pendingToolCalls = make([]pendingToolCall, 0, len(msg.Calls))
	m.toolBatchActive = true

	for _, call := range msg.Calls {
		LogSkillCall(call.Call.Name, call.Call.Arguments)
		m.chat = m.chat.AddFunctionCall(call.Call)
		m.pendingToolCalls = append(m.pendingToolCalls, pendingToolCall{
			name:       call.Call.Name,
			args:       call.Call.Arguments,
			toolCallID: call.ToolCallID,
			parseError: call.ParseError,
		})
	}

	firstCall := m.pendingToolCalls[0]
	m.pendingToolCallID = firstCall.toolCallID
	m.skills = m.skills.SetExecuting(firstCall.name)
	m.status = m.status.SetText(fmt.Sprintf("⚡ Executing: %s", firstCall.name))
	LogInfo(fmt.Sprintf("Starting execution of %d skill call(s)", len(m.pendingToolCalls)))

	nextCmd := m.executePendingToolCall(firstCall)
	if nextCmd == nil {
		m.pendingToolCalls = nil
		m.toolBatchActive = false
		return m, nil
	}

	return m, []tea.Cmd{nextCmd}
}

func (m AppModel) executePendingToolCall(call pendingToolCall) tea.Cmd {
	if call.parseError != "" {
		parseErr := call.parseError
		return func() tea.Msg {
			return SkillResultMsg{
				Name:       call.name,
				Result:     "",
				Err:        fmt.Errorf("failed to parse tool arguments: %s", parseErr),
				ToolCallID: call.toolCallID,
			}
		}
	}

	if m.llmClient == nil {
		return nil
	}

	return m.llmClient.ExecuteSkill(call.name, call.args, call.toolCallID)
}

func (m *AppModel) popPendingToolCall(toolCallID string) {
	if len(m.pendingToolCalls) == 0 {
		return
	}

	if toolCallID == "" {
		m.pendingToolCalls = m.pendingToolCalls[1:]
		return
	}

	for i, call := range m.pendingToolCalls {
		if call.toolCallID == toolCallID {
			m.pendingToolCalls = append(m.pendingToolCalls[:i], m.pendingToolCalls[i+1:]...)
			return
		}
	}

	// Fallback: dequeue the head if the ID wasn't found.
	m.pendingToolCalls = m.pendingToolCalls[1:]
}

func (m AppModel) buildToolFollowUpCmds() (AppModel, []tea.Cmd) {
	if m.llmClient == nil {
		return m, nil
	}

	m.streaming = true
	m.status = m.status.SetStreaming(true)
	m.status = m.status.SetText(StreamingSpinner(0) + " " + ThinkingAnimation(0))
	// Reset timing for the follow-up LLM call so the displayed stats reflect
	// only that call's latency, not the elapsed tool execution time.
	m.streamStart = time.Now()
	m.lastMsgInTok = 0
	m.lastMsgOutTok = 0

	toolsToSend := m.getToolsForDispatch()
	return m, []tea.Cmd{
		m.llmClient.SendMessage(m.chat.GetLLMMessages(), toolsToSend),
		tea.Tick(typingTickInterval*2, func(t time.Time) tea.Msg {
			return TickMsg{Time: t}
		}),
	}
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
	SetName(name string)
	ClearMessages()
	GetMessagesRaw() interface{}     // Returns []SessionMessage
	SetMessagesRaw(msgs interface{}) // Accepts []SessionMessage
	SummarizeRaw() interface{}       // Returns SessionSummary
	SetCommandHistory(history []string)
	GetCommandHistory() []string
}

// SessionMessage represents a message stored in session (matches config.SessionMessage).
type SessionMessage struct {
	Role      string
	Content   string
	Timestamp time.Time
}

// SessionSummary represents session metadata (matches config.SessionSummary).
// Duplicated here to avoid circular import with config package.
type SessionSummary struct {
	ID           string
	Name         string
	MessageCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	FirstMessage string
	Metadata     map[string]interface{}
}

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
			m.header = m.header.SetModel(model)

			// Initialize context tracker with session and model
			// Convert Session interface to *config.Session for ContextTracker
			if configSession, ok := session.(*config.Session); ok {
				// Pass config's ContextLimit as override if available
				if m.config != nil && m.config.ContextLimit > 0 {
					m.contextTracker = config.NewContextTracker(configSession, model, m.config.ContextLimit)
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
func (m AppModel) SetConfig(cfg *config.Config) AppModel {
	m.config = cfg
	if cfg == nil {
		m.runtimeMode = config.RuntimeModeClassic
		m.clawMaxIterations = config.DefaultClawMaxToolIterations
		return m
	}

	m.runtimeMode = config.NormalizeRuntimeMode(cfg.RuntimeMode)
	if cfg.ClawMaxToolIterations > 0 {
		m.clawMaxIterations = cfg.ClawMaxToolIterations
	} else {
		m.clawMaxIterations = config.DefaultClawMaxToolIterations
	}
	return m
}

// WithMessages restores chat history from session messages.
func (m AppModel) WithMessages(messages []ChatMessage) AppModel {
	// Restore all messages first
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			m.chat = m.chat.AddUserMessage(msg.Content)
		case "assistant":
			m.chat = m.chat.AddAssistantMessage(msg.Content)
		case "tool":
			m.chat = m.chat.AddToolResult(msg.ToolCallID, msg.Name, msg.Content)
		}
	}

	// Add a system message at the end indicating session was resumed
	if len(messages) > 0 {
		m.chat = m.chat.AddSystemMessage(fmt.Sprintf("📂 Resumed session (%d messages)", len(messages)))
	}

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
			m.skillsEnabled = caps.SupportsFunctionCalling
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

	m.currentSession.SetEndpoint(m.endpoint)
	m.currentSession.SetModel(m.model)
	m.currentSession.SetNSFWMode(m.nsfwMode)
	if hist := m.input.GetHistory(); len(hist) > 0 {
		m.currentSession.SetCommandHistory(hist)
	}

	// Convert TUI ChatMessages to config SessionMessages
	chatMsgs := m.chat.GetMessages()
	sessionMsgs := make([]SessionMessage, 0, len(chatMsgs))
	for _, msg := range chatMsgs {
		// Skip system messages (UI-only, not part of LLM conversation)
		if msg.Role == "system" {
			continue
		}
		sessionMsgs = append(sessionMsgs, SessionMessage{
			Role:      msg.Role,
			Content:   msg.Content,
			Timestamp: msg.Timestamp,
		})
	}
	m.currentSession.SetMessagesRaw(sessionMsgs)

	// Save asynchronously (ignore errors for now)
	go func() {
		_ = m.sessionManager.Save(m.currentSession)
	}()
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
			// TODO: Set name through metadata if action.Name is provided
			// config.Session doesn't currently have a SetName method
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
				m.currentSession = s

				// Clear current chat
				m.chat = m.chat.Clear()

				// Restore messages
				if messagesRaw := s.GetMessagesRaw(); messagesRaw != nil {
					if sessionMsgs, ok := messagesRaw.([]SessionMessage); ok {
						for _, msg := range sessionMsgs {
							switch msg.Role {
							case "user":
								m.chat = m.chat.AddUserMessage(msg.Content)
							case "assistant":
								m.chat = m.chat.AddAssistantMessage(msg.Content)
							}
						}
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
					if sm, ok := msgs.([]SessionMessage); ok {
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
							// Convert config.SessionSummary to tui.SessionSummary
							if configSummary, ok := summaryRaw.(config.SessionSummary); ok {
								tuiSummary := SessionSummary{
									ID:           configSummary.ID,
									Name:         configSummary.Name,
									MessageCount: configSummary.MessageCount,
									CreatedAt:    configSummary.CreatedAt,
									UpdatedAt:    configSummary.UpdatedAt,
									FirstMessage: configSummary.FirstMessage,
									Metadata:     make(map[string]interface{}),
								}
								// Copy metadata
								for k, v := range configSummary.Metadata {
									tuiSummary.Metadata[k] = v
								}
								summaries = append(summaries, tuiSummary)
							}
						}
					}
				}

				// Sort by UpdatedAt descending (most recent first)
				sort.Slice(summaries, func(i, j int) bool {
					return summaries[i].UpdatedAt.After(summaries[j].UpdatedAt)
				})

				var sb strings.Builder
				sb.WriteString(fmt.Sprintf("\n📋 Saved Sessions (%d):\n\n", len(summaries)))

				// Get current session ID for comparison
				var currentSessionID string
				if m.currentSession != nil {
					if currentSummaryRaw := m.currentSession.SummarizeRaw(); currentSummaryRaw != nil {
						if configSummary, ok := currentSummaryRaw.(config.SessionSummary); ok {
							currentSessionID = configSummary.ID
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
					sb.WriteString(fmt.Sprintf("• %s%s (%s)\n", currentMarker, displayName, summary.ID))
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
		m.chat = m.chat.AddSystemMessage("🗑️  Session cleared, new session started")

	case "merge":
		if toMerge, err := m.sessionManager.Load(action.SessionID); err == nil {
			merged := m.sessionManager.MergeSessions(m.currentSession, toMerge)
			if s, ok := merged.(Session); ok {
				m.currentSession = s

				// Clear and reload with merged messages
				m.chat = m.chat.Clear()
				if messagesRaw := s.GetMessagesRaw(); messagesRaw != nil {
					if sessionMsgs, ok := messagesRaw.([]SessionMessage); ok {
						for _, msg := range sessionMsgs {
							switch msg.Role {
							case "user":
								m.chat = m.chat.AddUserMessage(msg.Content)
							case "assistant":
								m.chat = m.chat.AddAssistantMessage(msg.Content)
							}
						}

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
				if sm, ok := msgs.([]SessionMessage); ok {
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

// SetShowContext controls whether context usage is displayed.
func (m HeaderModel) SetShowContext(show bool) HeaderModel {
	m.showContext = show
	return m
}

// GetContextWarningLevel returns the current context warning level.
func (m HeaderModel) GetContextWarningLevel() string {
	return m.contextIndicator.GetWarningLevel()
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

// ShowContextWarning displays a context warning message.
func (m StatusModel) ShowContextWarning(level string, message string) StatusModel {
	m.warningLevel = level
	m.warningMessage = message
	m.showWarning = true
	return m
}

// ClearContextWarning clears the context warning.
func (m StatusModel) ClearContextWarning() StatusModel {
	m.showWarning = false
	m.warningMessage = ""
	m.warningLevel = ""
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
		// Animated spinner
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		spinner := StatusStreamingStyle.Render(frames[m.frame%len(frames)])
		status = spinner + " " + StatusStreamingStyle.Render("Streaming...")
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

// Run starts the TUI application.
func Run(llmClient LLMClient) error {
	p := tea.NewProgram(
		NewApp(llmClient),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	_, err := p.Run()
	return err
}

// Typing delay for simulated streaming (40 chars/sec = 25ms per char)
const typingDelay = 25 * 1000000 // 25ms in nanoseconds

func truncateText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
