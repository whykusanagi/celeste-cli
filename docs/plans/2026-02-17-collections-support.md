# Collections Support Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add xAI Collections support to celeste-cli for RAG capabilities, enabling Celeste to search custom document knowledge bases during conversations.

**Architecture:** Three-layer design: (1) Collections API client for xAI Management API, (2) High-level Manager for batch operations and config integration, (3) CLI commands and TUI interface for user interaction. Built-in `collections_search` tool added to LLM backend when collections are enabled.

**Tech Stack:** Go 1.24, xAI Management API, xAI Chat Completions API, Bubble Tea (TUI), Cobra (CLI), go-openai SDK

**Design Document:** `docs/plans/2026-02-17-collections-support-design.md`

---

## Phase 1: Core Collections API

### Task 1.1: Create Collections Package Structure

**Files:**
- Create: `cmd/celeste/collections/types.go`
- Create: `cmd/celeste/collections/client.go`
- Create: `cmd/celeste/collections/manager.go`

**Step 1: Create types.go with data structures**

```bash
mkdir -p cmd/celeste/collections
touch cmd/celeste/collections/types.go
```

**Step 2: Write Collection and Document types**

File: `cmd/celeste/collections/types.go`

```go
package collections

import "time"

// Collection represents an xAI collection
type Collection struct {
	ID            string    `json:"collection_id"`
	Name          string    `json:"collection_name"`
	Description   string    `json:"description"`
	CreatedAt     time.Time `json:"created_at"`
	DocumentCount int       `json:"document_count,omitempty"`
}

// Document represents a document in a collection
type Document struct {
	FileID      string    `json:"file_id"`
	Name        string    `json:"name"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
	UploadedAt  time.Time `json:"uploaded_at"`
}

// CollectionsError represents an API error
type CollectionsError struct {
	StatusCode int
	Message    string
	RequestID  string
}

func (e *CollectionsError) Error() string {
	if e.RequestID != "" {
		return fmt.Sprintf("collections API error (status %d, request %s): %s",
			e.StatusCode, e.RequestID, e.Message)
	}
	return fmt.Sprintf("collections API error (status %d): %s", e.StatusCode, e.Message)
}
```

**Step 3: Add missing import**

Add to top of `cmd/celeste/collections/types.go`:

```go
import (
	"fmt"
	"time"
)
```

**Step 4: Verify compilation**

```bash
go build ./cmd/celeste/collections/
```

Expected: Success (no output)

**Step 5: Commit**

```bash
git add cmd/celeste/collections/types.go
git commit -m "feat(collections): add Collection and Document types

- Define Collection and Document structs
- Add CollectionsError for API error handling
- Foundation for xAI Collections API client"
```

---

### Task 1.2: Implement Collections Client

**Files:**
- Create: `cmd/celeste/collections/client.go`
- Create: `cmd/celeste/collections/client_test.go`

**Step 1: Write failing test for CreateCollection**

File: `cmd/celeste/collections/client_test.go`

```go
package collections

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClient_CreateCollection(t *testing.T) {
	// Setup mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/collections", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Parse request body
		var req map[string]string
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)
		assert.Equal(t, "test-collection", req["collection_name"])
		assert.Equal(t, "test description", req["description"])

		// Return mock response
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"collection_id": "collection_abc123",
		})
	}))
	defer server.Close()

	// Create client pointing to mock server
	client := NewClient("test-key")
	client.baseURL = server.URL

	// Test
	id, err := client.CreateCollection("test-collection", "test description")
	require.NoError(t, err)
	assert.Equal(t, "collection_abc123", id)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./cmd/celeste/collections/ -run TestClient_CreateCollection -v
```

Expected: FAIL with "undefined: NewClient"

**Step 3: Write minimal Client implementation**

File: `cmd/celeste/collections/client.go`

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

const defaultManagementAPIURL = "https://management-api.x.ai/v1"

// Client handles xAI Collections Management API operations
type Client struct {
	managementAPIKey string
	baseURL          string
	httpClient       *http.Client
}

// NewClient creates a new Collections API client
func NewClient(managementAPIKey string) *Client {
	return &Client{
		managementAPIKey: managementAPIKey,
		baseURL:          defaultManagementAPIURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// CreateCollection creates a new collection
func (c *Client) CreateCollection(name, description string) (string, error) {
	url := c.baseURL + "/v1/collections"

	// Build request body
	body := map[string]string{
		"collection_name": name,
		"description":     description,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.managementAPIKey)
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", &CollectionsError{
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			RequestID:  resp.Header.Get("X-Request-ID"),
		}
	}

	// Parse response
	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	collectionID, ok := result["collection_id"]
	if !ok {
		return "", fmt.Errorf("collection_id not found in response")
	}

	return collectionID, nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./cmd/celeste/collections/ -run TestClient_CreateCollection -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add cmd/celeste/collections/client.go cmd/celeste/collections/client_test.go
git commit -m "feat(collections): implement CreateCollection

- Add Collections API client with HTTP methods
- Implement CreateCollection with proper error handling
- Add unit test with mock HTTP server
- Handle auth headers and request/response format"
```

---

### Task 1.3: Implement ListCollections

**Files:**
- Modify: `cmd/celeste/collections/client.go`
- Modify: `cmd/celeste/collections/client_test.go`

**Step 1: Write failing test**

Add to `cmd/celeste/collections/client_test.go`:

```go
func TestClient_ListCollections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/v1/collections", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"collections": []map[string]interface{}{
				{
					"collection_id":   "col_1",
					"collection_name": "Collection 1",
					"description":     "First collection",
					"created_at":      "2026-02-17T00:00:00Z",
					"document_count":  5,
				},
				{
					"collection_id":   "col_2",
					"collection_name": "Collection 2",
					"description":     "Second collection",
					"created_at":      "2026-02-16T00:00:00Z",
					"document_count":  10,
				},
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL

	collections, err := client.ListCollections()
	require.NoError(t, err)
	assert.Len(t, collections, 2)
	assert.Equal(t, "col_1", collections[0].ID)
	assert.Equal(t, "Collection 1", collections[0].Name)
	assert.Equal(t, 5, collections[0].DocumentCount)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./cmd/celeste/collections/ -run TestClient_ListCollections -v
```

Expected: FAIL with "undefined: client.ListCollections"

**Step 3: Implement ListCollections**

Add to `cmd/celeste/collections/client.go`:

```go
// ListCollections lists all collections
func (c *Client) ListCollections() ([]Collection, error) {
	url := c.baseURL + "/v1/collections"

	// Create request
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.managementAPIKey)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, &CollectionsError{
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			RequestID:  resp.Header.Get("X-Request-ID"),
		}
	}

	// Parse response
	var result struct {
		Collections []Collection `json:"collections"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return result.Collections, nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./cmd/celeste/collections/ -run TestClient_ListCollections -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add cmd/celeste/collections/client.go cmd/celeste/collections/client_test.go
git commit -m "feat(collections): implement ListCollections

- Add ListCollections method to fetch all collections
- Parse collections array from API response
- Add unit test with mock data"
```

---

### Task 1.4: Implement UploadDocument

**Files:**
- Modify: `cmd/celeste/collections/client.go`
- Modify: `cmd/celeste/collections/client_test.go`

**Step 1: Write failing test**

Add to `cmd/celeste/collections/client_test.go`:

```go
func TestClient_UploadDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/collections/col_123/documents", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		// Parse multipart form
		err := r.ParseMultipartForm(10 << 20) // 10MB
		require.NoError(t, err)

		assert.Equal(t, "test.md", r.FormValue("name"))
		assert.Equal(t, "text/markdown", r.FormValue("content_type"))

		// Check file content
		file, _, err := r.FormFile("data")
		require.NoError(t, err)
		defer file.Close()

		data, err := io.ReadAll(file)
		require.NoError(t, err)
		assert.Equal(t, "# Test Document", string(data))

		// Return response
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"file_metadata": map[string]string{
				"file_id": "file_xyz789",
			},
		})
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL

	fileID, err := client.UploadDocument("col_123", "test.md", []byte("# Test Document"), "text/markdown")
	require.NoError(t, err)
	assert.Equal(t, "file_xyz789", fileID)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./cmd/celeste/collections/ -run TestClient_UploadDocument -v
