package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/genai"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
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

// Gemini 3.x rejects the turn after a tool call with "Function call is missing
// a thought_signature" unless the signature that arrived on the functionCall
// part is echoed back verbatim. These cover both halves of that round trip.

func TestGoogleFunctionCallCapturesThoughtSignature(t *testing.T) {
	backend := &GoogleBackend{}
	sig := []byte("opaque-signature-bytes")

	result := backend.convertFunctionCallToResult(&genai.FunctionCall{
		Name: "read_file",
		Args: map[string]any{"path": "main.go"},
	}, sig)

	assert.Equal(t, "read_file", result.Name)
	assert.Equal(t, sig, result.ThoughtSignature,
		"signature from the enclosing Part must reach ToolCallResult")
}

func TestGoogleOutboundEchoesThoughtSignature(t *testing.T) {
	backend := &GoogleBackend{}
	sig := []byte("opaque-signature-bytes")

	contents := backend.convertMessagesToGenAI([]tui.ChatMessage{
		{
			Role: "assistant",
			ToolCalls: []tui.ToolCallInfo{
				{
					ID:               "call_read_file",
					Name:             "read_file",
					Arguments:        `{"path":"main.go"}`,
					ThoughtSignature: sig,
				},
			},
		},
	})

	require.Len(t, contents, 1)
	var fnPart *genai.Part
	for _, p := range contents[0].Parts {
		if p.FunctionCall != nil {
			fnPart = p
			break
		}
	}
	require.NotNil(t, fnPart, "expected a functionCall part on the outbound turn")
	assert.Equal(t, sig, fnPart.ThoughtSignature,
		"outbound functionCall must echo the signature or Gemini 3.x returns 400")
}

// A provider that never supplies a signature must not gain a bogus empty one.
func TestGoogleOutboundOmitsAbsentThoughtSignature(t *testing.T) {
	backend := &GoogleBackend{}

	contents := backend.convertMessagesToGenAI([]tui.ChatMessage{
		{
			Role:      "assistant",
			ToolCalls: []tui.ToolCallInfo{{ID: "c1", Name: "ls", Arguments: `{}`}},
		},
	})

	require.Len(t, contents, 1)
	for _, p := range contents[0].Parts {
		if p.FunctionCall != nil {
			assert.Empty(t, p.ThoughtSignature)
		}
	}
}
