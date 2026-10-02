package llm

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sashabaranov/go-openai"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// Responses API input items (2.0 W8). Field order is fixed by the structs,
// so the same history always encodes to the same bytes (prompt caching).
type respMessage struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type respContentPart struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

type respFunctionCall struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type respFunctionCallOutput struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

// responsesInput converts the history to Responses input items. A message
// whose blocks came from this endpoint and model (key) is sent as its
// recorded output items, in order, instead of its neutral view (2.0 F3
// precedence); replayed reports whether any message was. The system prompt
// is not here: it goes in the request's instructions.
func responsesInput(messages []tui.ChatMessage, key string) (items []json.RawMessage, replayed bool) {
	add := func(v any) {
		if b, err := json.Marshal(v); err == nil {
			items = append(items, b)
		}
	}
	for _, msg := range messages {
		if raws, ok := tui.ReplayBlocks(msg, key); ok {
			for _, raw := range raws {
				items = append(items, replayItem(raw))
			}
			replayed = true
			continue
		}
		switch msg.Role {
		case "tool":
			add(respFunctionCallOutput{Type: "function_call_output", CallID: msg.ToolCallID, Output: msg.Content})
			if part, name, ok := imagePart(msg.Metadata); ok {
				add(respMessage{Type: "message", Role: "user", Content: []respContentPart{
					{Type: "input_text", Text: fmt.Sprintf("[Attached image from tool result: %s]", name)},
					part,
				}})
			}
		case "assistant":
			if msg.Content != "" {
				add(respMessage{Type: "message", Role: "assistant", Content: msg.Content})
			}
			for _, tc := range msg.ToolCalls {
				add(respFunctionCall{Type: "function_call", CallID: tc.ID, Name: tc.Name, Arguments: tc.Arguments})
			}
		default: // user, system
			if msg.Content != "" {
				add(respMessage{Type: "message", Role: msg.Role, Content: msg.Content})
			}
		}
	}
	return items, replayed
}

// imagePart returns a tool result's image (metadata type "image") as an
// input_image part, with the file name for the caption.
func imagePart(md map[string]any) (respContentPart, string, bool) {
	if md == nil {
		return respContentPart{}, "", false
	}
	if t, _ := md["type"].(string); t != "image" {
		return respContentPart{}, "", false
	}
	b64, ok := md["base64"].(string)
	if !ok {
		return respContentPart{}, "", false
	}
	format, _ := md["format"].(string)
	if format == "" {
		format = "png"
	}
	name, _ := md["filename"].(string)
	return respContentPart{Type: "input_image", ImageURL: fmt.Sprintf("data:image/%s;base64,%s", format, b64), Detail: "auto"}, name, true
}

// replayItem returns a stored output item as an input item. Requests use
// store=false, so OpenAI keeps no items to resolve ids against: every item
// but a reasoning item is sent without its "id". A reasoning item is sent
// byte for byte (its encrypted_content is what the API reads). raw itself
// is never modified (block bytes are read-only, F3).
func replayItem(raw json.RawMessage) json.RawMessage {
	var head struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &head) != nil || head.Type == "reasoning" {
		return raw
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return raw
	}
	if _, ok := fields["id"]; !ok {
		return raw
	}
	delete(fields, "id")
	out, err := json.Marshal(fields)
	if err != nil {
		return raw
	}
	return out
}

// responsesTools converts skill definitions to Responses function tools:
// flat (name, description and parameters next to type) and explicitly not
// strict, because the Responses API defaults function tools to strict
// schemas that celeste's tool schemas do not satisfy.
func responsesTools(tools []tui.SkillDefinition) []openai.Tool {
	var out []openai.Tool
	for _, t := range tools {
		params, err := json.Marshal(t.Parameters)
		if err != nil {
			tui.LogInfo(fmt.Sprintf("Skipping invalid tool '%s': failed to marshal parameters: %v", t.Name, err))
			continue
		}
		fields := map[string]any{"name": t.Name, "parameters": json.RawMessage(params), "strict": false}
		if t.Description != "" {
			fields["description"] = t.Description
		}
		out = append(out, openai.Tool{Type: openai.ToolTypeFunction, Parameters: fields})
	}
	return out
}

// openAIReasoningModel reports an OpenAI model that reasons, takes a
// reasoning effort (reasoning.effort on Responses, reasoning_effort on Chat
// Completions) and accepts include reasoning.encrypted_content: the o1, o3,
// o4 and gpt-5 families, except gpt-5-chat, which does not reason. OpenAI
// answers a request that asks a non-reasoning model for encrypted reasoning
// with a 400 ("Encrypted content is not supported with this model."), so
// this one predicate gates include and the effort on both APIs.
func openAIReasoningModel(model string) bool {
	m := strings.ToLower(model)
	if strings.HasPrefix(m, "gpt-5-chat") {
		return false
	}
	for _, p := range []string{"o1", "o3", "o4", "gpt-5"} {
		if strings.HasPrefix(m, p) {
			return true
		}
	}
	return false
}

// reasoningEffort maps celeste's thinking level to an OpenAI-style
// reasoning effort (low, medium, high; max is high). "" sends none.
func reasoningEffort(tc ThinkingConfig) string {
	if !tc.Enabled {
		return ""
	}
	switch tc.Level {
	case "low", "medium", "high":
		return tc.Level
	case "max":
		return "high"
	}
	return ""
}

// openAIEffort is reasoningEffort for a model that takes one
// (openAIReasoningModel), else "".
func openAIEffort(model string, tc ThinkingConfig) string {
	if !openAIReasoningModel(model) {
		return ""
	}
	return reasoningEffort(tc)
}
