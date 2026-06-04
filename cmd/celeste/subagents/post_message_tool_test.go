package subagents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

func TestPostMessageToolName(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewPostMessageTool(m, "parent")
	if tool.Name() != "post_message" {
		t.Fatalf("expected name 'post_message', got %q", tool.Name())
	}
}

func TestPostMessageToolValidateInput(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewPostMessageTool(m, "parent")

	// Missing to
	if err := tool.ValidateInput(map[string]any{"message": "hi"}); err == nil {
		t.Fatal("expected error for missing 'to'")
	}

	// Missing message
	if err := tool.ValidateInput(map[string]any{"to": "fire"}); err == nil {
		t.Fatal("expected error for missing 'message'")
	}

	// Empty to
	if err := tool.ValidateInput(map[string]any{"to": "", "message": "hi"}); err == nil {
		t.Fatal("expected error for empty 'to'")
	}

	// Valid
	if err := tool.ValidateInput(map[string]any{"to": "fire", "message": "hello"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPostMessageToolParameters(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewPostMessageTool(m, "parent")
	params := tool.Parameters()
	var schema map[string]any
	if err := json.Unmarshal(params, &schema); err != nil {
		t.Fatalf("invalid JSON schema: %v", err)
	}
	props := schema["properties"].(map[string]any)
	if _, ok := props["to"]; !ok {
		t.Fatal("schema missing 'to' property")
	}
	if _, ok := props["message"]; !ok {
		t.Fatal("schema missing 'message' property")
	}
}

func TestPostMessageTool_Execute_PostsToMailbox(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewPostMessageTool(m, "water")

	result, err := tool.Execute(context.Background(), map[string]any{
		"to":      "fire",
		"message": "found the config",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	if result.Content != "message queued for fire" {
		t.Fatalf("unexpected content: %q", result.Content)
	}

	// Verify the message landed in the mailbox
	msgs := m.mailbox.Drain("fire")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message in mailbox, got %d", len(msgs))
	}
	if msgs[0].From != "water" {
		t.Fatalf("expected From=water, got %q", msgs[0].From)
	}
	if msgs[0].Body != "found the config" {
		t.Fatalf("expected body 'found the config', got %q", msgs[0].Body)
	}
	if msgs[0].To != "fire" {
		t.Fatalf("expected To=fire, got %q", msgs[0].To)
	}
}

func TestPostMessageTool_IsReadOnly(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewPostMessageTool(m, "parent")
	if tool.IsReadOnly() {
		t.Fatal("post_message should not be read-only")
	}
}

func TestPostMessageTool_IsConcurrencySafe(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewPostMessageTool(m, "parent")
	if !tool.IsConcurrencySafe(nil) {
		t.Fatal("post_message should be concurrency safe")
	}
}

func TestPostMessageTool_InterruptBehavior(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	tool := NewPostMessageTool(m, "parent")
	if tool.InterruptBehavior() != tools.InterruptCancel {
		t.Fatal("post_message should have InterruptCancel behavior")
	}
}

func TestPostMessageTool_DefaultFrom(t *testing.T) {
	m := NewManager(&config.Config{}, "/tmp", false)
	// Empty from defaults to "parent"
	tool := NewPostMessageTool(m, "")
	result, err := tool.Execute(context.Background(), map[string]any{
		"to":      "earth",
		"message": "hello",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Error {
		t.Fatalf("unexpected tool error: %s", result.Content)
	}
	msgs := m.mailbox.Drain("earth")
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].From != "parent" {
		t.Fatalf("expected From=parent, got %q", msgs[0].From)
	}
}
