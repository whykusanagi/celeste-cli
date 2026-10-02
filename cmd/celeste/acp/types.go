package acp

import "encoding/json"

// The ACP v1 messages celeste uses, spelled as in the published schema
// (agentclientprotocol.com, schema/v1). Optional schema fields celeste never
// reads or sends are left out; unknown incoming fields are ignored.

// ProtocolVersion is the ACP version this agent speaks (and its latest).
const ProtocolVersion = 1

// InitializeParams is the client's initialize request.
type InitializeParams struct {
	ProtocolVersion    int                `json:"protocolVersion"`
	ClientCapabilities ClientCapabilities `json:"clientCapabilities"`
}

// ClientCapabilities are what the editor offers the agent.
type ClientCapabilities struct {
	FS       FileSystemCapabilities `json:"fs"`
	Terminal bool                   `json:"terminal"`
}

// FileSystemCapabilities are the editor's fs/* methods.
type FileSystemCapabilities struct {
	ReadTextFile  bool `json:"readTextFile"`
	WriteTextFile bool `json:"writeTextFile"`
}

// InitializeResult is the agent's answer to initialize.
type InitializeResult struct {
	ProtocolVersion   int               `json:"protocolVersion"`
	AgentCapabilities AgentCapabilities `json:"agentCapabilities"`
	AuthMethods       []AuthMethod      `json:"authMethods"`
}

// AgentCapabilities are what the agent supports.
type AgentCapabilities struct {
	LoadSession        bool               `json:"loadSession"`
	PromptCapabilities PromptCapabilities `json:"promptCapabilities"`
	McpCapabilities    McpCapabilities    `json:"mcpCapabilities"`
}

// PromptCapabilities are the content blocks a prompt may carry beyond text
// and resource links.
type PromptCapabilities struct {
	Image           bool `json:"image"`
	Audio           bool `json:"audio"`
	EmbeddedContext bool `json:"embeddedContext"`
}

// McpCapabilities are the MCP transports the agent accepts from the client
// besides stdio.
type McpCapabilities struct {
	HTTP bool `json:"http"`
	SSE  bool `json:"sse"`
}

// AuthMethod is one way to authenticate (celeste advertises none).
type AuthMethod struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AuthenticateParams is the client's authenticate request.
type AuthenticateParams struct {
	MethodID string `json:"methodId"`
}

// McpServer is an MCP server the client asks the agent to connect. Stdio
// servers carry no type; http and sse ones do.
type McpServer struct {
	Type    string        `json:"type,omitempty"`
	Name    string        `json:"name"`
	Command string        `json:"command,omitempty"`
	Args    []string      `json:"args,omitempty"`
	Env     []EnvVariable `json:"env,omitempty"`
	URL     string        `json:"url,omitempty"`
}

// EnvVariable is one environment variable of a stdio MCP server.
type EnvVariable struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// NewSessionParams is session/new.
type NewSessionParams struct {
	Cwd        string      `json:"cwd"`
	McpServers []McpServer `json:"mcpServers"`
}

// NewSessionResult is session/new's answer.
type NewSessionResult struct {
	SessionID string `json:"sessionId"`
}

// LoadSessionParams is session/load. Its answer is an object (the schema's
// LoadSessionResponse, all fields optional), not null.
type LoadSessionParams struct {
	SessionID  string      `json:"sessionId"`
	Cwd        string      `json:"cwd"`
	McpServers []McpServer `json:"mcpServers"`
}

// PromptParams is session/prompt.
type PromptParams struct {
	SessionID string         `json:"sessionId"`
	Prompt    []ContentBlock `json:"prompt"`
}

// PromptResult is session/prompt's answer.
type PromptResult struct {
	StopReason string `json:"stopReason"`
}

// Stop reasons.
const (
	StopEndTurn         = "end_turn"
	StopMaxTokens       = "max_tokens"
	StopMaxTurnRequests = "max_turn_requests"
	StopRefusal         = "refusal"
	StopCancelled       = "cancelled"
)

// CancelParams is the session/cancel notification.
type CancelParams struct {
	SessionID string `json:"sessionId"`
}

// Content block types.
const (
	BlockText         = "text"
	BlockImage        = "image"
	BlockAudio        = "audio"
	BlockResourceLink = "resource_link"
	BlockResource     = "resource"
)

// ContentBlock is one block of a prompt or a message: text, image, audio,
// resource_link (uri, name) or an embedded resource.
type ContentBlock struct {
	Type     string            `json:"type"`
	Text     string            `json:"text,omitempty"`
	Data     string            `json:"data,omitempty"`
	MimeType string            `json:"mimeType,omitempty"`
	URI      string            `json:"uri,omitempty"`
	Name     string            `json:"name,omitempty"`
	Resource *ResourceContents `json:"resource,omitempty"`
}

// MarshalJSON always writes a text block's text: the schema requires it,
// also when it is empty.
func (b ContentBlock) MarshalJSON() ([]byte, error) {
	if b.Type == BlockText {
		return json.Marshal(struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}{b.Type, b.Text})
	}
	type plain ContentBlock
	return json.Marshal(plain(b))
}

