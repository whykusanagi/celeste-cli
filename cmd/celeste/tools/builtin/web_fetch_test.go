package builtin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWebFetchTool(t *testing.T) {
	tool := NewWebFetchTool()
	assert.Equal(t, "web_fetch", tool.Name())
	assert.True(t, tool.IsReadOnly())
	assert.True(t, tool.IsConcurrencySafe(nil))
}

func TestWebFetchTool_MissingURL(t *testing.T) {
	tool := NewWebFetchTool()
	err := tool.ValidateInput(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "url")
}

func TestWebFetchTool_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><h1>Hello World</h1><p>Test paragraph.</p></body></html>"))
	}))
	defer srv.Close()

	tool := NewWebFetchTool()
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL}, nil)
	require.NoError(t, err)
	assert.False(t, result.Error)
	assert.Contains(t, result.Content, "Hello World")
	assert.Contains(t, result.Content, "Test paragraph")
}

func TestWebFetchTool_WithPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><body><p>Content</p></body></html>"))
	}))
	defer srv.Close()

	tool := NewWebFetchTool()
	result, err := tool.Execute(context.Background(), map[string]any{
		"url":    srv.URL,
		"prompt": "Extract the main content",
	}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "Extraction Guidance")
	assert.Contains(t, result.Content, "Extract the main content")
}

func TestWebFetchTool_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	tool := NewWebFetchTool()
	result, err := tool.Execute(context.Background(), map[string]any{"url": srv.URL}, nil)
	require.NoError(t, err)
	assert.True(t, result.Error)
	assert.Contains(t, result.Content, "404")
}
