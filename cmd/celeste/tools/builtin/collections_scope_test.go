package builtin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// Aikido 806869660: collections_search stays inside the active collections.
func TestCollectionsSearchStaysInActiveScope(t *testing.T) {
	var got [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Source struct {
				CollectionIDs []string `json:"collection_ids"`
			} `json:"source"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		got = append(got, body.Source.CollectionIDs)
		_, _ = w.Write([]byte(`{"matches":[]}`))
	}))
	defer srv.Close()

	active := []string{"coll-a", "coll-b"}
	cfg := &config.Config{APIKey: "test-key", Collections: &config.CollectionsConfig{ActiveCollections: active}}
	tool := NewCollectionsSearchTool(cfg)
	tool.endpoint = srv.URL
	active[0] = "mutated" // the tool keeps its own copy

	res, err := tool.Execute(context.Background(), map[string]any{"query": "q", "collection_id": "coll-other"}, nil)
	require.NoError(t, err)
	assert.True(t, res.Error)
	assert.Contains(t, res.Content, "not an active collection")
	assert.Empty(t, got, "an out-of-scope collection is never searched")

	res, err = tool.Execute(context.Background(), map[string]any{"query": "q", "collection_id": "coll-b"}, nil)
	require.NoError(t, err)
	assert.False(t, res.Error, res.Content)
	res, err = tool.Execute(context.Background(), map[string]any{"query": "q"}, nil)
	require.NoError(t, err)
	assert.False(t, res.Error, res.Content)
	assert.Equal(t, [][]string{{"coll-b"}, {"coll-a", "coll-b"}}, got)
}
