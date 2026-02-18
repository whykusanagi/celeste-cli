# Collections Support & xAI Enhancements Design

**Date:** 2026-02-17
**Branch:** `feature/collections-support`
**Status:** Approved
**Priority:** High

## Executive Summary

This design adds xAI Collections support to celeste-cli, enabling RAG (Retrieval-Augmented Generation) capabilities by allowing users to upload custom knowledge bases that the LLM can search during conversations. This brings celeste-cli to feature parity with celeste-tts-bot and celeste-discord-bot, both of which already use Collections extensively.

**Secondary Enhancements:**
- Add `web_search` and `x_search` built-in xAI tools
- Improve xAI-specific documentation

**Key Benefits:**
- Enable Celeste to access custom documentation, lore, and user-specific data
- Maintain existing tool calling implementation (no breaking changes)
- Provide interactive TUI for collection management
- Leverage proven patterns from existing projects

---

## Background

### Current State

**celeste-cli Tool Calling:**
- ✅ Uses OpenAI-compatible tool calling format
- ✅ Supports 21 built-in skills via function calling
- ✅ Works with OpenAI, Grok, Venice, Gemini, Vertex AI
- ✅ Properly handles assistant → tool → assistant conversation flow

**Other Projects:**
- **celeste-tts-bot:** Uses Collections extensively with upload scripts (`scripts/upload_collection.py`)
- **celeste-discord-bot:** Has tool execution framework

**Gap:** celeste-cli lacks Collections support for RAG capabilities.

### xAI Collections Overview

**What are Collections?**
- Document storage and semantic search system
- Supports MD, PDF, HTML, TXT formats (up to 10MB per file)
- Managed via separate Management API (`https://management-api.x.ai/v1`)
- Accessed at runtime via `collections_search` built-in tool

**How Collections Work:**
1. **Upload Phase:** Use Management API to create collections and upload documents
2. **Query Phase:** Enable `collections_search` tool in chat completions
3. **Search:** LLM automatically searches collections when it needs information
4. **Context:** Search results are injected into conversation context