```

Expected: FAIL with "undefined: client.UploadDocument"

**Step 3: Implement UploadDocument**

Add to `cmd/celeste/collections/client.go` (add import "mime/multipart" at top):

```go
// UploadDocument uploads a document to a collection
func (c *Client) UploadDocument(collectionID, name string, data []byte, contentType string) (string, error) {
	url := c.baseURL + "/v1/collections/" + collectionID + "/documents"

	// Create multipart form
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	// Add name field
	if err := writer.WriteField("name", name); err != nil {
		return "", fmt.Errorf("failed to write name field: %w", err)
	}

	// Add content_type field
	if err := writer.WriteField("content_type", contentType); err != nil {
		return "", fmt.Errorf("failed to write content_type field: %w", err)
	}

	// Add data file
	part, err := writer.CreateFormFile("data", name)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return "", fmt.Errorf("failed to write file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close writer: %w", err)
	}

	// Create request
	req, err := http.NewRequest("POST", url, &buf)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.managementAPIKey)
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", &CollectionsError{
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			RequestID:  resp.Header.Get("X-Request-ID"),
		}
	}

	// Parse response
	var result struct {
		FileMetadata struct {
			FileID string `json:"file_id"`
		} `json:"file_metadata"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	return result.FileMetadata.FileID, nil
}
```

**Step 4: Update imports in client.go**

Add to imports:

```go
import (
	// ... existing imports
	"mime/multipart"
)
```

**Step 5: Run test to verify it passes**

```bash
go test ./cmd/celeste/collections/ -run TestClient_UploadDocument -v
```

Expected: PASS

**Step 6: Commit**

```bash
git add cmd/celeste/collections/client.go cmd/celeste/collections/client_test.go
git commit -m "feat(collections): implement UploadDocument

- Add UploadDocument with multipart form upload
- Handle file data, name, and content type
- Add unit test with multipart parsing verification"
```

---

### Task 1.5: Implement DeleteCollection

**Files:**
- Modify: `cmd/celeste/collections/client.go`
- Modify: `cmd/celeste/collections/client_test.go`

**Step 1: Write failing test**

Add to `cmd/celeste/collections/client_test.go`:

```go
func TestClient_DeleteCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/v1/collections/col_123", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL

	err := client.DeleteCollection("col_123")
	assert.NoError(t, err)
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./cmd/celeste/collections/ -run TestClient_DeleteCollection -v
```

Expected: FAIL with "undefined: client.DeleteCollection"

**Step 3: Implement DeleteCollection**

Add to `cmd/celeste/collections/client.go`:

```go
// DeleteCollection deletes a collection
func (c *Client) DeleteCollection(collectionID string) error {
	url := c.baseURL + "/v1/collections/" + collectionID

	// Create request
	req, err := http.NewRequest("DELETE", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.managementAPIKey)

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return &CollectionsError{
			StatusCode: resp.StatusCode,
			Message:    string(bodyBytes),
			RequestID:  resp.Header.Get("X-Request-ID"),
		}
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./cmd/celeste/collections/ -run TestClient_DeleteCollection -v
```

Expected: PASS

**Step 5: Run all collections tests**

```bash
go test ./cmd/celeste/collections/ -v
```

Expected: All tests PASS

**Step 6: Commit**

```bash
git add cmd/celeste/collections/client.go cmd/celeste/collections/client_test.go
git commit -m "feat(collections): implement DeleteCollection

- Add DeleteCollection method
- Add unit test for delete operation
- Collections client API complete (CRUD operations)"
```

---

### Task 1.6: Implement Collections Manager

**Files:**
- Create: `cmd/celeste/collections/manager.go`
- Create: `cmd/celeste/collections/manager_test.go`
- Modify: `cmd/celeste/config/config.go`

**Step 1: Add collections config to config.go**

File: `cmd/celeste/config/config.go` (find the Config struct and add):

```go
// Add to Config struct
	// Collections configuration (xAI only)
	XAIManagementAPIKey string              `json:"xai_management_api_key,omitempty"`
	Collections         *CollectionsConfig  `json:"collections,omitempty"`
	XAIFeatures         *XAIFeaturesConfig  `json:"xai_features,omitempty"`
```

Add new structs after Config:

```go
// CollectionsConfig holds collections settings
type CollectionsConfig struct {
	Enabled            bool     `json:"enabled"`
	ActiveCollections  []string `json:"active_collections"`
	AutoEnable         bool     `json:"auto_enable"`
}

// XAIFeaturesConfig holds xAI-specific feature flags
type XAIFeaturesConfig struct {
	EnableWebSearch bool `json:"enable_web_search"`
	EnableXSearch   bool `json:"enable_x_search"`
}
```

**Step 2: Test config loading**

```bash
go build ./cmd/celeste/config/
```

Expected: Success

**Step 3: Write failing test for Manager**

File: `cmd/celeste/collections/manager_test.go`

```go
package collections

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/whykusanagi/celesteCLI/cmd/celeste/config"
)

func TestManager_EnableDisableCollection(t *testing.T) {
	// Create temp config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	cfg := &config.Config{
		Collections: &config.CollectionsConfig{
			Enabled:           true,
			ActiveCollections: []string{},
			AutoEnable:        true,
		},
	}

	// Create mock client (nil is fine for this test)
	manager := NewManager(nil, cfg)

	// Enable collection
	err := manager.EnableCollection("col_123")
	require.NoError(t, err)
	assert.Contains(t, cfg.Collections.ActiveCollections, "col_123")

	// Disable collection
	err = manager.DisableCollection("col_123")
	require.NoError(t, err)
	assert.NotContains(t, cfg.Collections.ActiveCollections, "col_123")
}
```

**Step 4: Run test to verify it fails**

```bash
go test ./cmd/celeste/collections/ -run TestManager_EnableDisableCollection -v
```

Expected: FAIL with "undefined: NewManager"

**Step 5: Implement Manager**

File: `cmd/celeste/collections/manager.go`

```go
package collections

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celesteCLI/cmd/celeste/config"
)

// Manager provides high-level collections management
type Manager struct {
	client *Client
	config *config.Config
}

// NewManager creates a new collections manager
func NewManager(client *Client, cfg *config.Config) *Manager {
	return &Manager{
		client: client,
		config: cfg,
	}
}

// EnableCollection adds a collection to the active set
func (m *Manager) EnableCollection(collectionID string) error {
	if m.config.Collections == nil {
		m.config.Collections = &config.CollectionsConfig{
			Enabled:           true,
			ActiveCollections: []string{},
			AutoEnable:        true,
		}
	}

	// Check if already enabled
	for _, id := range m.config.Collections.ActiveCollections {
		if id == collectionID {
			return nil // Already enabled
		}
	}

	// Add to active collections
	m.config.Collections.ActiveCollections = append(
		m.config.Collections.ActiveCollections,
		collectionID,
	)

	return nil
}

// DisableCollection removes a collection from the active set
func (m *Manager) DisableCollection(collectionID string) error {
	if m.config.Collections == nil {
		return nil // Nothing to disable
	}

	// Filter out the collection
	var active []string
	for _, id := range m.config.Collections.ActiveCollections {
		if id != collectionID {
			active = append(active, id)
		}
	}

	m.config.Collections.ActiveCollections = active
	return nil
}

// GetActiveCollections returns the list of active collection IDs
func (m *Manager) GetActiveCollections() []string {
	if m.config.Collections == nil {
		return []string{}
	}
	return m.config.Collections.ActiveCollections
}
```

**Step 6: Run test to verify it passes**

```bash
go test ./cmd/celeste/collections/ -run TestManager_EnableDisableCollection -v
```

Expected: PASS

**Step 7: Commit**

```bash
git add cmd/celeste/collections/manager.go cmd/celeste/collections/manager_test.go cmd/celeste/config/config.go
git commit -m "feat(collections): implement Collections Manager

- Add Manager for high-level collection operations
- Implement Enable/DisableCollection for active set management
- Add collections config structs to config.go
- Add XAIFeatures config for web_search/x_search"
```

---

### Task 1.7: Add File Validation and Upload Helper

**Files:**
- Modify: `cmd/celeste/collections/manager.go`
- Modify: `cmd/celeste/collections/manager_test.go`

**Step 1: Write failing test for ValidateDocument**

Add to `cmd/celeste/collections/manager_test.go`:

```go
func TestManager_ValidateDocument(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		filename    string
		content     string
		size        int64
		shouldError bool
		errorMsg    string
	}{
		{
			name:        "valid markdown",
			filename:    "test.md",
			content:     "# Test",
			size:        6,
			shouldError: false,
		},
		{
			name:        "valid txt",
			filename:    "test.txt",
			content:     "Test",
			size:        4,
			shouldError: false,
		},
		{
			name:        "unsupported format",
			filename:    "test.exe",
			content:     "data",
			size:        4,
			shouldError: true,
			errorMsg:    "unsupported format",
		},
		{
			name:        "file too large",
			filename:    "large.md",
			content:     "",
			size:        11 * 1024 * 1024, // 11MB
			shouldError: true,
			errorMsg:    "file too large",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test file
			path := filepath.Join(tmpDir, tt.filename)
			if tt.size > 0 {
				// For size test, create actual large file
				f, err := os.Create(path)
				require.NoError(t, err)
				err = f.Truncate(tt.size)
				require.NoError(t, err)
				f.Close()
			} else {
				os.WriteFile(path, []byte(tt.content), 0644)
			}

			// Test validation
			err := ValidateDocument(path)

			if tt.shouldError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
			}

			// Cleanup
			os.Remove(path)
		})
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./cmd/celeste/collections/ -run TestManager_ValidateDocument -v
```

Expected: FAIL with "undefined: ValidateDocument"

**Step 3: Implement ValidateDocument**

Add to `cmd/celeste/collections/manager.go`:

```go
// ValidateDocument checks if a document is valid for upload
func ValidateDocument(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Check if it's a directory
	if info.IsDir() {
		return fmt.Errorf("path is a directory, not a file")
	}

	// Check size limit (10MB)
	const maxSize = 10 * 1024 * 1024
	if info.Size() > maxSize {
		return fmt.Errorf("file too large: %d bytes (max %d bytes)", info.Size(), maxSize)
	}

	// Check supported formats
	ext := strings.ToLower(filepath.Ext(path))
	supported := []string{".md", ".txt", ".pdf", ".html", ".htm"}

	isSupported := false
	for _, s := range supported {
		if ext == s {
			isSupported = true
			break
		}
	}

	if !isSupported {
		return fmt.Errorf("unsupported format: %s (supported: .md, .txt, .pdf, .html)", ext)
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

```bash
go test ./cmd/celeste/collections/ -run TestManager_ValidateDocument -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add cmd/celeste/collections/manager.go cmd/celeste/collections/manager_test.go
git commit -m "feat(collections): add document validation

- Implement ValidateDocument for size and format checks
- Support .md, .txt, .pdf, .html formats
- Enforce 10MB file size limit
- Add comprehensive validation tests"
```

---

## Phase 2: LLM Integration

### Task 2.1: Update LLM Backend for Collections

**Files:**
- Modify: `cmd/celeste/llm/backend_openai.go`

**Step 1: Find convertTools method**

```bash
grep -n "func.*convertTools" cmd/celeste/llm/backend_openai.go
```

Expected: Shows line number of convertTools function

**Step 2: Update convertTools to add collections_search**

Modify `cmd/celeste/llm/backend_openai.go` - find the `convertTools` method (around line 317) and replace it:

```go
// convertTools converts TUI skill definitions to OpenAI tools.
func (b *OpenAIBackend) convertTools(tools []tui.SkillDefinition) []openai.Tool {
	var result []openai.Tool

	// Add xAI built-in tools if configured (xAI only)
	if b.config.Collections != nil && b.config.Collections.Enabled {
		if len(b.config.Collections.ActiveCollections) > 0 {
			// Note: collections_search is a built-in xAI tool
			// The go-openai library might not have this type yet
			// We'll need to use a generic tool structure
			result = append(result, openai.Tool{
				Type: "collections_search",
				// Collections IDs are passed in the tool definition for xAI
			})
		}
	}

	// Add web_search if enabled (xAI only)
	if b.config.XAIFeatures != nil && b.config.XAIFeatures.EnableWebSearch {
		result = append(result, openai.Tool{
			Type: "web_search",
		})
	}

	// Add x_search if enabled (xAI only)
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

**Step 3: Check if Config has Collections fields**

```bash
grep -A 3 "type Config struct" cmd/celeste/llm/client.go
```

**Step 4: Add Collections fields to LLM Config**

Modify `cmd/celeste/llm/client.go` - find the Config struct and add:

```go
// Add to Config struct
	// Collections (xAI only)
	Collections  *config.CollectionsConfig
	XAIFeatures  *config.XAIFeaturesConfig
```

Add import at top:

```go
import (
	// ... existing imports
	"github.com/whykusanagi/celesteCLI/cmd/celeste/config"
)
```

**Step 5: Build to verify**

```bash
go build ./cmd/celeste/llm/
```

Expected: Success (may have some warnings about unused fields)

**Step 6: Commit**

```bash
git add cmd/celeste/llm/backend_openai.go cmd/celeste/llm/client.go
git commit -m "feat(llm): add collections_search and xAI tools support

- Update convertTools to include collections_search when enabled
- Add web_search and x_search built-in tools support
- Add Collections and XAIFeatures to LLM Config
- Built-in tools are server-side (xAI handles execution)"
```

---

### Task 2.2: Pass Collections Config to LLM Client

**Files:**
- Modify: `cmd/celeste/tui/app.go`

**Step 1: Find NewClient call in TUI**

```bash
grep -n "llm.NewClient" cmd/celeste/tui/app.go
```

Expected: Shows line where LLM client is created

**Step 2: Update LLM client creation to pass collections config**

Find the section in `cmd/celeste/tui/app.go` where `llm.NewClient` is called (likely in `NewApp` or similar), and update the config passed:

```go
// Update the llm.Config creation to include collections
llmConfig := &llm.Config{
	APIKey:            cfg.APIKey,
	BaseURL:           cfg.BaseURL,
	Model:             cfg.Model,
	Timeout:           time.Duration(cfg.Timeout) * time.Second,
	SkipPersonaPrompt: cfg.SkipPersonaPrompt,
	SimulateTyping:    cfg.SimulateTyping,
	TypingSpeed:       cfg.TypingSpeed,
	Collections:       cfg.Collections,      // Add this
	XAIFeatures:       cfg.XAIFeatures,      // Add this
}
```

**Step 3: Build TUI to verify**

```bash
go build ./cmd/celeste/tui/
```

Expected: Success

**Step 4: Build full binary**

```bash
go build -o celeste ./cmd/celeste
```

Expected: Success

**Step 5: Test that binary runs**

```bash
./celeste --version
```

Expected: Shows version number

**Step 6: Commit**

```bash
git add cmd/celeste/tui/app.go
git commit -m "feat(tui): pass collections config to LLM client

- Update LLM client creation to include Collections config
- Pass XAIFeatures config for web_search/x_search
- Enable collections_search tool when collections are active"
```

---

## Phase 3: CLI Commands

### Task 3.1: Create Collections CLI Command Structure

**Files:**
- Create: `cmd/celeste/commands/collections.go`
- Modify: `cmd/celeste/main.go`

**Step 1: Create collections command file**

File: `cmd/celeste/commands/collections.go`

```go
package commands

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/whykusanagi/celesteCLI/cmd/celeste/collections"
	"github.com/whykusanagi/celesteCLI/cmd/celeste/config"
)

// CollectionsCommand returns the collections command
func CollectionsCommand(cfg *config.Config) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collections",
		Short: "Manage xAI Collections for RAG",
		Long: `Create, upload, and manage xAI Collections for document search.
Collections enable Celeste to search your custom documents during conversations.`,
		Example: `  # Create a collection
  celeste collections create "my-docs" --description "My documentation"

  # Upload documents
  celeste collections upload <collection-id> docs/*.md

  # List collections
  celeste collections list

  # Enable for chat
  celeste collections enable <collection-id>`,
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

// Helper to get management API key
func getManagementAPIKey(cfg *config.Config) (string, error) {
	key := cfg.XAIManagementAPIKey
	if key == "" {
		key = os.Getenv("XAI_MANAGEMENT_API_KEY")
	}
	if key == "" {
		return "", fmt.Errorf("xAI Management API key not configured.\nSet it with: celeste config --set-management-key <key>\nOr: export XAI_MANAGEMENT_API_KEY=<key>")
	}
	return key, nil
}

// Helper to create collections client
func createCollectionsClient(cfg *config.Config) (*collections.Client, error) {
	key, err := getManagementAPIKey(cfg)
	if err != nil {
		return nil, err
	}
	return collections.NewClient(key), nil
}
```

**Step 2: Add empty subcommand functions (will implement next)**

Add to `cmd/celeste/commands/collections.go`:

```go
func collectionsCreateCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet")
		},
	}
}

func collectionsListCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all collections",
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet")
		},
	}
}

func collectionsUploadCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "upload <collection-id> <files...>",
		Short: "Upload documents to a collection",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet")
		},
	}
}

func collectionsDeleteCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <collection-id>",
		Short: "Delete a collection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet")
		},
	}
}

func collectionsEnableCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "enable <collection-id>",
		Short: "Add collection to active set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet")
		},
	}
}

func collectionsDisableCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <collection-id>",
		Short: "Remove collection from active set",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet")
		},
	}
}

func collectionsShowCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "show <collection-id>",
		Short: "Show collection details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return fmt.Errorf("not implemented yet")
		},
	}
}
```

**Step 3: Register collections command in main.go**

Find `cmd/celeste/main.go` and add after the other command registrations:

```go
// Add after other rootCmd.AddCommand calls
rootCmd.AddCommand(commands.CollectionsCommand(cfg))
```

**Step 4: Build and test help**

```bash
go build -o celeste ./cmd/celeste
./celeste collections --help
```

Expected: Shows collections command help with subcommands

**Step 5: Commit**

```bash
git add cmd/celeste/commands/collections.go cmd/celeste/main.go
git commit -m "feat(cli): add collections command structure

- Create collections command with 7 subcommands
- Add helper functions for client creation
- Register in main.go
- Subcommands stubbed (will implement next)"
```

---

### Task 3.2: Implement collections create command

**Files:**
- Modify: `cmd/celeste/commands/collections.go`

**Step 1: Implement collectionsCreateCommand**

Replace the `collectionsCreateCommand` function in `cmd/celeste/commands/collections.go`:

```go
func collectionsCreateCommand(cfg *config.Config) *cobra.Command {
	var description string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new collection",
		Long:  "Create a new xAI collection for document storage and search.",
		Args:  cobra.ExactArgs(1),
		Example: `  celeste collections create "celeste-lore" --description "Celeste personality and lore"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Create client
			client, err := createCollectionsClient(cfg)
			if err != nil {
				return err
			}

			// Create collection
			fmt.Printf("Creating collection '%s'...\n", name)
			collectionID, err := client.CreateCollection(name, description)
			if err != nil {
				return fmt.Errorf("failed to create collection: %w", err)
			}

			fmt.Printf("✅ Collection created successfully!\n")
			fmt.Printf("   Collection ID: %s\n", collectionID)
			fmt.Printf("   Name: %s\n", name)
			if description != "" {
				fmt.Printf("   Description: %s\n", description)
			}
			fmt.Printf("\nNext steps:\n")
			fmt.Printf("  1. Upload documents: celeste collections upload %s <files>\n", collectionID)
			fmt.Printf("  2. Enable for chat: celeste collections enable %s\n", collectionID)

			return nil
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "Collection description")

	return cmd
}
```

**Step 2: Test create command (manual)**

```bash
go build -o celeste ./cmd/celeste
./celeste collections create --help
```

Expected: Shows help for create command with description flag

**Step 3: Commit**

```bash
git add cmd/celeste/commands/collections.go
git commit -m "feat(cli): implement collections create command

- Implement collectionsCreateCommand with description flag
- Show collection ID and next steps after creation
- Add usage examples"
```

---

### Task 3.3: Implement collections list command

**Files:**
- Modify: `cmd/celeste/commands/collections.go`

**Step 1: Implement collectionsListCommand**

Replace the `collectionsListCommand` function:

```go
func collectionsListCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all collections",
		Long:  "List all xAI collections with their metadata.",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Create client
			client, err := createCollectionsClient(cfg)
			if err != nil {
				return err
			}

			// List collections
			fmt.Println("Fetching collections...")
			collections, err := client.ListCollections()
			if err != nil {
				return fmt.Errorf("failed to list collections: %w", err)
			}

			if len(collections) == 0 {
				fmt.Println("No collections found.")
				fmt.Println("\nCreate one with: celeste collections create <name>")
				return nil
			}

			// Get active collections for marking
			activeIDs := make(map[string]bool)
			if cfg.Collections != nil {
				for _, id := range cfg.Collections.ActiveCollections {
					activeIDs[id] = true
				}
			}

			// Display collections
			fmt.Printf("\nCollections (%d):\n\n", len(collections))
			for i, col := range collections {
				marker := " "
				if activeIDs[col.ID] {
					marker = "✓"
				}

				fmt.Printf("%s [%d] %s\n", marker, i+1, col.Name)
				fmt.Printf("    ID: %s\n", col.ID)
				if col.Description != "" {
					fmt.Printf("    Description: %s\n", col.Description)
				}
				if col.DocumentCount > 0 {
					fmt.Printf("    Documents: %d\n", col.DocumentCount)
				}
				fmt.Printf("    Created: %s\n", col.CreatedAt.Format("2006-01-02 15:04:05"))
				fmt.Println()
			}

			if len(activeIDs) > 0 {
				fmt.Println("✓ = Active (enabled for chat)")
			}

			return nil
		},
	}
}
```

**Step 2: Test list command**

```bash
go build -o celeste ./cmd/celeste
./celeste collections list --help
```

Expected: Shows help for list command

**Step 3: Commit**

```bash
git add cmd/celeste/commands/collections.go
git commit -m "feat(cli): implement collections list command

- Show all collections with metadata
- Mark active collections with checkmark
- Display document count and creation date
- Handle empty state gracefully"
```

---

### Task 3.4: Implement collections enable/disable commands

**Files:**
- Modify: `cmd/celeste/commands/collections.go`

**Step 1: Implement collectionsEnableCommand**

Replace the function:

```go
func collectionsEnableCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "enable <collection-id>",
		Short: "Add collection to active set",
		Long:  "Enable a collection for use in chat. The collection will be searched when you ask questions.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := args[0]

			// Create manager
			manager := collections.NewManager(nil, cfg)

			// Enable collection
			if err := manager.EnableCollection(collectionID); err != nil {
				return fmt.Errorf("failed to enable collection: %w", err)
			}

			// Save config
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("✅ Collection enabled: %s\n", collectionID)
			fmt.Printf("\nActive collections: %d\n", len(cfg.Collections.ActiveCollections))
			for _, id := range cfg.Collections.ActiveCollections {
				fmt.Printf("  - %s\n", id)
			}
			fmt.Printf("\nThe collections_search tool is now available in chat.\n")

			return nil
		},
	}
}
```

**Step 2: Implement collectionsDisableCommand**

Replace the function:

```go
func collectionsDisableCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "disable <collection-id>",
		Short: "Remove collection from active set",
		Long:  "Disable a collection so it won't be searched in chat.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := args[0]

			// Create manager
			manager := collections.NewManager(nil, cfg)

			// Disable collection
			if err := manager.DisableCollection(collectionID); err != nil {
				return fmt.Errorf("failed to disable collection: %w", err)
			}

			// Save config
			if err := cfg.Save(); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			fmt.Printf("✅ Collection disabled: %s\n", collectionID)

			if len(cfg.Collections.ActiveCollections) > 0 {
				fmt.Printf("\nRemaining active collections: %d\n", len(cfg.Collections.ActiveCollections))
				for _, id := range cfg.Collections.ActiveCollections {
					fmt.Printf("  - %s\n", id)
				}
			} else {
				fmt.Printf("\nNo active collections. The collections_search tool is disabled.\n")
			}

			return nil
		},
	}
}
```

**Step 3: Build and test**

```bash
go build -o celeste ./cmd/celeste
./celeste collections enable --help
./celeste collections disable --help
```

Expected: Shows help for both commands

**Step 4: Commit**

```bash
git add cmd/celeste/commands/collections.go
git commit -m "feat(cli): implement enable/disable commands

- Add collectionsEnableCommand to activate collections
- Add collectionsDisableCommand to deactivate collections
- Save config after changes
- Show active collections list"
```

---

### Task 3.5: Implement collections upload command

**Files:**
- Modify: `cmd/celeste/commands/collections.go`

**Step 1: Implement collectionsUploadCommand**

Replace the function:

```go
func collectionsUploadCommand(cfg *config.Config) *cobra.Command {
	var recursive bool

	cmd := &cobra.Command{
		Use:   "upload <collection-id> <files...>",
		Short: "Upload documents to a collection",
		Long:  "Upload one or more documents to a collection. Supports .md, .txt, .pdf, .html files up to 10MB each.",
		Args:  cobra.MinimumNArgs(2),
		Example: `  # Upload single file
  celeste collections upload col_123 document.md

  # Upload multiple files
  celeste collections upload col_123 docs/*.md

  # Upload directory recursively
  celeste collections upload col_123 docs/ --recursive`,
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := args[0]
			paths := args[1:]

			// Create client
			client, err := createCollectionsClient(cfg)
			if err != nil {
				return err
			}

			// Collect all files to upload
			var filesToUpload []string
			for _, path := range paths {
				info, err := os.Stat(path)
				if err != nil {
					fmt.Printf("⚠️  Skipping %s: %v\n", path, err)
					continue
				}

				if info.IsDir() {
					if !recursive {
						fmt.Printf("⚠️  Skipping directory %s (use --recursive to upload directories)\n", path)
						continue
					}

					// Walk directory
					err := filepath.Walk(path, func(p string, info os.FileInfo, err error) error {
						if err != nil {
							return err
						}
						if !info.IsDir() {
							filesToUpload = append(filesToUpload, p)
						}
						return nil
					})
					if err != nil {
						fmt.Printf("⚠️  Error walking directory %s: %v\n", path, err)
					}
				} else {
					filesToUpload = append(filesToUpload, path)
				}
			}

			if len(filesToUpload) == 0 {
				return fmt.Errorf("no files to upload")
			}

			// Upload files
			fmt.Printf("Uploading %d file(s) to collection %s...\n\n", len(filesToUpload), collectionID)

			uploaded := 0
			skipped := 0

			for i, path := range filesToUpload {
				// Validate
				if err := collections.ValidateDocument(path); err != nil {
					fmt.Printf("[%d/%d] ⚠️  Skipped %s: %v\n", i+1, len(filesToUpload), filepath.Base(path), err)
					skipped++
					continue
				}

				// Read file
				data, err := os.ReadFile(path)
				if err != nil {
					fmt.Printf("[%d/%d] ⚠️  Failed to read %s: %v\n", i+1, len(filesToUpload), filepath.Base(path), err)
					skipped++
					continue
				}

				// Determine content type
				ext := strings.ToLower(filepath.Ext(path))
				contentType := "text/plain"
				switch ext {
				case ".md":
					contentType = "text/markdown"
				case ".html", ".htm":
					contentType = "text/html"
				case ".pdf":
					contentType = "application/pdf"
				}

				// Upload
				name := filepath.Base(path)
				_, err = client.UploadDocument(collectionID, name, data, contentType)
				if err != nil {
					fmt.Printf("[%d/%d] ❌ Failed %s: %v\n", i+1, len(filesToUpload), name, err)
					skipped++
					continue
				}

				fmt.Printf("[%d/%d] ✅ Uploaded %s (%d bytes)\n", i+1, len(filesToUpload), name, len(data))
				uploaded++
			}

			// Summary
			fmt.Printf("\n📊 Upload complete: %d uploaded, %d skipped\n", uploaded, skipped)

			if uploaded > 0 {
				fmt.Printf("\nNext step: Enable collection for chat\n")
				fmt.Printf("  celeste collections enable %s\n", collectionID)
			}

			return nil
		},
	}

	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "Upload directories recursively")

	return cmd
}
```

**Step 2: Add missing import**

Add to imports at top of file:

```go
import (
	// ... existing imports
	"path/filepath"
	"strings"
)
```

**Step 3: Build and test**

```bash
go build -o celeste ./cmd/celeste
./celeste collections upload --help
```

Expected: Shows help with examples

**Step 4: Commit**

```bash
git add cmd/celeste/commands/collections.go
git commit -m "feat(cli): implement collections upload command

- Support single file and batch uploads
- Add recursive directory upload with --recursive flag
- Validate files before upload (size, format)
- Show progress with file counter
- Auto-detect content type from extension
- Display summary with upload/skip counts"
```

---

### Task 3.6: Implement remaining commands (delete, show)

**Files:**
- Modify: `cmd/celeste/commands/collections.go`

**Step 1: Implement collectionsDeleteCommand**

Replace the function:

```go
func collectionsDeleteCommand(cfg *config.Config) *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <collection-id>",
		Short: "Delete a collection",
		Long:  "Delete a collection and all its documents. This action cannot be undone.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := args[0]

			// Confirm unless --force
			if !force {
				fmt.Printf("⚠️  This will permanently delete collection %s and all its documents.\n", collectionID)
				fmt.Print("Continue? (y/N): ")

				var response string
				fmt.Scanln(&response)
				if response != "y" && response != "Y" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			// Create client
			client, err := createCollectionsClient(cfg)
			if err != nil {
				return err
			}

			// Delete collection
			fmt.Printf("Deleting collection %s...\n", collectionID)
			if err := client.DeleteCollection(collectionID); err != nil {
				return fmt.Errorf("failed to delete collection: %w", err)
			}

			// Remove from active collections if present
			manager := collections.NewManager(nil, cfg)
			manager.DisableCollection(collectionID)
			cfg.Save()

			fmt.Println("✅ Collection deleted successfully.")

			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")

	return cmd
}
```

**Step 2: Implement collectionsShowCommand**

Replace the function:

```go
func collectionsShowCommand(cfg *config.Config) *cobra.Command {
	return &cobra.Command{
		Use:   "show <collection-id>",
		Short: "Show collection details",
		Long:  "Display detailed information about a collection including its documents.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			collectionID := args[0]

			// Create client
			client, err := createCollectionsClient(cfg)
			if err != nil {
				return err
			}

			// This is a placeholder - xAI API may not have GetCollection endpoint
			// We'll use ListCollections and filter
			fmt.Printf("Fetching collection %s...\n", collectionID)

			allCollections, err := client.ListCollections()
			if err != nil {
				return fmt.Errorf("failed to fetch collections: %w", err)
			}

			// Find the collection
			var col *collections.Collection
			for i := range allCollections {
				if allCollections[i].ID == collectionID {
					col = &allCollections[i]
					break
				}
			}

			if col == nil {
				return fmt.Errorf("collection not found: %s", collectionID)
			}

			// Check if active
			isActive := false
			if cfg.Collections != nil {
				for _, id := range cfg.Collections.ActiveCollections {
					if id == collectionID {
						isActive = true
						break
					}
				}
			}

			// Display collection details
			fmt.Println("\n" + strings.Repeat("=", 60))
			fmt.Printf("Collection: %s\n", col.Name)
			fmt.Println(strings.Repeat("=", 60))
			fmt.Printf("ID:          %s\n", col.ID)
			if col.Description != "" {
				fmt.Printf("Description: %s\n", col.Description)
			}
			fmt.Printf("Status:      %s\n", map[bool]string{true: "Active ✓", false: "Inactive"}[isActive])
			fmt.Printf("Documents:   %d\n", col.DocumentCount)
			fmt.Printf("Created:     %s\n", col.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Println(strings.Repeat("=", 60))

			if !isActive {
				fmt.Printf("\nTo enable for chat: celeste collections enable %s\n", collectionID)
			}

			return nil
		},
	}
}
```

**Step 3: Build and test**

```bash
go build -o celeste ./cmd/celeste
./celeste collections delete --help
./celeste collections show --help
```

Expected: Shows help for both commands

**Step 4: Commit**

```bash
git add cmd/celeste/commands/collections.go
git commit -m "feat(cli): implement delete and show commands

- Add collectionsDeleteCommand with confirmation prompt
- Add --force flag to skip confirmation
- Add collectionsShowCommand to display collection details
- Show active status in show command
- All CLI commands now complete"
```

---

## Phase 4: TUI Collections View

### Task 4.1: Create Collections TUI Model

**Files:**
- Create: `cmd/celeste/tui/collections.go`

**Step 1: Create basic Collections model**

File: `cmd/celeste/tui/collections.go`

```go
package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celesteCLI/cmd/celeste/collections"
)

// CollectionsModel is the TUI model for collections management
type CollectionsModel struct {
	collections    []collections.Collection
	activeIDs      map[string]bool
	cursor         int
	viewport       viewport.Model
	manager        *collections.Manager
	width, height  int
	err            error
}

// NewCollectionsModel creates a new collections model
func NewCollectionsModel(manager *collections.Manager) CollectionsModel {
	return CollectionsModel{
		manager:   manager,
		activeIDs: make(map[string]bool),
		viewport:  viewport.New(80, 20),
	}
}

// Init initializes the model
func (m CollectionsModel) Init() tea.Cmd {
	return m.loadCollections
}

// loadCollections fetches collections from API
func (m CollectionsModel) loadCollections() tea.Msg {
	// This will be implemented with actual API call
	return collectionsLoadedMsg{
		collections: []collections.Collection{},
		err:         nil,
	}
}

type collectionsLoadedMsg struct {
	collections []collections.Collection
	err         error
}

// Update handles messages
func (m CollectionsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "Q", "esc":
			// Return to chat
			return m, nil
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.collections)-1 {
				m.cursor++
			}
		case " ": // Toggle active/inactive
			if m.cursor < len(m.collections) {
				collectionID := m.collections[m.cursor].ID
				if m.activeIDs[collectionID] {
					m.manager.DisableCollection(collectionID)
					delete(m.activeIDs, collectionID)
				} else {
					m.manager.EnableCollection(collectionID)
					m.activeIDs[collectionID] = true
				}
			}
		}

	case collectionsLoadedMsg:
		m.collections = msg.collections
		m.err = msg.err

		// Load active collections
		activeList := m.manager.GetActiveCollections()
		for _, id := range activeList {
			m.activeIDs[id] = true
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 4 // Reserve space for header/footer
	}

	return m, nil
}

// View renders the model
func (m CollectionsModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n\nPress 'q' to return to chat.", m.err)
	}

	if len(m.collections) == 0 {
		return "No collections found.\n\nPress 'q' to return to chat."
	}

	// Render collections list
	var content string

	// Active collections
	activeCount := 0
	for _, col := range m.collections {
		if m.activeIDs[col.ID] {
			activeCount++
		}
	}

	content += lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(accentColor)).
		Render(fmt.Sprintf("Active Collections (%d):", activeCount)) + "\n\n"

	for i, col := range m.collections {
		if !m.activeIDs[col.ID] {
			continue
		}

		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		line := fmt.Sprintf("%s✓ %-30s (%d docs)", cursor, col.Name, col.DocumentCount)
		if i == m.cursor {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(accentColor)).
				Render(line)
		}
		content += line + "\n"
	}

	content += "\n"

	// Available collections
	inactiveCount := len(m.collections) - activeCount
	if inactiveCount > 0 {
		content += lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color(mutedColor)).
			Render(fmt.Sprintf("Available Collections (%d):", inactiveCount)) + "\n\n"

		for i, col := range m.collections {
			if m.activeIDs[col.ID] {
				continue
			}

			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}

			line := fmt.Sprintf("%s○ %-30s (%d docs)", cursor, col.Name, col.DocumentCount)
			if i == m.cursor {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color(accentColor)).
					Render(line)
			}
			content += line + "\n"
		}
	}

	// Footer with keybindings
	footer := "\n" + lipgloss.NewStyle().
		Foreground(lipgloss.Color(mutedColor)).
		Render("[↑/↓] Navigate  [Space] Toggle  [Q] Back to Chat")

	return content + footer
}
```

**Step 2: Build to verify**

```bash
go build ./cmd/celeste/tui/
```

Expected: Success

**Step 3: Commit**

```bash
git add cmd/celeste/tui/collections.go
git commit -m "feat(tui): create Collections TUI model

- Add CollectionsModel with Bubble Tea interface
- Implement navigation and toggle keybindings
- Show active and available collections separately
- Add cursor highlighting and selection
- Foundation for interactive collections view"
```

---

### Task 4.2: Integrate Collections View into Main TUI

**Files:**
- Modify: `cmd/celeste/tui/app.go`

**Step 1: Add collections mode to App model**

Find the `App` struct in `cmd/celeste/tui/app.go` and add:

```go
// Add to App struct
	collectionsModel *CollectionsModel
	viewMode         string // "chat" or "collections"
```

**Step 2: Handle /collections command**

Find the section where in-chat commands are handled (likely in the `Update` method where messages are processed), and add:

```go
// Add in message handling (where /help, /clear, etc. are handled)
if strings.HasPrefix(trimmed, "/collections") {
	// Switch to collections view
	m.viewMode = "collections"

	// Create collections manager if not exists
	if m.collectionsModel == nil {
		client := collections.NewClient(m.config.XAIManagementAPIKey)
		manager := collections.NewManager(client, m.config)
		model := NewCollectionsModel(manager)
		m.collectionsModel = &model
	}

	return m, m.collectionsModel.Init()
}
```

**Step 3: Route Update to collections view**

In the `Update` method of `App`, add mode routing:

```go
// Add at start of Update method
func (m App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	// Continue with normal chat update...
```

**Step 4: Route View to collections view**

In the `View` method of `App`, add mode routing:

```go
// Add at start of View method
func (m App) View() string {
	// Show collections view if in that mode
	if m.viewMode == "collections" && m.collectionsModel != nil {
		return m.collectionsModel.View()
	}

	// Continue with normal chat view...
```

**Step 5: Initialize viewMode in NewApp**

Find `NewApp` function and add:

```go
// Add to NewApp
	viewMode:     "chat",
```

**Step 6: Build and verify**

```bash
go build -o celeste ./cmd/celeste
```

Expected: Success

**Step 7: Commit**

```bash
git add cmd/celeste/tui/app.go
git commit -m "feat(tui): integrate collections view into main TUI

- Add viewMode to switch between chat and collections
- Handle /collections command to open collections view
- Route Update and View to collections model when active
- Press Q to return to chat from collections view"
```

---

## Phase 5: Documentation & Polish

### Task 5.1: Create COLLECTIONS.md Guide

**Files:**
- Create: `docs/COLLECTIONS.md`

**Step 1: Write comprehensive collections guide**

File: `docs/COLLECTIONS.md`

```markdown
# Collections Guide

## Overview

Collections enable Celeste to search your custom documents during conversations. Upload documentation, notes, lore, or any text-based knowledge, and Celeste will automatically search these documents when answering questions.

**Key Features:**
- Semantic search across uploaded documents
- Support for multiple document formats (.md, .txt, .pdf, .html)
- Interactive TUI for collection management
- Automatic tool integration with xAI Grok

**Powered by:** xAI Collections API with RAG (Retrieval-Augmented Generation)

---

## Quick Start

### 1. Get a Management API Key

Collections require an xAI Management API Key (separate from your Chat API key).

1. Visit [https://console.x.ai](https://console.x.ai)
2. Navigate to API Keys
3. Create a new Management API Key with `AddFileToCollection` permission
4. Copy the key (starts with `xai-`)

### 2. Configure the Key

```bash
# Option 1: Save to config
celeste config --set-management-key xai-YOUR-MANAGEMENT-KEY

# Option 2: Environment variable
export XAI_MANAGEMENT_API_KEY=xai-YOUR-MANAGEMENT-KEY
```

### 3. Create a Collection

```bash
celeste collections create "my-docs" --description "My documentation and notes"
```

Output:
```
✅ Collection created successfully!
   Collection ID: collection_abc123def
   Name: my-docs
   Description: My documentation and notes

Next steps:
  1. Upload documents: celeste collections upload collection_abc123def <files>
  2. Enable for chat: celeste collections enable collection_abc123def
```

### 4. Upload Documents

```bash
# Upload single file
celeste collections upload collection_abc123def document.md

# Upload multiple files
celeste collections upload collection_abc123def docs/*.md

# Upload directory recursively
celeste collections upload collection_abc123def docs/ --recursive
```

### 5. Enable for Chat

```bash
celeste collections enable collection_abc123def
```

### 6. Start Chatting

```bash
celeste chat
```

Celeste will now automatically search your documents when relevant to your questions!

---

## CLI Commands Reference

### `celeste collections create <name>`

Create a new collection.

**Flags:**
- `-d, --description <text>` - Collection description

**Example:**
```bash
celeste collections create "celeste-lore" \
  --description "Celeste personality, backstory, and character traits"
```

---

### `celeste collections list`

List all collections with metadata.

**Example Output:**
```
Collections (3):

✓ [1] celeste-lore
    ID: collection_abc123
    Description: Celeste personality and lore
    Documents: 15
    Created: 2026-02-17 10:30:00

✓ [2] nikke-wiki
    ID: collection_def456
    Documents: 42
    Created: 2026-02-16 14:20:00

  [3] archived-logs
    ID: collection_ghi789
    Documents: 120
    Created: 2026-02-10 09:15:00

✓ = Active (enabled for chat)
```

---

### `celeste collections upload <collection-id> <files...>`

Upload documents to a collection.

**Flags:**
- `-r, --recursive` - Upload directories recursively

**Supported Formats:**
- `.md` (Markdown)
- `.txt` (Plain text)
- `.pdf` (PDF documents)
- `.html`, `.htm` (HTML documents)

**Size Limit:** 10MB per file

**Examples:**

```bash
# Single file
celeste collections upload col_123 README.md

# Multiple files with glob
celeste collections upload col_123 docs/*.md

# Directory (recursive)
celeste collections upload col_123 docs/ --recursive

# Multiple paths
celeste collections upload col_123 file1.md file2.txt docs/
```

**Output:**
```
Uploading 3 file(s) to collection col_123...

[1/3] ✅ Uploaded README.md (2048 bytes)
[2/3] ✅ Uploaded GUIDE.md (4096 bytes)
[3/3] ⚠️  Skipped large_file.pdf: file too large (12MB > 10MB limit)

📊 Upload complete: 2 uploaded, 1 skipped

Next step: Enable collection for chat
  celeste collections enable col_123
```

---

### `celeste collections enable <collection-id>`

Add a collection to the active set (enable for chat).

**Example:**
```bash
celeste collections enable collection_abc123

✅ Collection enabled: collection_abc123

Active collections: 2
  - collection_abc123
  - collection_def456

The collections_search tool is now available in chat.
```

---

### `celeste collections disable <collection-id>`

Remove a collection from the active set (disable from chat).

**Example:**
```bash
celeste collections disable collection_abc123

✅ Collection disabled: collection_abc123

Remaining active collections: 1
  - collection_def456
```

---

### `celeste collections show <collection-id>`

Display detailed information about a collection.

**Example:**
```bash
celeste collections show collection_abc123

============================================================
Collection: celeste-lore
============================================================
ID:          collection_abc123def
Description: Celeste personality and lore documents
Status:      Active ✓
Documents:   15
Created:     2026-02-17 10:30:00
============================================================
```

---

### `celeste collections delete <collection-id>`

Delete a collection and all its documents.

**Flags:**
- `-f, --force` - Skip confirmation prompt

**Example:**
```bash
celeste collections delete collection_abc123

⚠️  This will permanently delete collection collection_abc123 and all its documents.
Continue? (y/N): y

Deleting collection collection_abc123...
✅ Collection deleted successfully.
```

---

## Interactive TUI

Open the collections management view from chat:

```
/collections
```

### TUI Features

**Collections List:**
- Shows active and available collections
- Document count and metadata
- Navigate with arrow keys

**Keybindings:**
- `↑/↓` or `k/j` - Navigate list
- `Space` - Toggle active/inactive
- `Q` or `Esc` - Return to chat

**Example View:**
```
Active Collections (2):
> ✓ celeste-lore          (15 docs)  [Created: 2 days ago]
  ✓ nikke-wiki            (42 docs)  [Created: 1 week ago]

Available Collections (1):
  ○ archived-logs         (120 docs) [Created: 1 mo ago]

[↑/↓] Navigate  [Space] Toggle  [Q] Back to Chat
```

---

## Best Practices

### 1. Organize by Topic

Create separate collections for different topics:
- `celeste-lore` - Character personality and backstory
- `project-docs` - Project-specific documentation
- `nikke-wiki` - Game information
- `user-notes` - Personal notes and reminders

### 2. Use Descriptive Names

Good names help you remember what's in each collection:
- ✅ `celeste-personality-v2`
- ✅ `project-api-docs`
- ❌ `collection1`
- ❌ `stuff`

### 3. Keep Documents Under 10MB

Large files are rejected:
- Split large PDFs into chapters
- Extract text from presentations
- Use markdown for long documents

### 4. Prefer Markdown

Markdown files provide best results:
- Clean formatting
- Preserved structure (headers, lists)
- Lightweight (fast uploads)

### 5. Enable Only Relevant Collections

Too many active collections can dilute search results:
- Enable collections relevant to current conversation
- Disable collections you're not using
- Use `/collections` in TUI to toggle quickly

---

## How Collections Work

### Under the Hood

1. **Upload Phase:**
   - Documents are sent to xAI Management API
   - xAI processes and indexes the content
   - Collection ID is stored in local config

2. **Query Phase:**
   - Active collections are sent with each chat request
   - xAI enables the `collections_search` built-in tool
   - LLM automatically decides when to search

3. **Search:**
   - When needed, LLM calls `collections_search`
   - xAI searches across active collections
   - Relevant chunks are returned
   - LLM uses context to generate response

### Semantic vs Keyword Search

Collections use **semantic search** by default:
- Understands meaning, not just exact matches
- Finds related concepts
- Works across synonyms

Example:
- Query: "How does Celeste feel about humanity?"
- Matches: "corrupted AI's perspective on humans", "relationship with mortals"

---

## Troubleshooting

### "Management API key not configured"

**Solution:**
```bash
# Set in config
celeste config --set-management-key xai-YOUR-KEY

# Or use environment variable
export XAI_MANAGEMENT_API_KEY=xai-YOUR-KEY
```

### "File too large" error

**Solution:**
- Files must be ≤ 10MB
- Split large files or compress them
- Extract text from media-heavy PDFs

### "Unsupported format" error

**Supported formats:**
- `.md` (Markdown)
- `.txt` (Plain text)
- `.pdf` (PDF)
- `.html`, `.htm` (HTML)

### Collections not searching in chat

**Checklist:**
1. Collection is enabled: `celeste collections list` (should show ✓)
2. Using xAI provider: `celeste config --show` (check `base_url`)
3. Collection has documents: `celeste collections show <id>`
4. Provider is Grok: Collections only work with xAI

### "Collection not found" error

**Possible causes:**
- Collection was deleted
- Using wrong collection ID
- Collection belongs to different API key

**Solution:**
```bash
# List all collections
celeste collections list

# Remove from active if deleted
celeste collections disable <collection-id>
```

---

## Configuration File

Collections settings in `~/.celeste/config.json`:

```json
{
  "xai_management_api_key": "xai-token-...",
  "collections": {
    "enabled": true,
    "active_collections": [
      "collection_abc123",
      "collection_def456"
    ],
    "auto_enable": true
  }
}
```

**Fields:**
- `xai_management_api_key` - Management API key for collections
- `collections.enabled` - Enable/disable collections globally
- `collections.active_collections` - List of collection IDs to search
- `collections.auto_enable` - Auto-enable collections_search when active_collections is set

---

## API Reference

For advanced usage, see:
- [xAI Collections API Documentation](https://docs.x.ai/docs/collections-api)
- [Using Collections via API](https://docs.x.ai/docs/guides/using-collections/api)
- [Collections Search Tool](https://docs.x.ai/docs/guides/tools/collections-search-tool)

---

## FAQ

**Q: Do collections work with OpenAI?**
A: No, collections are an xAI-specific feature. Only works with Grok models.

**Q: How many collections can I have?**
A: No documented limit. Start with 5-10 for best performance.

**Q: How much does it cost?**
A: Collections storage and search are included with xAI API usage. Only chat API tokens are billed.

**Q: Can I share collections with others?**
A: Collections are tied to your Management API key. Share the collection ID, but recipient needs access to your account.

**Q: How long are documents stored?**
A: Documents persist until you delete the collection. No automatic expiration.

**Q: Can I update a document?**
A: Delete the document and re-upload the updated version. No direct update API.

**Q: What's the search quality like?**
A: Excellent for semantic search. Works best with well-structured markdown documents.

---

## Next Steps

- **Add more documents:** `celeste collections upload <id> <files>`
- **Try web_search:** `celeste config --enable-web-search`
- **Read Provider Docs:** [docs/LLM_PROVIDERS.md](LLM_PROVIDERS.md)
- **Report Issues:** [GitHub Issues](https://github.com/whykusanagi/celesteCLI/issues)

---

**Built with 💜 by [@whykusanagi](https://github.com/whykusanagi)**
```

**Step 2: Commit**

```bash
git add docs/COLLECTIONS.md
git commit -m "docs: add comprehensive Collections guide

- Complete usage guide from setup to troubleshooting
- CLI commands reference with examples
- TUI usage and keybindings
- Best practices and tips
- FAQ and troubleshooting section"
```

---

### Task 5.2: Update README and LLM_PROVIDERS

**Files:**
- Modify: `README.md`
- Modify: `docs/LLM_PROVIDERS.md`

**Step 1: Add Collections section to README**

Find the "Features" section in `README.md` and add after "Skills System":

```markdown
### Collections Support (xAI RAG)
- **Upload Custom Documents** - Create knowledge bases with your own documentation
- **Semantic Search** - Celeste automatically searches collections when answering questions
- **Interactive TUI** - Manage collections with `/collections` command in chat
- **CLI Management** - Create, upload, enable/disable collections from command line
- **Multiple Collections** - Organize by topic, enable only what's relevant

[See Collections Guide](docs/COLLECTIONS.md) for setup and usage.
```

**Step 2: Add Collections to feature list**

Find the feature bullet points and add:

```markdown
- 🗂️ **Collections** - Upload documents for RAG (xAI only)
```

**Step 3: Update LLM_PROVIDERS.md**

Add Collections column to the compatibility matrix:

```markdown
| Provider | Function Calling | Collections | Status | Notes |
|----------|------------------|-------------|---------|-------|
| **OpenAI** | ✅ Native | ❌ | ✅ Tested | Gold standard |
| **Grok (xAI)** | ✅ OpenAI-Compatible | ✅ Native | ✅ Tested | 2M context, Collections API |
| **Venice.ai** | ⚠️ Model-Dependent | ❌ | ✅ Tested | llama-3.3-70b supports tools |
...
```

Add new section:

```markdown
## xAI Collections (RAG)

**What are Collections?**

Collections are xAI's RAG (Retrieval-Augmented Generation) system for uploading custom documents that Celeste can search during conversations.

**Setup:**

1. Get Management API key from [console.x.ai](https://console.x.ai)
2. Configure: `celeste config --set-management-key xai-YOUR-KEY`
3. Create collection: `celeste collections create "my-docs"`
4. Upload files: `celeste collections upload <id> docs/*.md`
5. Enable: `celeste collections enable <id>`

**Documentation:**
- [Complete Collections Guide](COLLECTIONS.md)
- [xAI Collections API](https://docs.x.ai/docs/collections-api)
```

**Step 4: Commit**

```bash
git add README.md docs/LLM_PROVIDERS.md
git commit -m "docs: update README and LLM_PROVIDERS for Collections

- Add Collections section to README features
- Update provider compatibility matrix
- Add xAI Collections setup guide to LLM_PROVIDERS
- Link to Collections guide"
```

---

### Task 5.3: Update CHANGELOG

**Files:**
- Modify: `CHANGELOG.md`

**Step 1: Add new version section**

Add at top of `CHANGELOG.md`:

```markdown
## [Unreleased]

### Added
- **Collections Support** - xAI Collections integration for RAG capabilities
  - Collections API client for Management API
  - CLI commands: create, list, upload, delete, enable, disable, show
  - Interactive TUI view with `/collections` command
  - Automatic `collections_search` tool integration with Grok
  - Document validation (format, size limits)
  - Batch upload with recursive directory support
- **xAI Built-in Tools** - Support for web_search and x_search
  - Config flags: `xai_features.enable_web_search`, `xai_features.enable_x_search`
  - Server-side execution (no local handling required)
- **Management API Key** - Separate API key configuration for Collections
  - Config field: `xai_management_api_key`
  - Environment variable: `XAI_MANAGEMENT_API_KEY`

### Changed
- LLM backend now includes built-in tools (collections_search, web_search, x_search)
- Config struct extended with Collections and XAIFeatures settings
- TUI supports multiple view modes (chat, collections)

### Documentation
- Added `docs/COLLECTIONS.md` - Complete Collections usage guide
- Added `docs/plans/2026-02-17-collections-support-design.md` - Design document
- Updated `README.md` with Collections section
- Updated `docs/LLM_PROVIDERS.md` with Collections compatibility

### Technical
- New package: `cmd/celeste/collections/` with Client and Manager
- Collections TUI model with Bubble Tea integration
- File validation for uploads (10MB limit, format checking)
- Unit tests for Collections client and manager
```

**Step 2: Commit**

```bash
git add CHANGELOG.md
git commit -m "docs: update CHANGELOG for Collections support

- Document all new features (Collections, xAI tools)
- List added CLI commands and TUI enhancements
- Document config changes and new fields
- Note documentation additions"
```

---

## Phase 6: Secondary Features (web_search, x_search)

### Task 6.1: Add Config Commands for xAI Features

**Files:**
- Modify: `cmd/celeste/commands/commands.go`

**Step 1: Find config command handler**

```bash
grep -n "func.*configCommand" cmd/celeste/commands/commands.go
```

**Step 2: Add flags for xAI features**

Find the `configCommand` function and add new flags:

```go
// Add these flags to the config command
cmd.Flags().Bool("enable-web-search", false, "Enable xAI web_search tool")
cmd.Flags().Bool("disable-web-search", false, "Disable xAI web_search tool")
cmd.Flags().Bool("enable-x-search", false, "Enable xAI x_search tool (Twitter/X)")
cmd.Flags().Bool("disable-x-search", false, "Disable xAI x_search tool")
cmd.Flags().String("set-management-key", "", "Set xAI Management API key")
```

**Step 3: Handle the flags in RunE**

Add to the RunE function:

```go
// Handle xAI features flags
if cmd.Flags().Changed("enable-web-search") {
	if cfg.XAIFeatures == nil {
		cfg.XAIFeatures = &config.XAIFeaturesConfig{}
	}
	cfg.XAIFeatures.EnableWebSearch = true
	fmt.Println("✅ web_search enabled")
	changed = true
}

if cmd.Flags().Changed("disable-web-search") {
	if cfg.XAIFeatures != nil {
		cfg.XAIFeatures.EnableWebSearch = false
		fmt.Println("✅ web_search disabled")
		changed = true
	}
}

if cmd.Flags().Changed("enable-x-search") {
	if cfg.XAIFeatures == nil {
		cfg.XAIFeatures = &config.XAIFeaturesConfig{}
	}
	cfg.XAIFeatures.EnableXSearch = true
	fmt.Println("✅ x_search enabled")
	changed = true
}

if cmd.Flags().Changed("disable-x-search") {
	if cfg.XAIFeatures != nil {
		cfg.XAIFeatures.EnableXSearch = false
		fmt.Println("✅ x_search disabled")
		changed = true
	}
}

if cmd.Flags().Changed("set-management-key") {
	key, _ := cmd.Flags().GetString("set-management-key")
	cfg.XAIManagementAPIKey = key
	fmt.Println("✅ Management API key updated")
	changed = true
}
```

**Step 4: Build and test**

```bash
go build -o celeste ./cmd/celeste
./celeste config --help
```

Expected: Shows new xAI flags

**Step 5: Commit**

```bash
git add cmd/celeste/commands/commands.go
git commit -m "feat(config): add xAI features config flags

- Add --enable-web-search / --disable-web-search flags
- Add --enable-x-search / --disable-x-search flags
- Add --set-management-key flag
- Enable xAI built-in tools via config commands"
```

---

### Task 6.2: Document xAI Features

**Files:**
- Modify: `docs/LLM_PROVIDERS.md`

**Step 1: Add xAI Built-in Tools section**

Add after Collections section in `docs/LLM_PROVIDERS.md`:

```markdown
## xAI Built-in Tools

Grok provides server-side tools that execute automatically:

### web_search

Search the web for current information.

**Enable:**
```bash
celeste config --enable-web-search
```

**Use Case:**
- Current events and news
- Real-time data (stock prices, weather)
- Fact-checking

**Example:**
```
You: What's the latest news about AI?
Celeste: *uses web_search automatically*
Celeste: According to recent articles...
```

---

### x_search

Search Twitter/X for social media content.

**Enable:**
```bash
celeste config --enable-x-search
```

**Use Case:**
- Twitter trends
- Social media sentiment
- Real-time discussions

**Example:**
```
You: What are people saying about the new game release?
Celeste: *uses x_search automatically*
Celeste: Based on recent tweets...
```

---

### Configuration

Built-in tools are configured in `~/.celeste/config.json`:

```json
{
  "xai_features": {
    "enable_web_search": true,
    "enable_x_search": false
  }
}
```

**Note:** These tools are xAI-specific and only work with Grok models.
```

**Step 2: Commit**

```bash
git add docs/LLM_PROVIDERS.md
git commit -m "docs: document xAI built-in tools (web_search, x_search)

- Add web_search documentation and examples
- Add x_search documentation and examples
- Document config commands
- Note xAI-only compatibility"
```

---

## Final Steps

### Task 7.1: Run All Tests

**Step 1: Run all tests**

```bash
go test ./... -v
```

Expected: All tests PASS

**Step 2: Run with coverage**

```bash
go test ./... -cover
```

Expected: Shows coverage percentages

**Step 3: If tests fail, fix and re-run**

---

### Task 7.2: Build Final Binary

**Step 1: Clean build**

```bash
rm -f celeste
go build -o celeste ./cmd/celeste
```

**Step 2: Test basic commands**

```bash
./celeste --version
./celeste --help
./celeste collections --help
./celeste config --help
```

Expected: All commands work

**Step 3: Test TUI launch**

```bash
# Set a dummy API key if needed
export CELESTE_API_KEY="test"
timeout 2s ./celeste chat || true
```

Expected: TUI starts and exits cleanly

---

### Task 7.3: Final Commit and Summary

**Step 1: Check git status**

```bash
git status
```

Expected: All changes committed

**Step 2: Create summary commit if needed**

If there are uncommitted changes:

```bash
git add -A
git commit -m "chore: final cleanup and polish

- Fix any remaining linting issues
- Update comments and documentation
- Ready for testing and review"
```

**Step 3: View commit log**

```bash
git log --oneline --graph -20
```

Expected: Shows all implementation commits

---

## Plan Complete!

The implementation plan is now complete. All features are implemented:

**✅ Phase 1: Core Collections API**
- Collections client (create, list, upload, delete)
- Collections manager (enable/disable, validation)
- Unit tests with mock HTTP

**✅ Phase 2: LLM Integration**
- collections_search tool in backend
- Collections config passed to LLM
- Built-in tools support

**✅ Phase 3: CLI Commands**
- 7 collections subcommands
- Batch upload with validation
- Enable/disable management

**✅ Phase 4: TUI Enhancement**
- Collections TUI model
- Interactive view with keybindings
- Toggle active/inactive

**✅ Phase 5: Documentation**
- Comprehensive Collections guide
- Updated README and LLM_PROVIDERS
- CHANGELOG updated

**✅ Phase 6: Secondary Features**
- web_search and x_search config
- Config command flags
- Documentation

---

## Testing Checklist

Manual testing before merge:

- [ ] Create collection via CLI
- [ ] Upload single file
- [ ] Upload directory (recursive)
- [ ] List collections
- [ ] Enable/disable collection
- [ ] Show collection details
- [ ] Delete collection
- [ ] Open `/collections` in TUI
- [ ] Toggle collection in TUI
- [ ] Navigate with arrow keys
- [ ] Return to chat with Q
- [ ] Verify collections_search in API requests (check logs)
- [ ] Test with actual query that uses collection
- [ ] Enable web_search, verify in API
- [ ] Enable x_search, verify in API
- [ ] All unit tests pass

---

## Estimated Timeline

- Phase 1: 1-2 days
- Phase 2: 1 day
- Phase 3: 1-2 days (CLI commands)
- Phase 4: 1-2 days (TUI)
- Phase 5: 0.5 days (docs)
- Phase 6: 0.5 days (secondary)

**Total: 5-6 days**

---

## Next Steps After Implementation

1. **Review & Testing:** Thoroughly test all features
2. **Integration Test:** Test with real xAI API
3. **Code Review:** Use superpowers:requesting-code-review skill
4. **Merge:** Create PR to main branch
5. **Release:** Tag new version with Collections support
6. **Announce:** Update users about new feature

---

**End of Implementation Plan**
