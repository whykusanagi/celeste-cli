// Package skills provides the skill registry and execution system.
package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// ExecutionResult represents the result of a skill execution.
type ExecutionResult struct {
	Success bool        `json:"success"`
	Result  interface{} `json:"result,omitempty"`
	Error   string      `json:"error,omitempty"`
	Time    time.Time   `json:"timestamp"`
}

// ExecutionContext provides context for skill execution.
type ExecutionContext struct {
	Ctx      context.Context
	Registry *Registry
	OnStatus func(status string) // Callback for status updates
}

// Executor handles skill execution with context and callbacks.
type Executor struct {
	registry *Registry
}

// NewExecutor creates a new skill executor.
func NewExecutor(registry *Registry) *Executor {
	return &Executor{
		registry: registry,
	}
}

// Execute runs a skill by name with the given arguments.
func (e *Executor) Execute(ctx context.Context, name string, argsJSON string) (*ExecutionResult, error) {
	result := &ExecutionResult{
		Time: time.Now(),
	}

	// Parse arguments
	var args map[string]interface{}
	if argsJSON != "" {
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			result.Error = fmt.Sprintf("failed to parse arguments: %v", err)
			return result, fmt.Errorf("failed to parse arguments: %w", err)
		}
	} else {
		args = make(map[string]interface{})
	}

	// Execute skill
	output, err := e.registry.Execute(name, args)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	result.Success = true
	result.Result = output
	return result, nil
}

// ExecuteFromToolCall executes a skill from an OpenAI tool call format.
func (e *Executor) ExecuteFromToolCall(ctx context.Context, toolCall ToolCall) (*ExecutionResult, error) {
	return e.Execute(ctx, toolCall.Function.Name, toolCall.Function.Arguments)
}

// ToolCall represents an OpenAI tool call.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall represents the function details in a tool call.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// FormatResultForLLM formats an execution result for sending back to the LLM.
func FormatResultForLLM(toolCallID string, result *ExecutionResult) map[string]interface{} {
	content := ""
	if result.Success {
		// Format result as string
		switch v := result.Result.(type) {
		case string:
			content = v
		case map[string]interface{}:
			b, _ := json.Marshal(v)
			content = string(b)
		default:
			b, _ := json.Marshal(result.Result)
			content = string(b)
		}
	} else {
		content = fmt.Sprintf("Error: %s", result.Error)
	}

	return map[string]interface{}{
		"role":         "tool",
		"tool_call_id": toolCallID,
		"content":      content,
	}
}

// ParseToolCalls extracts tool calls from an LLM response.
func ParseToolCalls(response map[string]interface{}) ([]ToolCall, error) {
	choices, ok := response["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		return nil, nil
	}

	choice, ok := choices[0].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	message, ok := choice["message"].(map[string]interface{})
	if !ok {
		return nil, nil
	}

	toolCallsRaw, ok := message["tool_calls"].([]interface{})
	if !ok {
		return nil, nil
	}

	var toolCalls []ToolCall
	for _, tc := range toolCallsRaw {
		tcMap, ok := tc.(map[string]interface{})
		if !ok {
			continue
		}

		funcMap, ok := tcMap["function"].(map[string]interface{})
		if !ok {
			continue
		}

		toolCall := ToolCall{
			ID:   getString(tcMap, "id"),
			Type: getString(tcMap, "type"),
			Function: FunctionCall{
				Name:      getString(funcMap, "name"),
				Arguments: getString(funcMap, "arguments"),
			},
		}

		toolCalls = append(toolCalls, toolCall)
	}

	return toolCalls, nil
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}
