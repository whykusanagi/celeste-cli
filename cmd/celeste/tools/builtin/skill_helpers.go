package builtin

import (
	"encoding/json"
	"fmt"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// formatErrorResponse creates a structured error response for LLM interpretation.
func formatErrorResponse(errorType, message, hint string, context map[string]any) map[string]any {
	result := map[string]any{
		"error":      true,
		"error_type": errorType,
		"message":    message,
	}
	if hint != "" {
		result["hint"] = hint
	}
	// Merge context into result
	for k, v := range context {
		result[k] = v
	}
	return result
}

// getUserOrDefault gets a value from args first, then falls back to config default.
func getUserOrDefault(args map[string]any, key string, configGetter func() string) (string, bool) {
	if val, ok := args[key].(string); ok && val != "" {
		return val, true
	}
	if configGetter != nil {
		defaultVal := configGetter()
		if defaultVal != "" {
			return defaultVal, true
		}
	}
	return "", false
}

// formatConfigError creates a structured error when both user value and config default are missing.
func formatConfigError(skillName, fieldName, configCommand string) map[string]any {
	return formatErrorResponse(
		"config_error",
		fmt.Sprintf("%s is required for %s. Please provide %s in your request, or set a default using: %s", fieldName, skillName, fieldName, configCommand),
		fmt.Sprintf("You can ask the user for their %s or location", fieldName),
		map[string]any{
			"skill":          skillName,
			"field":          fieldName,
			"config_command": configCommand,
			"info":           fmt.Sprintf("No default %s configured. User must provide %s in request.", fieldName, fieldName),
		},
	)
}

// resultFromMap converts a map[string]any to a ToolResult by marshalling to JSON.
func resultFromMap(m any) (tools.ToolResult, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return tools.ToolResult{}, fmt.Errorf("failed to marshal result: %w", err)
	}

	// Check if the response contains an error field
	isError := false
	if mp, ok := m.(map[string]any); ok {
		if errVal, ok := mp["error"]; ok {
			if b, ok := errVal.(bool); ok && b {
				isError = true
			}
		}
	}

	return tools.ToolResult{
		Content: string(data),
		Error:   isError,
	}, nil
}

// mustJSON marshals v to json.RawMessage, panicking on error (for static definitions).
func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("mustJSON: %v", err))
	}
	return data
}
