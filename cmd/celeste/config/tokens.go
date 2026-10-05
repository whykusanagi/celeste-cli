// Package config provides configuration management for Celeste CLI.
// This file provides thin wrappers around ctxmgr for token estimation
// and model limit queries. Session-specific helpers that depend on
// config types (SessionMessage, Session) remain here.
package config

import (
	"fmt"
	"sync"

	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
)

// ModelLimits is kept as an alias for backward compatibility.
// Canonical data lives in ctxmgr.ModelLimits.
var ModelLimits = ctxmgr.ModelLimits

// EstimateTokens approximates token count (delegates to ctxmgr).
func EstimateTokens(text string) int {
	return ctxmgr.EstimateTokens(text)
}

// EstimateMessageTokens counts tokens in a message.
func EstimateMessageTokens(msg SessionMessage) int {
	// Role overhead: ~4 tokens + content
	return 4 + ctxmgr.EstimateTokens(msg.Content)
}

// EstimateSessionTokens counts total tokens in session.
func EstimateSessionTokens(session *Session) int {
	total := 0
	for _, msg := range session.Messages {
		total += EstimateMessageTokens(msg)
	}
	return total
}

// EstimateSessionTokensByRole calculates separate input/output token counts.
// Returns (promptTokens, completionTokens, totalTokens).
func EstimateSessionTokensByRole(session *Session) (int, int, int) {
	promptTokens := 0
	completionTokens := 0

	for _, msg := range session.Messages {
		msgTokens := EstimateMessageTokens(msg)
		switch msg.Role {
		case "user", "system":
			promptTokens += msgTokens
		case "assistant":
			completionTokens += msgTokens
		}
	}

	return promptTokens, completionTokens, promptTokens + completionTokens
}

// LookupModelLimit reports the limit and whether the model is known
// (delegates to ctxmgr). Callers validating a user-configured window need the
// second value: for an unknown model the limit is a fallback, not knowledge.
func LookupModelLimit(model string) (int, bool) {
	return ctxmgr.LookupModelLimit(model)
}

// FormatTokenCount formats token count with K/M suffix (delegates to ctxmgr).
func FormatTokenCount(tokens int) string {
	return ctxmgr.FormatTokenCount(tokens)
}

// ResolveContextLimit returns the effective context window and whether that
// number is actually knowledge.
//
// An explicit override always wins. A local endpoint whose server reports
// its window (the probe EnableLocalWindowProbe installs) gets that.
// Otherwise local endpoints get ctxmgr.LocalDefaultLimit
// even when the model name is in the table: a local server names its model
// whatever it likes, so a hit is coincidence. That mattered in practice — a
// fresh profile inherits the seed default's model (fugu), so pointing it at a
// local server produced a confident 1,000,000-token budget for a server that
// might have 8k. Unknown hosted models get the table's 128k default (#201).
func ResolveContextLimit(baseURL, model string, override int, apiKey string) (limit int, known bool) {
	limit, src := ResolveContextLimitSource(baseURL, model, override, apiKey)
	return limit, src != SourceFallback
}

// Where ResolveContextLimitSource's window came from.
const (
	SourceConfigured = "configured"
	SourceReported   = "reported by the server"
	SourceModel      = "model default"
	SourceFallback   = "fallback"
)

// ResolveContextLimitSource is ResolveContextLimit with where the number
// came from (`celeste config` shows it). apiKey is the endpoint's key: a
// local server started with one answers the probe only with it.
func ResolveContextLimitSource(baseURL, model string, override int, apiKey string) (int, string) {
	if override > 0 {
		return override, SourceConfigured
	}
	// A server on this machine or the local network is asked for its own
	// window (#310), decided on the parsed host so a hosted URL is never
	// probed.
	if providers.IsLocalHost(baseURL) {
		if n := localWindow(baseURL, apiKey, model); n > 0 {
			return n, SourceReported
		}
	}
	if providers.DetectProvider(baseURL) == "local" {
		return ctxmgr.LocalDefaultLimit, SourceFallback
	}
	n, known := LookupModelLimit(model)
	if known {
		return n, SourceModel
	}
	return n, SourceFallback
}

var unknownContextWarned sync.Map

// UnknownContextNotice returns the one-time warning for a model whose window
// was guessed, and "" on every later call for the same model (#201).
func UnknownContextNotice(model string, limit int) string {
	if _, seen := unknownContextWarned.LoadOrStore(model, true); seen {
		return ""
	}
	// Not logged here: the standard logger writes to the terminal, under
	// the TUI's alternate screen, where it left fragments of this line
	// (#319). Callers show it: the TUI in the chat, the agent on stderr.
	return fmt.Sprintf("Unknown model %q: assuming a %s context window. Set \"context_limit\" in your config if that's wrong.", model, FormatTokenCount(limit))
}
