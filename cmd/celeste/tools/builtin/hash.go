package builtin

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// HashTool generates cryptographic hashes.
type HashTool struct {
	BaseTool
}

// NewHashTool creates a HashTool.
func NewHashTool() *HashTool {
	return &HashTool{
		BaseTool: BaseTool{
			ToolName:        "generate_hash",
			ToolDescription: "Generate cryptographic hash (MD5, SHA256, SHA512) for a given string",
			ToolParameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"text": map[string]interface{}{
						"type":        "string",
						"description": "Text to hash",
					},
					"algorithm": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"md5", "sha256", "sha512"},
						"description": "Hash algorithm to use",
					},
				},
				"required": []string{"text", "algorithm"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"text", "algorithm"},
		},
	}
}

func (t *HashTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	text, ok := input["text"].(string)
	if !ok || text == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'text' parameter is required",
			"Please provide the text you want to hash.",
			map[string]interface{}{
				"skill": "generate_hash",
				"field": "text",
			},
		))
	}

	algorithm, ok := input["algorithm"].(string)
	if !ok || algorithm == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'algorithm' parameter is required",
			"Please specify a hash algorithm: 'md5', 'sha256', or 'sha512'.",
			map[string]interface{}{
				"skill": "generate_hash",
				"field": "algorithm",
			},
		))
	}

	algorithm = strings.ToLower(algorithm)
	var hash string

	switch algorithm {
	case "md5":
		h := md5.Sum([]byte(text))
		hash = hex.EncodeToString(h[:])
	case "sha256":
		h := sha256.Sum256([]byte(text))
		hash = hex.EncodeToString(h[:])
	case "sha512":
		h := sha512.Sum512([]byte(text))
		hash = hex.EncodeToString(h[:])
	default:
		return resultFromMap(formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Unsupported algorithm '%s'", algorithm),
			"Please use one of: 'md5', 'sha256', or 'sha512'.",
			map[string]interface{}{
				"skill":     "generate_hash",
				"field":     "algorithm",
				"provided":  algorithm,
				"supported": []string{"md5", "sha256", "sha512"},
			},
		))
	}

	return resultFromMap(map[string]interface{}{
		"text":      text,
		"algorithm": algorithm,
		"hash":      hash,
	})
}
