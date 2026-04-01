package builtin

import (
	"context"
	"crypto/rand"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// PasswordTool generates secure random passwords.
type PasswordTool struct {
	BaseTool
}

// NewPasswordTool creates a PasswordTool.
func NewPasswordTool() *PasswordTool {
	return &PasswordTool{
		BaseTool: BaseTool{
			ToolName:        "generate_password",
			ToolDescription: "Generate a secure random password",
			ToolParameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"length": map[string]interface{}{
						"type":        "integer",
						"description": "Password length (default: 16, min: 8, max: 128)",
					},
					"include_symbols": map[string]interface{}{
						"type":        "boolean",
						"description": "Include special symbols (default: true)",
					},
					"include_numbers": map[string]interface{}{
						"type":        "boolean",
						"description": "Include numbers (default: true)",
					},
				},
				"required": []string{},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
		},
	}
}

func (t *PasswordTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	length := 16
	if l, ok := input["length"].(float64); ok {
		length = int(l)
		if length < 8 {
			length = 8
		}
		if length > 128 {
			length = 128
		}
	}

	includeSymbols := true
	if s, ok := input["include_symbols"].(bool); ok {
		includeSymbols = s
	}

	includeNumbers := true
	if n, ok := input["include_numbers"].(bool); ok {
		includeNumbers = n
	}

	lowercase := "abcdefghijklmnopqrstuvwxyz"
	uppercase := "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	numbers := "0123456789"
	symbols := "!@#$%^&*()_+-=[]{}|;:,.<>?"

	charset := lowercase + uppercase
	if includeNumbers {
		charset += numbers
	}
	if includeSymbols {
		charset += symbols
	}

	password := make([]byte, length)
	for i := range password {
		b := make([]byte, 1)
		_, _ = rand.Read(b)
		password[i] = charset[int(b[0])%len(charset)]
	}

	return resultFromMap(map[string]interface{}{
		"password":        string(password),
		"length":          length,
		"include_symbols": includeSymbols,
		"include_numbers": includeNumbers,
	})
}