**Reference Documentation:**
- [xAI Collections API](https://docs.x.ai/docs/collections-api)
- [Using Collections via API](https://docs.x.ai/docs/guides/using-collections/api)
- [Collections Search Tool](https://docs.x.ai/docs/guides/tools/collections-search-tool)

---

## Architecture

### Component Overview

```
celeste-cli/
├── cmd/celeste/
│   ├── collections/          [NEW]
│   │   ├── client.go         # Management API client
│   │   ├── manager.go        # Collection CRUD operations
│   │   └── uploader.go       # Document upload logic
│   ├── llm/
│   │   ├── backend_openai.go # Add collections_search tool
│   │   └── client.go         # Pass collection IDs to API
│   ├── tui/
│   │   ├── collections.go    [NEW] # Collections management view
│   │   ├── app.go            # Add collections mode toggle
│   │   └── styles.go         # Collections UI styles
│   ├── config/
│   │   └── config.go         # Add collections config fields
│   └── commands/
│       └── collections.go    [NEW] # CLI commands
└── docs/
    ├── plans/
    │   └── 2026-02-17-collections-support-design.md  [THIS FILE]
    └── COLLECTIONS.md        [NEW] # Usage guide
```

### Data Flow

#### 1. Upload Phase (One-Time Setup)

```
User → CLI Command → Collections Manager → Management API Client → xAI Management API
                                                                              ↓
User ← Config Updated ← Collection ID Stored ←────────────────────────────────┘
```

**Example:**
```bash
celeste collections create "celeste-lore" --description "Celeste personality and lore"
celeste collections upload <collection-id> data/lore/*.md
```

#### 2. Query Phase (Runtime)

```
User Message → TUI → LLM Backend → xAI Chat API (with collections_search tool)
                                           ↓
                                   LLM decides to search
                                           ↓
                                   xAI searches collections
                                           ↓
User ← TUI ← LLM Backend ← Results with context ←┘
```

#### 3. TUI Management (Interactive)

```
User → /collections command → Collections View
                                     ↓
                        List collections with metadata
                                     ↓
                        [Space] Toggle active/inactive
                                     ↓
                        Config updated automatically
```

### Configuration Schema

**Location:** `~/.celeste/config.json`

```json
{
  "api_key": "sk-...",
  "base_url": "https://api.x.ai/v1",
  "model": "grok-4-1-fast",

  "xai_management_api_key": "xai-token-...",

  "collections": {
    "enabled": true,
    "active_collections": [
      "collection_abc123",
      "collection_def456"
    ],
    "auto_enable": true
  },

  "xai_features": {
    "enable_web_search": false,
    "enable_x_search": false
  }
}
```

**Config Fields:**

| Field | Type | Description |
|-------|------|-------------|
| `xai_management_api_key` | string | Management API key for collection operations |
| `collections.enabled` | bool | Enable collections_search tool globally |
| `collections.active_collections` | []string | Collection IDs to search |
| `collections.auto_enable` | bool | Automatically enable when active_collections is set |
| `xai_features.enable_web_search` | bool | Enable Grok's web_search built-in tool |
| `xai_features.enable_x_search` | bool | Enable Grok's x_search (Twitter/X) tool |

---

## Component Details

### 1. Collections Client (`cmd/celeste/collections/client.go`)

**Responsibilities:**
- Authenticate with xAI Management API
- HTTP client wrapper for Collections endpoints
- Handle rate limiting and retries

**API Methods:**

```go
package collections

import (
    "bytes"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
    "time"
)

const managementAPIURL = "https://management-api.x.ai/v1"

type Client struct {
    managementAPIKey string
    baseURL          string
    httpClient       *http.Client
}

func NewClient(managementAPIKey string) *Client {
    return &Client{
        managementAPIKey: managementAPIKey,
        baseURL:          managementAPIURL,
        httpClient: &http.Client{
            Timeout: 60 * time.Second,
        },
    }
}

// Collection represents a collection
type Collection struct {
    ID          string    `json:"collection_id"`
    Name        string    `json:"collection_name"`
    Description string    `json:"description"`
    CreatedAt   time.Time `json:"created_at"`
    DocumentCount int     `json:"document_count,omitempty"`
}

// Document represents a document in a collection
type Document struct {
    FileID      string    `json:"file_id"`
    Name        string    `json:"name"`
    ContentType string    `json:"content_type"`
    Size        int64     `json:"size"`
    UploadedAt  time.Time `json:"uploaded_at"`
}

// Collection CRUD operations
func (c *Client) CreateCollection(name, description string) (string, error)
func (c *Client) ListCollections() ([]Collection, error)
func (c *Client) GetCollection(id string) (*Collection, error)
func (c *Client) DeleteCollection(id string) error

// Document operations
func (c *Client) UploadDocument(collectionID, name string, data []byte, contentType string) (string, error)
func (c *Client) ListDocuments(collectionID string) ([]Document, error)
func (c *Client) DeleteDocument(collectionID, fileID string) error
```

**Error Handling:**

```go
type CollectionsError struct {
    StatusCode int
    Message    string
    RequestID  string
}

func (e *CollectionsError) Error() string {
    return fmt.Sprintf("collections API error (status %d): %s", e.StatusCode, e.Message)
}
```

---

### 2. Collections Manager (`cmd/celeste/collections/manager.go`)

**Responsibilities:**
- High-level collection management
- Batch upload operations
- Local config persistence

**Key Methods:**

```go
package collections

import (
    "path/filepath"
    "github.com/whykusanagi/celesteCLI/cmd/celeste/config"
)

type Manager struct {
    client *Client
    config *config.Config
}

func NewManager(client *Client, cfg *config.Config) *Manager {
    return &Manager{
        client: client,
        config: cfg,
    }
}

// Batch operations
func (m *Manager) UploadDirectory(collectionID, dirPath string) ([]string, error) {
    // Find all supported files (.md, .txt, .pdf, .html)
    // Validate each file (size < 10MB, supported format)
    // Upload in parallel with progress reporting
    // Return list of uploaded file IDs
}

func (m *Manager) SyncCollection(collectionID, dirPath string) error {
    // Compare local files with remote documents
    // Upload new files, delete removed files
    // Update existing files if modified
}

// Config integration
func (m *Manager) EnableCollection(collectionID string) error {
    // Add to active_collections in config
    // Save config
}

func (m *Manager) DisableCollection(collectionID string) error {
    // Remove from active_collections in config
    // Save config
}

func (m *Manager) GetActiveCollections() ([]string, error) {
    return m.config.Collections.ActiveCollections, nil
}
```

**File Validation:**

```go
func ValidateDocument(path string) error {
    info, err := os.Stat(path)
    if err != nil {
        return err
    }

    // Check size limit (10MB)
    if info.Size() > 10*1024*1024 {
        return fmt.Errorf("file too large: %s (max 10MB)", humanize.Bytes(uint64(info.Size())))
    }

    // Check supported formats
    ext := strings.ToLower(filepath.Ext(path))
    supported := []string{".md", ".txt", ".pdf", ".html"}
    if !contains(supported, ext) {
        return fmt.Errorf("unsupported format: %s (supported: %v)", ext, supported)
    }

    return nil
}
```

---

### 3. CLI Commands (`cmd/celeste/commands/collections.go`)

**Command Structure:**

```bash
celeste collections <subcommand> [flags]

Subcommands:
  create <name>              Create a new collection
  list                       List all collections
  upload <id> <files...>     Upload documents to a collection
  delete <id>                Delete a collection
  enable <id>                Add collection to active set
  disable <id>               Remove collection from active set
  show <id>                  Show collection details and documents
```

**Examples:**

```bash
# Create collection
celeste collections create "celeste-lore" \
  --description "Celeste personality, lore, and character background"

# Upload files
celeste collections upload collection_abc123 docs/lore/*.md

# Upload directory (recursive)
celeste collections upload collection_abc123 docs/lore/ --recursive

# List collections
celeste collections list

# Enable for use in chat
celeste collections enable collection_abc123

# Show details
celeste collections show collection_abc123
```

**Implementation:**

```go
package commands

func CollectionsCommand(cfg *config.Config) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "collections",
        Short: "Manage xAI Collections for RAG",
        Long:  "Create, upload, and manage xAI Collections for document search",
    }

    cmd.AddCommand(
        collectionsCreateCommand(cfg),
        collectionsListCommand(cfg),
        collectionsUploadCommand(cfg),
        collectionsDeleteCommand(cfg),
        collectionsEnableCommand(cfg),
        collectionsDisableCommand(cfg),
        collectionsShowCommand(cfg),
    )

    return cmd
}
```

---

### 4. TUI Collections View (`cmd/celeste/tui/collections.go`)

**UI Layout:**

```
┌─ Collections Management ──────────────────────────────────────────┐
│                                                                    │
│  Active Collections (2):                                           │
│  ✓ celeste-lore          (15 docs, 2.3 MB)  [Created: 2 days ago]│
│  ✓ nikke-wiki            (42 docs, 8.1 MB)  [Created: 1 week ago]│
│                                                                    │
│  Available Collections (1):                                        │
│  ○ archived-logs         (120 docs, 15.2 MB) [Created: 1 mo ago] │
│                                                                    │
│  [↑/↓] Navigate  [Space] Toggle  [Enter] View  [D] Delete        │
│  [N] New  [U] Upload  [Q] Back to Chat                           │
└────────────────────────────────────────────────────────────────────┘
```

**Bubble Tea Model:**

```go
package tui

import (
    "github.com/charmbracelet/bubbles/viewport"
    tea "github.com/charmbracelet/bubbletea"
    "github.com/whykusanagi/celesteCLI/cmd/celeste/collections"
)

type CollectionsModel struct {
    collections    []collections.Collection
    activeIDs      map[string]bool
    cursor         int
    viewport       viewport.Model
    manager        *collections.Manager
    width, height  int
}

func NewCollectionsModel(manager *collections.Manager) CollectionsModel {
    return CollectionsModel{
        manager:   manager,
        activeIDs: make(map[string]bool),
        viewport:  viewport.New(80, 20),
    }
}

// Bubble Tea interface
func (m CollectionsModel) Init() tea.Cmd {
    return m.loadCollections()
}

func (m CollectionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case tea.KeyMsg:
        switch msg.String() {
        case "up", "k":
            if m.cursor > 0 {
                m.cursor--
            }
        case "down", "j":
            if m.cursor < len(m.collections)-1 {
                m.cursor++
            }
        case " ": // Toggle active/inactive
            return m, m.toggleCollection()
        case "enter": // View documents
            return m, m.viewDocuments()
        case "d", "D": // Delete collection
            return m, m.deleteCollection()
        case "q", "Q": // Back to chat
            return m, tea.Quit
        }
    }
    return m, nil
}

func (m CollectionsModel) View() string {
    // Render collections list with styles
    // Show active collections first with checkmarks
    // Then show available collections
    // Footer with keybindings
}
```

**Keybindings:**

| Key | Action |
|-----|--------|
| `↑/↓` or `k/j` | Navigate list |
| `Space` | Toggle active/inactive |
| `Enter` | View collection documents |
| `D` | Delete collection (with confirmation) |
| `N` | Create new collection |
| `U` | Upload files to selected collection |
| `Q` | Return to chat |

---

### 5. LLM Backend Integration

**Changes to `cmd/celeste/llm/backend_openai.go`:**

```go
// convertTools adds skill tools and xAI built-in tools
func (b *OpenAIBackend) convertTools(tools []tui.SkillDefinition) []openai.Tool {
    var result []openai.Tool

    // Add xAI built-in tools if configured
    if b.config.Collections != nil && b.config.Collections.Enabled {
        if len(b.config.Collections.ActiveCollections) > 0 {
            result = append(result, openai.Tool{
                Type: "collections_search",
                CollectionsSearch: &openai.CollectionsSearchTool{
                    CollectionIDs: b.config.Collections.ActiveCollections,
                },
            })
        }
    }

    // Add web_search if enabled
    if b.config.XAIFeatures != nil && b.config.XAIFeatures.EnableWebSearch {
        result = append(result, openai.Tool{
            Type: "web_search",
        })
    }

    // Add x_search if enabled
    if b.config.XAIFeatures != nil && b.config.XAIFeatures.EnableXSearch {
        result = append(result, openai.Tool{
            Type: "x_search",
        })
    }

    // Add user-defined function tools
    for _, tool := range tools {
        params, _ := json.Marshal(tool.Parameters)
        result = append(result, openai.Tool{
            Type: "function",
            Function: &openai.FunctionDefinition{
                Name:        tool.Name,
                Description: tool.Description,
                Parameters:  json.RawMessage(params),
            },
        })
    }

    return result
}
```

**Note:** Built-in tools (`collections_search`, `web_search`, `x_search`) are executed server-side by xAI. We don't need to handle their execution locally.

---

## Error Handling

### API Error Scenarios

| Error Type | HTTP Status | Handling Strategy | User Feedback |
|------------|-------------|------------------|---------------|
| **Unauthorized** | 401 | Prompt for Management API key | "⚠️ Management API key invalid or missing. Run: `celeste config --set-management-key <key>`" |
| **Collection Not Found** | 404 | Remove from active list, warn | "⚠️ Collection 'xyz' not found. Removed from active set." |
| **Document Too Large** | 413 | Skip file, continue batch | "⚠️ Skipped large_doc.pdf (>10MB limit)" |
| **Rate Limit** | 429 | Exponential backoff, retry | "⏳ Rate limited, retrying in 5s..." |
| **Server Error** | 500 | Retry 3x, then fail gracefully | "❌ xAI API unavailable. Try again later." |
| **Network Timeout** | - | Retry with longer timeout | "⏳ Upload taking longer than expected..." |

### Validation Rules

**Before Upload:**
- File size must be ≤ 10MB
- Format must be: `.md`, `.txt`, `.pdf`, `.html`
- Collection must exist and be accessible
- Management API key must be configured

**Before API Call:**
- At least one collection must be active
- Collections feature must be enabled in config
- Provider must support built-in tools (xAI only for now)

### Graceful Degradation

If Collections API is unavailable:
1. Log warning to stderr
2. Continue chat without collections_search tool
3. Show indicator in TUI header: "⚠️ Collections unavailable"
4. Don't block user from chatting

---

## Testing Strategy

### Unit Tests

**Collections Client (`cmd/celeste/collections/client_test.go`):**

```go
func TestClient_CreateCollection(t *testing.T) {
    // Mock HTTP server
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        assert.Equal(t, "POST", r.Method)
        assert.Equal(t, "/v1/collections", r.URL.Path)

        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{
            "collection_id": "collection_abc123",
        })
    }))
    defer server.Close()

    client := NewClient("test-key")
    client.baseURL = server.URL

    id, err := client.CreateCollection("test", "description")
    assert.NoError(t, err)
    assert.Equal(t, "collection_abc123", id)
}

func TestClient_UploadDocument(t *testing.T)
func TestClient_ListCollections(t *testing.T)
func TestClient_HandleRateLimit(t *testing.T)
func TestClient_HandleUnauthorized(t *testing.T)
```

**Collections Manager (`cmd/celeste/collections/manager_test.go`):**

```go
func TestManager_UploadDirectory(t *testing.T)
func TestManager_ValidateDocument(t *testing.T)
func TestManager_EnableDisableCollection(t *testing.T)
func TestManager_SyncCollection(t *testing.T)
```

**TUI Collections View (`cmd/celeste/tui/collections_test.go`):**

```go
func TestCollectionsModel_ToggleActive(t *testing.T)
func TestCollectionsModel_Navigation(t *testing.T)
func TestCollectionsModel_DeleteWithConfirmation(t *testing.T)
```

---

### Integration Tests

**Real API Tests (`cmd/celeste/collections/integration_test.go`):**

```go
// +build integration

func TestIntegration_CollectionsWorkflow(t *testing.T) {
    if os.Getenv("XAI_MANAGEMENT_API_KEY") == "" {
        t.Skip("Skipping integration test: XAI_MANAGEMENT_API_KEY not set")
    }

    client := NewClient(os.Getenv("XAI_MANAGEMENT_API_KEY"))

    // Create collection
    collectionID, err := client.CreateCollection("test-collection", "Integration test")
    require.NoError(t, err)
    defer client.DeleteCollection(collectionID)

    // Upload document
    testDoc := []byte("# Test Document\n\nThis is a test.")
    fileID, err := client.UploadDocument(collectionID, "test.md", testDoc, "text/markdown")
    require.NoError(t, err)

    // List documents
    docs, err := client.ListDocuments(collectionID)
    require.NoError(t, err)
    assert.Len(t, docs, 1)
    assert.Equal(t, "test.md", docs[0].Name)

    // Delete document
    err = client.DeleteDocument(collectionID, fileID)
    assert.NoError(t, err)
}
```

**Run Integration Tests:**

```bash
export XAI_MANAGEMENT_API_KEY="xai-token-..."
go test -tags=integration -v ./cmd/celeste/collections/
```

---

### Manual Testing Checklist

**CLI Commands:**
- [ ] Create collection: `celeste collections create "test"`
- [ ] Upload single file: `celeste collections upload <id> test.md`
- [ ] Upload directory: `celeste collections upload <id> docs/ --recursive`
- [ ] List collections: `celeste collections list`
- [ ] Show collection: `celeste collections show <id>`
- [ ] Enable collection: `celeste collections enable <id>`
- [ ] Disable collection: `celeste collections disable <id>`
- [ ] Delete collection: `celeste collections delete <id>`

**TUI:**
- [ ] Open collections view: `/collections` in chat
- [ ] Navigate list with arrow keys
- [ ] Toggle active/inactive with Space
- [ ] View documents with Enter
- [ ] Delete collection with D (confirm prompt)
- [ ] Return to chat with Q

**LLM Integration:**
- [ ] Verify collections_search tool appears in API request
- [ ] Ask question that triggers collection search
- [ ] Verify search results are used in response
- [ ] Test with multiple active collections
- [ ] Disable collections, verify tool is not sent

**Error Handling:**
- [ ] Invalid Management API key (401)
- [ ] Non-existent collection (404)
- [ ] File too large (413)
- [ ] Offline/API down
- [ ] Upload with unsupported format

---

## Implementation Plan

### Phase 1: Core Collections API (Primary)

**Goal:** Basic Collections management via CLI

**Tasks:**
1. Create `cmd/celeste/collections/` package
2. Implement `Client` with HTTP methods
3. Implement `Manager` with high-level operations
4. Add config fields to `cmd/celeste/config/config.go`
5. Create CLI commands in `cmd/celeste/commands/collections.go`
6. Write unit tests with mock HTTP server
7. Test CLI commands manually

**Deliverables:**
- Collections client and manager
- CLI commands working
- Unit tests passing
- Config integration complete

**Estimated Time:** 1-2 days

---

### Phase 2: LLM Integration

**Goal:** Enable collections_search tool in chat

**Tasks:**
1. Update `Config` struct with collections fields
2. Modify `backend_openai.go` to add collections_search tool
3. Pass collection IDs in API requests
4. Test with real xAI API
5. Write integration tests

**Deliverables:**
- Collections_search tool working in chat
- API requests include collection IDs
- Integration tests passing

**Estimated Time:** 1 day

---

### Phase 3: TUI Enhancement

**Goal:** Interactive collections management in TUI

**Tasks:**
1. Create `cmd/celeste/tui/collections.go`
2. Implement Bubble Tea model for collections view
3. Add keybindings for navigation and actions
4. Integrate with main TUI app
5. Add `/collections` command to open view
6. Style with Lip Gloss (corrupted theme)
7. Write TUI tests

**Deliverables:**
- Collections view accessible via `/collections`
- Toggle active/inactive works
- UI styled consistently
- Tests passing

**Estimated Time:** 1-2 days

---

### Phase 4: Documentation & Polish

**Goal:** Comprehensive documentation and examples

**Tasks:**
1. Write `docs/COLLECTIONS.md` usage guide
2. Update `README.md` with Collections section
3. Update `docs/LLM_PROVIDERS.md` with xAI Collections info
4. Add example workflows and screenshots
5. Update `CHANGELOG.md`
6. Code cleanup and refactoring

**Deliverables:**
- Complete documentation
- Updated README
- Example workflows
- Clean, well-documented code

**Estimated Time:** 0.5 days

---

### Phase 5: Secondary Features (web_search, x_search)

**Goal:** Enable xAI built-in tools

**Tasks:**
1. Add `XAIFeatures` config struct
2. Update `backend_openai.go` to include web_search/x_search
3. Add config flags: `--enable-web-search`, `--enable-x-search`
4. Document in LLM_PROVIDERS.md
5. Test with real API

**Deliverables:**
- web_search and x_search tools working
- Config flags functional
- Documentation updated

**Estimated Time:** 0.5 days

---

**Total Estimated Time:** 5-6 days

---

## Documentation Updates

### New Documentation

**`docs/COLLECTIONS.md`:**
- What are Collections?
- Getting started (create → upload → enable)
- CLI command reference
- TUI usage guide
- Best practices (document size, formats, organization)
- Troubleshooting

**Example Structure:**

```markdown
# Collections Guide

## Overview
Collections enable Celeste to search your custom documents...

## Quick Start
1. Get a Management API key
2. Create a collection
3. Upload documents
4. Enable in chat

## CLI Reference
### celeste collections create
### celeste collections upload
...

## TUI Guide
Press `/collections` in chat to open...

## Best Practices
- Organize by topic
- Use descriptive names
- Keep documents under 10MB
...

## Troubleshooting
### "401 Unauthorized"
### "Collection not found"
...
```

---

### Updated Documentation

**`README.md`:**
- Add Collections section after Skills System
- Update feature list
- Add Collections commands to Usage section

**`docs/LLM_PROVIDERS.md`:**
- Add Collections column to compatibility matrix
- Add xAI Collections section with setup guide
- Document web_search and x_search tools

**`CHANGELOG.md`:**
```markdown
## [Unreleased]

### Added
- Collections support for xAI (RAG capabilities)
- Collections management CLI commands
- Collections TUI view for interactive management
- xAI built-in tools: web_search, x_search
- Management API key configuration

### Changed
- Updated xAI provider to support collections_search tool
- Enhanced LLM backend to handle built-in tools

### Documentation
- Added COLLECTIONS.md guide
- Updated LLM_PROVIDERS.md with Collections section
```

---

## Security Considerations

### API Keys

**Management API Key Storage:**
- Stored in `~/.celeste/config.json` (same as Chat API key)
- File permissions: 0600 (user read/write only)
- Never logged or displayed in UI
- Separate from Chat API key (different permissions scope)

**Key Permissions:**
- Management API key requires `AddFileToCollection` permission
- Chat API key doesn't need Collections permissions

### Document Uploads

**Sensitive Data:**
- Users are responsible for uploaded content
- No automatic PII detection/redaction
- Documents are stored on xAI servers
- Deletion removes from xAI but may be retained in backups

**Recommendations:**
- Review documents before upload
- Don't upload sensitive credentials, API keys, or PII
- Use separate collections for different sensitivity levels
- Regularly audit uploaded documents

### Rate Limiting

**Protection:**
- Exponential backoff on 429 errors
- Max 3 retries per request
- Batch uploads with delays between requests
- User-visible progress indicators

---

## Alternative Approaches Considered

### 1. SQLite-Based Local RAG

**Approach:** Implement local vector database instead of xAI Collections.

**Pros:**
- Works with any LLM provider (not xAI-specific)
- No additional API costs
- Full control over search implementation

**Cons:**
- Requires embedding model (tiktoken, sentence-transformers)
- Adds significant complexity to celeste-cli
- Slower than server-side search
- Increases binary size

**Decision:** Rejected. xAI Collections is simpler, faster, and already proven in other projects.

---

### 2. Direct File Injection (No Collections)

**Approach:** Read files locally and inject into system prompt.

**Pros:**
- Very simple implementation
- No additional API calls
- Works with any provider

**Cons:**
- Limited by context window
- No semantic search
- Must re-send files with every message
- Expensive (token-wise)

**Decision:** Rejected. Doesn't scale beyond small documents. Collections provides semantic search and efficient caching.

---

### 3. Hybrid: Collections + Local Cache

**Approach:** Cache collection documents locally for offline access.

**Pros:**
- Faster repeated queries
- Works offline (after initial sync)
- Reduces API calls

**Cons:**
- Significantly more complex
- Sync issues (stale data)
- Storage management required

**Decision:** Deferred to future enhancement. Start with pure cloud-based approach.

---

## Success Metrics

### Functional Metrics

- [ ] Collections can be created via CLI
- [ ] Documents upload successfully
- [ ] collections_search tool appears in API requests
- [ ] LLM successfully retrieves information from collections
- [ ] TUI collections view is functional and responsive
- [ ] All unit tests pass
- [ ] Integration tests pass with real API

### Quality Metrics

- [ ] Unit test coverage >70% for new code
- [ ] No memory leaks in TUI (verified with long sessions)
- [ ] Upload performance: >1MB/s for batch uploads
- [ ] API error handling: 100% of error types handled gracefully
- [ ] Documentation: All CLI commands documented with examples

### User Experience Metrics

- [ ] Setup time: <5 minutes from install to first collection query
- [ ] CLI discoverability: `celeste collections --help` is clear
- [ ] TUI responsiveness: <100ms for all interactions
- [ ] Error messages: Actionable and helpful

---

## Future Enhancements

### Phase 6+ (Post-Initial Release)

**Enhanced Search:**
- Metadata filtering in collections_search
- Custom retrieval modes (keyword vs semantic)
- Search result ranking/scoring

**Collection Syncing:**
- `celeste collections sync <id> <dir>` command
- Watch directory for changes and auto-upload
- Incremental updates (only modified files)

**TUI Enhancements:**
- Search within collection documents
- Preview document contents in TUI
- Upload progress bars
- Collection analytics (most queried docs)

**Multi-Provider RAG:**
- Adapter pattern for non-xAI providers
- Local RAG for OpenAI/Anthropic
- Unified interface regardless of backend

**Export/Import:**
- Export collection metadata as JSON
- Import collections from other celeste-cli instances
- Backup/restore functionality

---

## References

### External Documentation

- [xAI Collections API](https://docs.x.ai/docs/collections-api)
- [Using Collections via API](https://docs.x.ai/docs/guides/using-collections/api)
- [Collections Search Tool](https://docs.x.ai/docs/guides/tools/collections-search-tool)
- [xAI Function Calling](https://docs.x.ai/docs/guides/function-calling)
- [xAI Tools Overview](https://docs.x.ai/docs/guides/tools/overview)

### Internal Code References

- `celeste-tts-bot/scripts/upload_collection.py` - Upload script implementation
- `celeste-tts-bot/internal/ai/grok_client.go` - xAI client with tool calling
- `celeste-discord-bot/go/internal/tools/` - Tool execution patterns
- `cmd/celeste/llm/backend_openai.go` - Current tool calling implementation
- `cmd/celeste/skills/registry.go` - Skill/tool format

---

## Approval & Sign-off

**Design Author:** Claude (AI Assistant)
**Date:** 2026-02-17
**Status:** ✅ Approved by @whykusanagi

**Approved Features:**
- ✅ Collections support (Phase 1-3)
- ✅ Documentation improvements (Phase 4)
- ✅ Secondary features: web_search, x_search (Phase 5)
- ✅ TUI enhancements for collection management

**Next Steps:**
1. Proceed to implementation (Phase 1)
2. Create tasks/issues for each phase
3. Begin with Collections client implementation

---

**End of Design Document**
