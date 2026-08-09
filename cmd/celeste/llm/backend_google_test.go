package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoogleConvertSchemaRequiredFromStringSlice(t *testing.T) {
	backend := &GoogleBackend{}

	schema := backend.convertSchemaToGenAI(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []string{"location"},
	})

	require.NotNil(t, schema)
	assert.Equal(t, []string{"location"}, schema.Required)
}

func TestGoogleConvertSchemaRequiredFromInterfaceSlice(t *testing.T) {
	backend := &GoogleBackend{}

	schema := backend.convertSchemaToGenAI(map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"location": map[string]interface{}{
				"type": "string",
			},
		},
		"required": []interface{}{"location"},
	})

	require.NotNil(t, schema)
	assert.Equal(t, []string{"location"}, schema.Required)
}

// Google retired the v1 endpoint for current models: on v1, gemini-3.6-flash
// and gemini-2.5-flash both 404 even though /v1/models still lists them, and
// gemini-2.0-flash returns "no longer available". v1beta serves all of them.
// AI Studio must therefore use v1beta. Vertex (aiplatform.googleapis.com) keeps
// v1, which is its correct convention.
func TestGoogleAPIVersionForBaseURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		want    string
	}{
		{"ai studio default (empty)", "", "v1beta"},
		{"ai studio explicit", "https://generativelanguage.googleapis.com/v1beta", "v1beta"},
		{"ai studio openai-compat", "https://generativelanguage.googleapis.com/v1beta/openai", "v1beta"},
		{"vertex", "https://aiplatform.googleapis.com/v1/projects/p/locations/l", "v1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := googleAPIVersion(c.baseURL); got != c.want {
				t.Errorf("googleAPIVersion(%q) = %q, want %q", c.baseURL, got, c.want)
			}
		})
	}
}
