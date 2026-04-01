package builtin

import (
	"context"
	"encoding/base64"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// Base64EncodeTool encodes text to base64.
type Base64EncodeTool struct {
	BaseTool
}

// NewBase64EncodeTool creates a Base64EncodeTool.
func NewBase64EncodeTool() *Base64EncodeTool {
	return &Base64EncodeTool{
		BaseTool: BaseTool{
			ToolName:        "base64_encode",
			ToolDescription: "Encode a string to base64",
			ToolParameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to encode",
					},
				},
				"required": []string{"text"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"text"},
		},
	}
}

func (t *Base64EncodeTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	text, ok := input["text"].(string)
	if !ok || text == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'text' parameter is required",
			"Please provide the text you want to encode.",
			map[string]interface{}{
				"skill": "base64_encode",
				"field": "text",
			},
		))
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(text))

	return resultFromMap(map[string]interface{}{
		"original": text,
		"encoded":  encoded,
	})
}

// Base64DecodeTool decodes a base64 string.
type Base64DecodeTool struct {
	BaseTool
}

// NewBase64DecodeTool creates a Base64DecodeTool.
func NewBase64DecodeTool() *Base64DecodeTool {
	return &Base64DecodeTool{
		BaseTool: BaseTool{
			ToolName:        "base64_decode",
			ToolDescription: "Decode a base64 string",
			ToolParameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"encoded": map[string]interface{}{
						"type":        "string",
						"description": "Base64 encoded string to decode",
					},
				},
				"required": []string{"encoded"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"encoded"},
		},
	}
}

func (t *Base64DecodeTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	encoded, ok := input["encoded"].(string)
	if !ok || encoded == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'encoded' parameter is required",
			"Please provide the base64 encoded string you want to decode.",
			map[string]interface{}{
				"skill": "base64_decode",
				"field": "encoded",
			},
		))
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"Invalid base64 string",
			"The provided string is not valid base64 encoded data.",
			map[string]interface{}{
				"skill": "base64_decode",
				"field": "encoded",
				"error": err.Error(),
			},
		))
	}

	return resultFromMap(map[string]interface{}{
		"encoded": encoded,
		"decoded": string(decoded),
	})
}
