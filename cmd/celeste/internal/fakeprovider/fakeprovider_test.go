package fakeprovider

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
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
		t.Fatalf("turn 1 = %+v, want one tool call", res)
	}
	if res.ProviderBlocks == nil || len(res.ProviderBlocks.Blocks) != 2 {
		t.Fatalf("turn 1 kept %+v, want its thinking and tool_use blocks (#192)", res.ProviderBlocks)
	}
	res, err = c.SendMessageSync(context.Background(), msgs, nil)
	if err != nil || res.Content != "done" {
		t.Fatalf("turn 2 = %+v, %v", res, err)
	}
	if srv.Requests()[0].Path != "/v1/messages" {
		t.Fatalf("path = %q", srv.Requests()[0].Path)
	}
}

func TestHoldWhileParksRequestsWithoutConsumingTurns(t *testing.T) {
	srv := NewOpenAI(t, Turn{Text: "after"})
	var mu sync.Mutex
	hold := true
	srv.HoldWhile(func() bool { mu.Lock(); defer mu.Unlock(); return hold })
	c := client(srv.BaseURL(), "")
	msgs := []tui.ChatMessage{{Role: "user", Content: "x"}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := c.SendMessageSync(ctx, msgs, nil); err == nil {
		t.Fatal("a held request answered")
	}
	if srv.Remaining() != 1 || len(srv.Requests()) != 1 {
		t.Fatalf("remaining = %d, requests = %d; want 1, 1", srv.Remaining(), len(srv.Requests()))
	}
	mu.Lock()
	hold = false
	mu.Unlock()
	res, err := c.SendMessageSync(context.Background(), msgs, nil)
	if err != nil || res.Content != "after" {
		t.Fatalf("after the hold = %+v, %v", res, err)
	}
}
