package subagents

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// PostMessageTool is a built-in tool that allows a subagent to post a message
// to another subagent's mailbox. The recipient is identified by its element
// name (e.g. "fire", "water"). Messages are delivered at spawn time when the
// recipient agent next starts (spawn-time injection; live mid-run injection is
// a follow-up, see issue #31).
type PostMessageTool struct {
	manager *Manager
	// from is the sender address used when posting messages. Defaults to
	// "parent" when the executing agent's identity is not available in the
	// tool context. A future enhancement may thread the element name through
	// the tool execution context so each subagent self-reports its identity.
	from string
}

// NewPostMessageTool creates a post_message tool backed by the given manager.
// from should be the element name of the agent that holds this tool instance,
// or "parent" when the tool is held by the top-level orchestrator.
func NewPostMessageTool(manager *Manager, from string) *PostMessageTool {
	if from == "" {
		from = "parent"
	}
	return &PostMessageTool{manager: manager, from: from}
}

func (t *PostMessageTool) Name() string { return "post_message" }

func (t *PostMessageTool) Description() string {
	return "Post a message to another subagent's mailbox. The message is delivered " +
		"the next time that agent starts (spawn-time injection). Use the recipient's " +
		"element name as the address (e.g. 'fire', 'water', 'earth', 'light', 'dark', 'wind'). " +
		"Useful for passing coordination data to a later subagent without a hard DAG dependency."
}

func (t *PostMessageTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"to": {
				"type": "string",
				"description": "Recipient element name (e.g. 'fire', 'water'). Must match the target subagent's element address."
			},
			"message": {
				"type": "string",
				"description": "The message body to deliver to the recipient."
			}
		},
		"required": ["to", "message"]
	}`)
}

func (t *PostMessageTool) IsConcurrencySafe(input map[string]any) bool { return true }
func (t *PostMessageTool) IsReadOnly() bool                            { return false }

func (t *PostMessageTool) InterruptBehavior() tools.InterruptBehavior {
	return tools.InterruptCancel
}

func (t *PostMessageTool) ValidateInput(input map[string]any) error {
	to, ok := input["to"].(string)
	if !ok || to == "" {
		return fmt.Errorf("'to' is required and must be a non-empty string")
	}
	msg, ok := input["message"].(string)
	if !ok || msg == "" {
		return fmt.Errorf("'message' is required and must be a non-empty string")
	}
	return nil
}

func (t *PostMessageTool) Execute(_ context.Context, input map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	to := input["to"].(string)
	body := input["message"].(string)

	t.manager.mailbox.Post(to, t.from, body)

	return tools.ToolResult{
		Content: fmt.Sprintf("message queued for %s", to),
		Metadata: map[string]any{
			"to":   to,
			"from": t.from,
		},
	}, nil
}
