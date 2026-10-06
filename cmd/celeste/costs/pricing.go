// Package costs provides token cost tracking and pricing for LLM models.
package costs

import (
	"regexp"
	"strings"
)

// ModelCost holds the per-1M-token pricing for a model.
type ModelCost struct {
	Input  float64 // USD per 1M input tokens
	Output float64 // USD per 1M output tokens
	// CacheRead, CacheWrite (5-minute entries) and CacheWrite1h price the
	// prompt tokens read from or written to the prompt cache (#312). 0: the
	// rate is not modeled, and those tokens are priced at Input.
	CacheRead    float64
	CacheWrite   float64
	CacheWrite1h float64
}

// Usage is one request's tokens. Input is the whole prompt, cached tokens
// included (as llm.TokenUsage.PromptTokens counts it); CacheRead and
// CacheWrite are the parts of it read from and written to the cache, and
// CacheWrite1h the part of CacheWrite written with a 1-hour lifetime.
type Usage struct {
	Input, Output                       int
	CacheRead, CacheWrite, CacheWrite1h int
}

// ModelPricing maps model identifiers to their costs.
var ModelPricing = map[string]ModelCost{
	// OpenAI — current generation (from pricing page, 2026-04). Cached
	// input: a tenth of input on the gpt-5 family, a quarter on gpt-4.1 and
	// the o-series; pro models get no cache discount.
	"gpt-4.1":       {Input: 2.50, Output: 15.00, CacheRead: 0.625},
	"gpt-4.1-mini":  {Input: 0.75, Output: 4.50, CacheRead: 0.1875},
	"gpt-4.1-nano":  {Input: 0.20, Output: 1.25, CacheRead: 0.05},
	"gpt-5.3-codex": {Input: 2.50, Output: 15.00, CacheRead: 0.25},
	"gpt-5.4":       {Input: 2.50, Output: 15.00, CacheRead: 0.25},
	"gpt-5.4-mini":  {Input: 0.75, Output: 4.50, CacheRead: 0.075},
	"gpt-5.4-nano":  {Input: 0.20, Output: 1.25, CacheRead: 0.02},
	"gpt-5.4-pro":   {Input: 15.00, Output: 60.00},
	"o3":            {Input: 2.50, Output: 15.00, CacheRead: 0.625},
	"o4-mini":       {Input: 0.75, Output: 4.50, CacheRead: 0.1875},
	// xAI Grok — current generation (from pricing page, 2026-04)
	"grok-build-0.1":              {Input: 1.00, Output: 2.00, CacheRead: 0.20}, // grok code model
	"grok-4-1-fast":               {Input: 0.20, Output: 0.50, CacheRead: 0.05},
	"grok-4-1-fast-reasoning":     {Input: 0.20, Output: 0.50, CacheRead: 0.05},
	"grok-4-1-fast-non-reasoning": {Input: 0.20, Output: 0.50, CacheRead: 0.05},
	// grok-4.x family: $1.25 in / $2.50 out per 1M (docs.x.ai, 2026-06)
	"grok-4.3":                     {Input: 1.25, Output: 2.50},
	"grok-4.20-0309-reasoning":     {Input: 1.25, Output: 2.50},
	"grok-4.20-0309-non-reasoning": {Input: 1.25, Output: 2.50},
	"grok-4.20-multi-agent-0309":   {Input: 1.25, Output: 2.50},
	"grok-code-fast-1":             {Input: 0.20, Output: 0.50},
	// Google
	"gemini-2.0-flash": {Input: 0.10, Output: 0.40, CacheRead: 0.025},
	// Anthropic (current models, 2026-04)
	// Cache writes are 1.25x input (5 minutes) and 2x (1 hour); cache reads
	// 0.1x, except where the model lists its own read rate (Opus 5.5 $0.20,
	// Fable 5.1 $0.25; models table, 2026-09).
	"claude-fable-5-1":  {Input: 10.00, Output: 50.00, CacheRead: 0.25, CacheWrite: 12.50, CacheWrite1h: 20.00},
	"claude-opus-5-5":   {Input: 4.00, Output: 20.00, CacheRead: 0.20, CacheWrite: 5.00, CacheWrite1h: 8.00},
	"claude-opus-5":     {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25, CacheWrite1h: 10.00},
	"claude-opus-4-8":   {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25, CacheWrite1h: 10.00},
	"claude-opus-4-7":   {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25, CacheWrite1h: 10.00},
	"claude-opus-4-6":   {Input: 5.00, Output: 25.00, CacheRead: 0.50, CacheWrite: 6.25, CacheWrite1h: 10.00},
	"claude-sonnet-5-5": {Input: 2.00, Output: 10.00, CacheRead: 0.20, CacheWrite: 2.50, CacheWrite1h: 4.00},
	"claude-sonnet-5":   {Input: 2.00, Output: 10.00, CacheRead: 0.20, CacheWrite: 2.50, CacheWrite1h: 4.00},
	"claude-sonnet-4-6": {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75, CacheWrite1h: 6.00},
	"claude-sonnet-4-5": {Input: 3.00, Output: 15.00, CacheRead: 0.30, CacheWrite: 3.75, CacheWrite1h: 6.00},
	"claude-haiku-4-5":  {Input: 1.00, Output: 5.00, CacheRead: 0.10, CacheWrite: 1.25, CacheWrite1h: 2.00},
	// Venice-unique models (from docs.venice.ai, 2026-04)
	"venice-uncensored":                    {Input: 0.20, Output: 0.90},
	"venice-uncensored-role-play":          {Input: 0.50, Output: 2.00},
	"deepseek-v3.2":                        {Input: 0.33, Output: 0.48},
	"qwen3-coder-480b-a35b-instruct":       {Input: 0.75, Output: 3.00},
	"qwen3-coder-480b-a35b-instruct-turbo": {Input: 0.35, Output: 1.50},
	"qwen3-235b-a22b-thinking-2507":        {Input: 0.45, Output: 3.50},
	"kimi-k2-5":                            {Input: 0.56, Output: 3.50},
	"zai-org-glm-4.7":                      {Input: 0.55, Output: 2.65},
	"mistral-small-3-2-24b-instruct":       {Input: 0.09, Output: 0.25},
	"llama-3.3-70b":                        {Input: 0.70, Output: 2.80},
	"minimax-m25":                          {Input: 0.34, Output: 1.19},
}

