# Architecture Documentation

Comprehensive system architecture for Celeste CLI.

## Table of Contents

- [System Overview](#system-overview)
- [Component Architecture](#component-architecture)
- [Data Flow](#data-flow)
- [Provider System](#provider-system)
- [Skills System](#skills-system)
- [TUI Component](#tui-component)
- [Session Management](#session-management)
- [Configuration System](#configuration-system)

---

## System Overview

Celeste CLI is a terminal-based AI assistant that provides an interactive chat interface with function calling capabilities (skills), multi-provider LLM support, and persistent session management.

### Key Features

- **Multi-Provider Support**: OpenAI, Grok, Venice.ai, Anthropic, Gemini, etc.
- **Function Calling**: 18 built-in skills (weather, currency, QR codes, etc.)
- **Interactive TUI**: Bubble Tea-based terminal interface
- **Session Persistence**: Save and resume conversations
- **Streaming Responses**: Real-time LLM output
- **NSFW Mode**: Uncensored content generation via Venice.ai
- **Content Generation**: Platform-specific content (Twitter, TikTok, YouTube)

### Technology Stack

- **Language**: Go 1.21+
- **TUI Framework**: Bubble Tea + Lip Gloss
- **HTTP Client**: net/http with streaming support
- **Testing**: testify/assert + testify/require
- **Configuration**: JSON-based config files

---

## Component Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                        User Interface                        │
│  ┌──────────────────────────────────────────────────────┐  │
│  │              Bubble Tea TUI (cmd/celeste/tui/)       │  │
│  │  - Chat view (chat.go)                               │  │
│  │  - Skills view (skills.go)                           │  │
│  │  - Streaming handler (streaming.go)                  │  │
│  │  - Styles & themes (styles.go)                       │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            ↕
┌─────────────────────────────────────────────────────────────┐
│                      Command Layer                           │
│  ┌──────────────────────────────────────────────────────┐  │
│  │        Command Parser (cmd/celeste/commands/)        │  │
│  │  - Command definitions (commands.go)                 │  │
│  │  - Provider commands (providers.go)                  │  │
│  │  - Context management (context.go)                   │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            ↕
┌─────────────────────────────────────────────────────────────┐
│                      Business Logic                          │
│  ┌──────────────────────────────────────────────────────┐  │
│  │        LLM Client (cmd/celeste/llm/)                 │  │
│  │  - Chat completion (client.go)                       │  │
│  │  - Streaming (client.go)                             │  │
│  │  - Function calling (client.go)                      │  │
│  │  - Context summarization (summarize.go)              │  │
│  └──────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │        Skills System (cmd/celeste/skills/)           │  │
│  │  - Registry (registry.go)                            │  │
│  │  - Built-in skills (builtin.go)                      │  │
│  │  - Executor (executor.go)                            │  │
│  └──────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │        Provider Registry (cmd/celeste/providers/)    │  │
│  │  - Provider registry (registry.go)                   │  │
│  │  - Model detection (models.go)                       │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            ↕
┌─────────────────────────────────────────────────────────────┐
│                      Data & Config                           │
│  ┌──────────────────────────────────────────────────────┐  │
│  │        Configuration (cmd/celeste/config/)           │  │
│  │  - Config management (config.go)                     │  │
│  │  - Session storage (session.go)                      │  │
│  │  - Export/import (export.go)                         │  │
│  └──────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │        Prompts (cmd/celeste/prompts/)                │  │
│  │  - Persona essence (celeste.go)                      │  │
│  │  - System prompts (celeste.go)                       │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                            ↕
┌─────────────────────────────────────────────────────────────┐
│                      External Services                       │
│  - OpenAI API                                               │
│  - Grok (xAI) API                                           │
│  - Venice.ai API (NSFW/media)                               │
│  - Anthropic API                                            │
│  - Gemini API                                               │
│  - Weather API, Currency API, etc.                          │
└─────────────────────────────────────────────────────────────┘
```

---

## Data Flow

### Basic Chat Flow

```
1. User types message
   ↓
2. TUI captures input (tui/chat.go)
   ↓
3. Check for slash command (commands/commands.go)
   ├─ Yes → Execute command → Update UI
   └─ No → Continue to LLM
   ↓
4. Build request with system prompt (prompts/celeste.go)
   ↓
5. Add skills as tools (skills/registry.go)
   ↓
6. Send to LLM client (llm/client.go)
   ↓
7. Stream response chunks
   ↓
8. Check for tool calls
   ├─ Yes → Execute skills → Send results back
   └─ No → Display response
   ↓
9. Save to session (config/session.go)
```

### Function Call Flow

```
User: "What's the weather in NYC?"
   ↓
1. LLM receives message + skill definitions
   ↓
2. LLM decides to call "get_weather" skill
   ↓
3. LLM returns tool_call:
   {
     "name": "get_weather",
     "arguments": {"location": "NYC"}
   }
   ↓
4. Skills executor finds handler (skills/executor.go)
   ↓
5. Handler makes API call (skills/builtin.go)
   ↓
6. Result returned: {"temperature": 45, "condition": "cloudy"}
   ↓
7. Result sent back to LLM with role: "tool"
   ↓
8. LLM generates natural response: "It's 45°F and cloudy in NYC..."
   ↓
9. Response displayed to user
```

### Provider Detection Flow

```
1. Config loaded with base_url (config/config.go)
   ↓
2. DetectProvider(baseURL) called (providers/registry.go)
   ↓
3. URL pattern matching:
   - "api.openai.com" → openai
   - "api.x.ai" → grok
   - "api.venice.ai" → venice
   - etc.
   ↓
4. Provider capabilities retrieved
   ↓
5. Model recommendations based on provider
   ↓
6. Function calling support determined
```

---

## Provider System

Located in `cmd/celeste/providers/`.

### Design Philosophy

Centralized provider registry with capability-based detection.

### Components

**1. Provider Registry** (`registry.go`):

```go
type ProviderCapabilities struct {
    Name                      string
    BaseURL                   string
    DefaultModel              string
    PreferredToolModel        string
    SupportsFunctionCalling   bool
    SupportsModelListing      bool
    SupportsTokenTracking     bool
    IsOpenAICompatible        bool
    RequiresAPIKey            bool
}

// Registry maps provider names to capabilities
var providerRegistry = map[string]ProviderCapabilities{
    "openai": {
        Name:                    "openai",
        BaseURL:                 "https://api.openai.com/v1",
        DefaultModel:            "gpt-4o-mini",
        PreferredToolModel:      "gpt-4o-mini",
        SupportsFunctionCalling: true,
        SupportsModelListing:    true,
        SupportsTokenTracking:   true,
        IsOpenAICompatible:      true,
        RequiresAPIKey:          true,
    },
    // ... 8 more providers
}
```

**2. Model Detection** (`models.go`):

- Static model lists per provider
- Best tool model recommendations
- Model capability detection (function calling support)

**3. Provider Detection** (`registry.go:DetectProvider()`):

- URL pattern matching
- Fallback to "openai" for unknown URLs
- Case-insensitive detection

### Usage

```go
// Detect provider from URL
provider := providers.DetectProvider("https://api.x.ai/v1")
// Returns: "grok"

// Get capabilities
caps, ok := providers.GetProvider("grok")
if caps.SupportsFunctionCalling {
    // Use with skills
}

// List all tool-capable providers
toolProviders := providers.GetToolCallingProviders()
// Returns: ["openai", "grok", "venice", ...]
```

---

## Skills System

Located in `cmd/celeste/skills/`.

### Design Philosophy

Registry-based skill system with OpenAI function calling format.

### Components

**1. Skill Registry** (`registry.go`):

```go
type Skill struct {
    Name        string
    Description string
    Parameters  map[string]interface{} // JSON schema
}

type Registry struct {
    skills   map[string]Skill
    handlers map[string]SkillHandler
}
```

**2. Built-in Skills** (`builtin.go`):

18 skills across categories:
- **Utilities**: UUID, password generation, base64, hashing
- **APIs**: Weather, currency, Twitch, YouTube
- **Media**: QR codes, image generation
- **Personal**: Notes, reminders
- **Mystical**: Tarot reading

**3. Skill Executor** (`executor.go`):

- Parses OpenAI tool calls
- Executes handlers with arguments
- Formats results for LLM

### Skill Definition Pattern

```go
func WeatherSkill() Skill {
    return Skill{
        Name:        "get_weather",
        Description: "Get current weather for a location",
        Parameters: map[string]interface{}{
            "type": "object",
            "properties": map[string]interface{}{
                "location": map[string]interface{}{
                    "type":        "string",
                    "description": "City name or zip code",
                },
            },
            "required": []string{"location"},
        },
    }
}

func WeatherHandler(args map[string]interface{}) (interface{}, error) {
    location := args["location"].(string)
    // Make API call
    // Return structured result
}
```

### Tool Definition Format

Skills are converted to OpenAI's function calling format:

```json
{
  "type": "function",
  "function": {
    "name": "get_weather",
    "description": "Get current weather for a location",
    "parameters": {
      "type": "object",
      "properties": {
        "location": {
          "type": "string",
          "description": "City name or zip code"
        }
      },
      "required": ["location"]
    }
  }
}
```

---

## TUI Component

Located in `cmd/celeste/tui/`.

### Bubble Tea Architecture

Follows The Elm Architecture (TEA):

```
┌─────────────────────────────────────┐
│           Model (State)              │
│  - Messages                          │
│  - Input buffer                      │
│  - Viewport                          │
│  - Skills list                       │
│  - Current view                      │
└─────────────────────────────────────┘
         ↓                 ↑
    ┌────────┐        ┌────────┐
    │ Update │        │  View  │
    └────────┘        └────────┘
         ↑                 ↓
    ┌────────┐        ┌────────┐
    │  Msg   │        │ String │
    └────────┘        └────────┘
```

### Components

**1. App Model** (`app.go`):

```go
type App struct {
    config      *Config
    llmClient   *llm.Client
    messages    []Message
    input       textinput.Model
    viewport    viewport.Model
    skills      []Skill
    currentView View
    streaming   bool
}
```

**2. Message Types**:

- User messages
- Assistant messages
- Tool calls (function execution)
- System messages
- Error messages

**3. Update Cycle** (`app.go:Update()`):

```go
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        // Handle keyboard input
    case StreamChunkMsg:
        // Handle LLM stream chunks
    case CompletionDoneMsg:
        // Handle completion end
    case ToolCallMsg:
        // Handle function calls
    }
    return a, cmd
}
```

**4. View Rendering** (`app.go:View()`):

- Message history viewport
- Input box at bottom
- Status indicators (thinking, streaming)
- Styled output (Lip Gloss)

---

## Session Management

Located in `cmd/celeste/config/`.

### Session Structure

```go
type Session struct {
    ID            string
    Name          string
    CreatedAt     time.Time
    UpdatedAt     time.Time
    Messages      []Message
    Provider      string
    Model         string
    BaseURL       string
    ContextWindow int
    TokenCount    int
}
```

### Storage Format

Sessions saved as JSON in `~/.celeste/sessions/`:

```
~/.celeste/
├── config.json
├── sessions/
│   ├── session_abc123.json
│   ├── session_def456.json
│   └── ...
└── skills/
    ├── custom_skill_1.json
    └── ...
```

### Session Operations

**1. Create Session** (`session.go:NewSession()`):
- Generate unique ID
- Set creation timestamp
- Initialize empty message history

**2. Save Session** (`session.go:Save()`):
- Marshal to JSON
- Write to `~/.celeste/sessions/session_{ID}.json`

**3. Load Session** (`session.go:Load()`):
- Read JSON file
- Unmarshal to Session struct
- Restore message history

**4. Export/Import** (`export.go`):
- Export sessions to custom location
- Import from external files
- Batch export/import

---

## Configuration System

Located in `cmd/celeste/config/`.

### Config Structure

```go
type Config struct {
    BaseURL         string
    Model           string
    APIKey          string
    ContextWindow   int
    SystemPrompt    string
    NSFWMode        bool
    SkipPrompt      bool
    SessionID       string
    Temperature     float64
    TopP            float64
}
```

### Config File

Location: `~/.celeste/config.json`

```json
{
  "base_url": "https://api.openai.com/v1",
  "model": "gpt-4o-mini",
  "api_key": "sk-...",
  "context_window": 128000,
  "system_prompt": "",
  "nsfw_mode": false,
  "skip_prompt": false,
  "temperature": 0.7,
  "top_p": 1.0
}
```

### Config Loading Priority

1. Command-line flags
2. Environment variables (`OPENAI_API_KEY`, etc.)
3. Config file (`~/.celeste/config.json`)
4. Default values

### Provider-Specific Configs

Some features require provider-specific config:

```
~/.celeste/
├── config.json          # Main config
├── venice_config.json   # Venice.ai API key + media settings
├── weather_config.json  # Weather API key
├── twitch_config.json   # Twitch credentials
└── youtube_config.json  # YouTube API key
```

---

## Key Design Patterns

### 1. Registry Pattern

Used for:
- Provider registry (providers/)
- Skill registry (skills/)

Benefits:
- Centralized registration
- Easy extension
- Capability-based querying

### 2. Strategy Pattern

Used for:
- Provider selection (different APIs, same interface)
- Model selection (best for task)

### 3. Observer Pattern

Used for:
- Streaming responses (TUI observes LLM chunks)
- State updates (Bubble Tea message loop)

### 4. Command Pattern

Used for:
- Slash commands (/help, /providers, /clear)
- Skill execution

---

## Extension Points

### Adding a New Provider

1. Add to `providers/registry.go`:

```go
"newprovider": {
    Name:                    "newprovider",
    BaseURL:                 "https://api.newprovider.com/v1",
    DefaultModel:            "model-name",
    SupportsFunctionCalling: true,
    IsOpenAICompatible:      true,
    RequiresAPIKey:          true,
},
```

2. Add URL detection in `DetectProvider()`
3. Add model list in `models.go`
4. Test with integration tests

### Adding a New Skill

1. Define skill in `skills/builtin.go`:

```go
func NewSkill() Skill {
    return Skill{
        Name:        "new_skill",
        Description: "Description",
        Parameters:  /* JSON schema */,
    }
}
```

2. Implement handler:

```go
func NewSkillHandler(args map[string]interface{}) (interface{}, error) {
    // Implementation
}
```

3. Register in `RegisterBuiltinSkills()`:

```go
registry.RegisterSkill(NewSkill())
registry.RegisterHandler("new_skill", NewSkillHandler)
```

### Adding a New Command

1. Add to `commands/commands.go`:

```go
case "newcmd":
    return handleNewCommand(cmd, ctx)
```

2. Implement handler:

```go
func handleNewCommand(cmd *Command, ctx *CommandContext) *CommandResult {
    // Implementation
}
```

3. Add tests in `commands_test.go`

---

## Performance Considerations

### Streaming

- All LLM responses use streaming
- Reduces perceived latency
- Better UX for long responses

### Context Management

- Automatic summarization when context window fills
- Keeps recent messages, summarizes old ones
- Configurable context window per provider

### Caching

- Session files cached in memory during chat
- Config loaded once at startup
- Provider capabilities cached in registry

---

## Security Considerations

### API Key Storage

- Stored in `~/.celeste/config.json` (permissions: 0600)
- Never logged or displayed
- Can use environment variables instead

### Skill Execution

- Skills run in same process (no sandboxing)
- Trust model: user-controlled skills directory
- Validate skill inputs before execution

### Network Requests

- All HTTPS by default
- Streaming over persistent connections
- Timeout configurations

---

## Testing Strategy

### Unit Tests

- **Providers**: Registry, model detection, capabilities
- **Skills**: Registration, tool definitions, parameter schemas
- **Commands**: Parsing, execution, state changes
- **Prompts**: Persona loading, system prompt generation
- **Venice**: Media parsing, file handling

### Integration Tests

- **Provider APIs**: Real API calls (gated by API keys)
- **Skills**: With mocked external dependencies
- **End-to-end**: Full chat flow (requires HTTP mocking)

### Test Coverage

- Target: 20%+ (achieved: 17.4%)
- Critical packages: >70% (prompts, providers)
- Feature packages: >20% (commands, skills, venice)
- Infrastructure: Requires mocking (llm, tui)

---

## Future Architecture Improvements

1. **Plugin System**: Load skills from external binaries
2. **HTTP Mocking**: Test llm package without real APIs
3. **TUI Testing**: Bubble Tea test framework
4. **Parallel Requests**: Concurrent skill execution
5. **Caching Layer**: Cache LLM responses for similar queries
6. **Native Provider APIs**: Direct integration (Anthropic, Gemini)
7. **Multi-Agent**: Specialist agents for different tasks

---

## Further Reading

- [Provider Documentation](./LLM_PROVIDERS.md)
- [Testing Guide](./TESTING.md)
- [Contributing Guide](./CONTRIBUTING.md)
- [Bubble Tea Docs](https://github.com/charmbracelet/bubbletea)
- [OpenAI Function Calling](https://platform.openai.com/docs/guides/function-calling)

---

**Last Updated**: December 14, 2024
**Version**: v1.2.0
