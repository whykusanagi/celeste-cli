package server

import (
	"context"
	"math"
	"sync"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/costs"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
)

// sessionCost adds up the tokens and priced cost of every LLM request this
// server process made for MCP calls (celeste chat and agent runs,
// celeste_content), for celeste_status (#210). The session is the process:
// it starts at zero when the client starts `celeste serve`.
//
// A request on a model missing from costs.ModelPricing (fugu, a local model)
// adds its tokens but no cost, and counts in unpriced_requests, so a zero
// cost is never read as a free session.
type sessionCost struct {
	mu       sync.Mutex
	input    int
	output   int
	usd      float64
	requests int
	unpriced int
}

// record adds one request's usage. A nil usage (the provider sent none) is
// not counted.
func (c *sessionCost) record(model string, u *llm.TokenUsage) {
	if u == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests++
	c.input += u.PromptTokens
	c.output += u.CompletionTokens
	if _, ok := costs.ModelPricing[model]; ok {
		c.usd += costs.GetCost(model, u.PromptTokens, u.CompletionTokens)
	} else {
		c.unpriced++
	}
}

// snapshot is the celeste_status "session_cost" object. The cost is rounded
// to a millionth of a dollar so float noise never reaches the client.
func (c *sessionCost) snapshot() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"total_cost_usd":    math.Round(c.usd*1e6) / 1e6,
		"input_tokens":      c.input,
		"output_tokens":     c.output,
		"requests":          c.requests,
		"unpriced_requests": c.unpriced,
	}
}

type costKey struct{}

// withCost carries the server's tally into an agent run, which executes
// through agentExecFn and has no *Server.
func withCost(ctx context.Context, c *sessionCost) context.Context {
	return context.WithValue(ctx, costKey{}, c)
}

func costFrom(ctx context.Context) *sessionCost {
	c, _ := ctx.Value(costKey{}).(*sessionCost)
	return c
}