// datedSuffix is a snapshot date on a model ID: "-20250929" or "@20250929".
var datedSuffix = regexp.MustCompile(`[-@]\d{8}$`)

// pricing finds model's row: the ID as given, else lowercased without a
// provider prefix ("anthropic.", Bedrock) and a snapshot date (the
// Anthropic default claude-sonnet-4-5-20250929, Vertex's @date), so a
// dated ID prices as the model it names.
func pricing(model string) (ModelCost, bool) {
	if mc, ok := ModelPricing[model]; ok {
		return mc, true
	}
	id := strings.ToLower(model)
	id = strings.TrimPrefix(id, "anthropic.")
	id = datedSuffix.ReplaceAllString(id, "")
	mc, ok := ModelPricing[id]
	return mc, ok
}

// Priced reports whether model is in the pricing table.
func Priced(model string) bool {
	_, ok := pricing(model)
	return ok
}

// CostOf is the USD cost of one request's usage on model, cache reads and
// writes at their own rates (#312). Returns 0 if the model is not in the
// pricing table.
func CostOf(model string, u Usage) float64 {
	mc, ok := pricing(model)
	if !ok {
		return 0
	}
	rate := func(r, fallback float64) float64 {
		if r > 0 {
			return r
		}
		return fallback
	}
	write1h := min(max(u.CacheWrite1h, 0), max(u.CacheWrite, 0))
	write5m := max(u.CacheWrite, 0) - write1h
	read := max(u.CacheRead, 0)
	uncached := max(u.Input-read-write5m-write1h, 0)
	perM := func(n int, r float64) float64 { return float64(n) / 1_000_000.0 * r }
	return perM(uncached, mc.Input) +
		perM(read, rate(mc.CacheRead, mc.Input)) +
		perM(write5m, rate(mc.CacheWrite, mc.Input)) +
		perM(write1h, rate(mc.CacheWrite1h, rate(mc.CacheWrite, mc.Input))) +
		perM(u.Output, mc.Output)
}

// GetCost is CostOf for usage with no cached tokens.
func GetCost(model string, inputTokens, outputTokens int) float64 {
	return CostOf(model, Usage{Input: inputTokens, Output: outputTokens})
}