// ResourceContents is an embedded resource: text or a base64 blob.
type ResourceContents struct {
	URI      string `json:"uri"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

// TextBlock is a text content block.
func TextBlock(text string) ContentBlock { return ContentBlock{Type: BlockText, Text: text} }

// SessionNotification is the session/update notification.
type SessionNotification struct {
	SessionID string `json:"sessionId"`
	Update    any    `json:"update"`
}

// Session update kinds.
const (
	UpdateUserMessageChunk  = "user_message_chunk"
	UpdateAgentMessageChunk = "agent_message_chunk"
	UpdateAgentThoughtChunk = "agent_thought_chunk"
	UpdateToolCall          = "tool_call"
	UpdateToolCallUpdate    = "tool_call_update"
	UpdatePlan              = "plan"
)

// ContentChunk is a user_message_chunk, agent_message_chunk or
// agent_thought_chunk update.
type ContentChunk struct {
	SessionUpdate string       `json:"sessionUpdate"`
	Content       ContentBlock `json:"content"`
}

// UserMessageChunk is a user_message_chunk update with text.
func UserMessageChunk(text string) ContentChunk {
	return ContentChunk{SessionUpdate: UpdateUserMessageChunk, Content: TextBlock(text)}
}

// AgentMessageChunk is an agent_message_chunk update with text.
func AgentMessageChunk(text string) ContentChunk {
	return ContentChunk{SessionUpdate: UpdateAgentMessageChunk, Content: TextBlock(text)}
}

// AgentThoughtChunk is an agent_thought_chunk update with text.
func AgentThoughtChunk(text string) ContentChunk {
	return ContentChunk{SessionUpdate: UpdateAgentThoughtChunk, Content: TextBlock(text)}
}

// Tool kinds.
const (
	ToolKindRead       = "read"
	ToolKindEdit       = "edit"
	ToolKindDelete     = "delete"
	ToolKindMove       = "move"
	ToolKindSearch     = "search"
	ToolKindExecute    = "execute"
	ToolKindThink      = "think"
	ToolKindFetch      = "fetch"
	ToolKindSwitchMode = "switch_mode"
	ToolKindOther      = "other"
)

// Tool call statuses.
const (
	ToolStatusPending    = "pending"
	ToolStatusInProgress = "in_progress"
	ToolStatusCompleted  = "completed"
	ToolStatusFailed     = "failed"
)

// ToolCall is a tool_call update: a new tool call.
type ToolCall struct {
	SessionUpdate string            `json:"sessionUpdate"`
	ToolCallID    string            `json:"toolCallId"`
	Title         string            `json:"title"`
	Name          string            `json:"name,omitempty"`
	Kind          string            `json:"kind,omitempty"`
	Status        string            `json:"status,omitempty"`
	Content       []ToolCallContent `json:"content,omitempty"`
	Locations     []Location        `json:"locations,omitempty"`
	RawInput      any               `json:"rawInput,omitempty"`
	RawOutput     any               `json:"rawOutput,omitempty"`
}

// ToolCallUpdate changes a tool call. As a session update its
// SessionUpdate is "tool_call_update"; inside session/request_permission
// it is left empty and omitted.
type ToolCallUpdate struct {
	SessionUpdate string            `json:"sessionUpdate,omitempty"`
	ToolCallID    string            `json:"toolCallId"`
	Title         string            `json:"title,omitempty"`
	Name          string            `json:"name,omitempty"`
	Kind          string            `json:"kind,omitempty"`
	Status        string            `json:"status,omitempty"`
	Content       []ToolCallContent `json:"content,omitempty"`
	Locations     []Location        `json:"locations,omitempty"`
	RawInput      any               `json:"rawInput,omitempty"`
	RawOutput     any               `json:"rawOutput,omitempty"`
}

// ToolCallContent is content a tool call produced; celeste sends the
// "content" type (a content block).
type ToolCallContent struct {
	Type    string        `json:"type"`
	Content *ContentBlock `json:"content,omitempty"`
}

// TextToolContent is a tool call's text content.
func TextToolContent(text string) ToolCallContent {
	b := TextBlock(text)
	return ToolCallContent{Type: "content", Content: &b}
}

// Location is a file a tool call touches.
type Location struct {
	Path string `json:"path"`
	Line *int   `json:"line,omitempty"`
}

// Plan is a plan update; it replaces the whole plan.
type Plan struct {
	SessionUpdate string      `json:"sessionUpdate"`
	Entries       []PlanEntry `json:"entries"`
}

// NewPlan is a plan update with entries ([] when there are none).
func NewPlan(entries []PlanEntry) Plan {
	if entries == nil {
		entries = []PlanEntry{}
	}
	return Plan{SessionUpdate: UpdatePlan, Entries: entries}
}

// PlanEntry is one plan entry. Priority is high, medium or low; Status is
// pending, in_progress or completed.
type PlanEntry struct {
	Content  string `json:"content"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
}

// Permission option kinds.
const (
	OptionAllowOnce    = "allow_once"
	OptionAllowAlways  = "allow_always"
	OptionRejectOnce   = "reject_once"
	OptionRejectAlways = "reject_always"
)

// PermissionOption is one choice offered in a permission prompt.
type PermissionOption struct {
	OptionID string `json:"optionId"`
	Name     string `json:"name"`
	Kind     string `json:"kind"`
}

// RequestPermissionParams is the agent's session/request_permission.
type RequestPermissionParams struct {
	SessionID string             `json:"sessionId"`
	ToolCall  ToolCallUpdate     `json:"toolCall"`
	Options   []PermissionOption `json:"options"`
}

// RequestPermissionResult is the client's answer.
type RequestPermissionResult struct {
	Outcome PermissionOutcome `json:"outcome"`
}

// Permission outcomes.
const (
	OutcomeSelected  = "selected"
	OutcomeCancelled = "cancelled"
)

// PermissionOutcome is {outcome: "selected", optionId} or
// {outcome: "cancelled"}.
type PermissionOutcome struct {
	Outcome  string `json:"outcome"`
	OptionID string `json:"optionId,omitempty"`
}
