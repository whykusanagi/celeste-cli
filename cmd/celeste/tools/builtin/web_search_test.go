package builtin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewWebSearchTool(t *testing.T) {
	tool := NewWebSearchTool()
	assert.Equal(t, "web_search", tool.Name())
	assert.True(t, tool.IsReadOnly())
	assert.True(t, tool.IsConcurrencySafe(nil))
}

func TestWebSearchTool_MissingQuery(t *testing.T) {
	tool := NewWebSearchTool()
	err := tool.ValidateInput(map[string]any{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query")
}

func TestWebSearchTool_RateLimit(t *testing.T) {
	tool := NewWebSearchTool()
	tool.searchCount = maxSearchesPerSession

	result, err := tool.Execute(context.Background(), map[string]any{"query": "test"}, nil)
	require.NoError(t, err)
	assert.Contains(t, result.Content, "rate_limit")
}

func TestParseDDGResults(t *testing.T) {
	html := `
	<a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fexample.com&amp;rut=abc">Example Title</a>
	<td class="result__snippet">This is a snippet about the example.</td>
	<a class="result__a" href="https://duckduckgo.com/l/?uddg=https%3A%2F%2Fother.com&amp;rut=xyz"><b>Other</b> Result</a>
	<td class="result__snippet">Another snippet here.</td>
	`
	results := parseDDGResults(html)
	require.Len(t, results, 2)
	assert.Equal(t, "Example Title", results[0].Title)
	assert.Equal(t, "https://example.com", results[0].URL)
	assert.Equal(t, "This is a snippet about the example.", results[0].Snippet)
	assert.Equal(t, "Other Result", results[1].Title)
	assert.Equal(t, "https://other.com", results[1].URL)
}

func TestStripHTMLTags(t *testing.T) {
	assert.Equal(t, "hello world", stripHTMLTags("<b>hello</b> <i>world</i>"))
	assert.Equal(t, "plain", stripHTMLTags("plain"))
}
