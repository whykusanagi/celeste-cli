package acp

import (
	"context"
	"errors"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/hooks"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/termsafe"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// compactTrigger is the PreCompact/PostCompact trigger of a compaction the
// loop starts (the agent runtime's too).
const compactTrigger = "auto"

var errCompactionBlocked = errors.New("compaction blocked by a PreCompact hook")

// compactor is a session's loop.Compactor (ruling 12): the agent runtime's
// ladder built from the same pieces. Before every request it prunes old
// tool results (compact.Prune, kept for recall_tool_result) once the
// history passes the window's threshold, and summarizes the older history
// with the small model when pruning was not enough. It runs on the loop's
// goroutine; one prompt runs at a time, so its budget is never shared.
type compactor struct {
	budget    *ctxmgr.TokenBudget
	store     *compact.Store        // nil: no pruning
	summarize compact.SummarizeFunc // nil: no summary rung
	hooks     *hooks.Runner         // PreCompact/PostCompact; nil: none
	logf      func(string, ...any)  // never nil
	// summaryTimeout bounds one summary: the request cap of the session's
	// client config, which the summarizer's client is built from (#345).
	summaryTimeout time.Duration
}

// newCompactor builds a session's compactor: the window from the config
// (context_limit, else the model's known window), the system prompt as the
// fixed overhead, pruned results in store, summaries from summarize.
func newCompactor(cfg *config.Config, systemPrompt string, store *compact.Store, summarize compact.SummarizeFunc, h *hooks.Runner, logf func(string, ...any)) *compactor {
	limit, known := config.ResolveContextLimit(cfg.BaseURL, cfg.Model, cfg.ContextLimit, cfg.APIKey)
	if !known {
		logf("acp: unknown context window for model %q: assuming %d tokens (set context_limit if that is wrong)", cfg.Model, limit)
	}
	return &compactor{
		budget:    ctxmgr.NewTokenBudget(limit, ctxmgr.EstimateTokens(systemPrompt), 0),
		store:     store,
		summarize: summarize,
		hooks:     h,
		logf:      logf,
		// session.go builds the summarizer from llm.ConfigFrom(cfg).
		summaryTimeout: llm.ConfigFrom(cfg).RequestCap(),
	}
}

// Compact implements loop.Compactor.
func (c *compactor) Compact(ctx context.Context, history []tui.ChatMessage, usage *llm.TokenUsage, force bool) ([]tui.ChatMessage, []string, bool) {
	if usage != nil {
		c.budget.AddTurn(usage.PromptTokens, usage.CompletionTokens)
	}
	msgs := history
	overhead := c.budget.SystemPromptTokens + c.budget.ToolDefinitionTokens
	used := compact.Estimate(msgs) + overhead
	if last := c.budget.LastPromptTokens; last > used {
		used = last // the provider's count includes tool schemas the estimate misses
	}
	overhead = used - compact.Estimate(msgs) // the prefix as the provider counts it
	var notes []string
	changed := false
	if c.store != nil {
		pruned, res := compact.Prune(msgs, compact.Options{Window: c.budget.ModelLimit, Used: used, Overhead: overhead, Force: force}, c.store)
		if res.Pruned() {
			msgs, changed = pruned, true
			c.budget.RecordCompaction(compact.Estimate(msgs))
			notes = append(notes, "context compacted: "+res.Summary())
		}
	}
	// Over the overhead-aware threshold with history to replace (L2).
	stillOver := compact.NeedsSummary(msgs, c.budget.ModelLimit, compact.Estimate(msgs)+overhead)
	if c.summarize == nil || !(stillOver || (force && !changed)) {
		return msgs, notes, changed
	}
	summarize, blocked := c.hookedSummarize()
	sctx, cancel := context.WithTimeout(ctx, c.summaryTimeout)
	out, sres, err := compact.Summarize(sctx, msgs, compact.SummaryOptions{Window: c.budget.ModelLimit, Overhead: overhead}, summarize)
	cancel()
	if reason := blocked(); reason != "" {
		notes = append(notes, "compaction blocked by a PreCompact hook: "+termsafe.Line(reason)) // hook output
		return msgs, notes, changed
	}
	if err != nil {
		if !errors.Is(err, compact.ErrNothingToSummarize) {
			notes = append(notes, "context summary failed: "+err.Error())
		}
		return msgs, notes, changed
	}
	c.budget.RecordCompaction(sres.TokensAfter)
	notes = append(notes, "context compacted: "+sres.Line())
	if c.hooks != nil {
		c.hooks.PostCompact(ctx, compactTrigger, sres.Summary)
	}
	return out, notes, true
}

// hookedSummarize wraps the summarizer with PreCompact, fired once, before
// the first summary request: a deny blocks the summary (its reason is
// returned by the second func), additional context joins the request.
func (c *compactor) hookedSummarize() (compact.SummarizeFunc, func() string) {
	h := c.hooks
	if h == nil || !h.Has(hooks.EventPreCompact) {
		return c.summarize, func() string { return "" }
	}
	blocked, fired := "", false
	return func(ctx context.Context, system, user string) (string, error) {
		if blocked != "" {
			return "", errCompactionBlocked
		}
		if !fired {
			fired = true
			pre := h.PreCompact(ctx, compactTrigger, "")
			if pre.Decision != hooks.Allow {
				blocked = pre.Reason
				if blocked == "" {
					blocked = "no reason given"
				}
				return "", errCompactionBlocked
			}
			if pre.AdditionalContext != "" {
				user += "\n\nAdditional instructions from a PreCompact hook:\n" + pre.AdditionalContext
			}
		}
		return c.summarize(ctx, system, user)
	}, func() string { return blocked }
}
