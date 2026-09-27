package fakeprovider

import (
	"context"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func client(url string, backend llm.BackendType) *llm.Client {
	return llm.NewClient(&llm.Config{APIKey: "k", BaseURL: url, Model: "fake-model", Timeout: 10 * time.Second, Backend: backend}, nil)
}

func TestOpenAITextAndParallelToolCalls(t *testing.T) {
	srv := NewOpenAI(t,
		Turn{ToolCalls: []ToolCall{{ID: "call_a", Name: "read_file", Args: `{"path":"a.go"}`}, {ID: "call_b", Name: "read_file", Args: `{"path":"b.go"}`}}},
		Turn{Text: "READ"},
	)
	c := client(srv.BaseURL(), "")
	msgs := []tui.ChatMessage{{Role: "user", Content: "read both"}}

	res, err := c.SendMessageSync(context.Background(), msgs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Content != "" || len(res.ToolCalls) != 2 || res.ToolCalls[1].ID != "call_b" {
		t.Fatalf("turn 1 = %+v, want two text-free tool calls", res)
	}
	res, err = c.SendMessageSync(context.Background(), msgs, nil)
	if err != nil || res.Content != "READ" {
		t.Fatalf("turn 2 = %+v, %v", res, err)
	}
	if got := len(srv.Requests()); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	if srv.Requests()[0].Path != "/v1/chat/completions" {
		t.Fatalf("path = %q", srv.Requests()[0].Path)
	}
}

func TestOpenAIStatusAndExhaustion(t *testing.T) {
	srv := NewOpenAI(t, Turn{Status: 400, Body: `{"error":{"message":"prompt is too long: 9000 tokens > 8192 maximum","type":"invalid_request_error"}}`})
	c := client(srv.BaseURL(), "")
	if _, err := c.SendMessageSync(context.Background(), []tui.ChatMessage{{Role: "user", Content: "x"}}, nil); err == nil {
		t.Fatal("want an error for a scripted 400")
	}
	if _, err := c.SendMessageSync(context.Background(), []tui.ChatMessage{{Role: "user", Content: "x"}}, nil); err == nil {
		t.Fatal("want an error once the script is exhausted")
	}
}

func TestAnthropicTextToolAndThinking(t *testing.T) {
	srv := NewAnthropic(t,
		Turn{Thinking: &Thinking{Text: "plan", Signature: "sig-1"}, ToolCalls: []ToolCall{{ID: "toolu_1", Name: "read_file", Args: `{"path":"a.go"}`}}},
		Turn{Text: "done"},
	)
	c := client(srv.BaseURL(), llm.BackendTypeAnthropic)
	msgs := []tui.ChatMessage{{Role: "user", Content: "read"}}

	res, err := c.SendMessageSync(context.Background(), msgs, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ID != "toolu_1" {
		t.Fatalf("turn 1 = %+v, want one tool call (thinking is dropped today, #192)", res)
	}
	res, err = c.SendMessageSync(context.Background(), msgs, nil)
	if err != nil || res.Content != "done" {
		t.Fatalf("turn 2 = %+v, %v", res, err)
	}
	if srv.Requests()[0].Path != "/v1/messages" {
		t.Fatalf("path = %q", srv.Requests()[0].Path)
	}
}
