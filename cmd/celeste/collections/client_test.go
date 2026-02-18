package collections

import (
	"encoding/json"
	"io"
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
		assert.Equal(t, "/collections", r.URL.Path)
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

func TestClient_ListCollections(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/collections", r.URL.Path)
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

func TestClient_UploadDocument(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/collections/col_123/documents", r.URL.Path)
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

func TestClient_DeleteCollection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/collections/col_123", r.URL.Path)
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient("test-key")
	client.baseURL = server.URL

	err := client.DeleteCollection("col_123")
	assert.NoError(t, err)
}
