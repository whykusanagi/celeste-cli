package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/param"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// anthropicDefaultBaseURL is the SDK's endpoint when the config has none.
const anthropicDefaultBaseURL = "https://api.anthropic.com"

// anthropicSDKBaseURL is a configured base URL in the form the SDK wants:
// the API root without /v1, since the SDK appends v1/messages itself.
// celeste advertised https://api.anthropic.com/v1 before 2.0, so a trailing
// /v1 (and any trailing slash) is dropped; both forms reach /v1/messages.
func anthropicSDKBaseURL(baseURL string) string {
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	return strings.TrimSuffix(base, "/v1")
}

// replayedContent turns stored blocks into request content blocks that
// marshal to exactly the stored bytes (2.0 W2 ruling 3). A replayed block
// has no cache_control and is never edited.
func replayedContent(raws []json.RawMessage) []anthropic.ContentBlockParamUnion {
	out := make([]anthropic.ContentBlockParamUnion, len(raws))
	for i, raw := range raws {
		out[i] = param.Override[anthropic.ContentBlockParamUnion](raw)
	}
	return out
}

// keptBlockTypes make a reply worth keeping verbatim (ruling 2): its
// thinking must be replayed, or (W1) its compaction block must be.
var keptBlockTypes = map[string]bool{"thinking": true, "redacted_thinking": true, "compaction": true}

// inputTransformation is one entry of a response's input_transformations
// (thinking-binding-controls beta).
type inputTransformation struct {
	Type   string `json:"type"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// anthropicCapture rebuilds a reply's content blocks from its stream with
// the SDK's accumulator, which keeps each block's wire JSON with the
// streamed deltas applied (ruling 1), and reads input_transformations
// from message_start.
type anthropicCapture struct {
	msg             anthropic.Message
	broken          bool
	transformations []inputTransformation
}

func (c *anthropicCapture) add(ev anthropic.MessageStreamEventUnion) {
	if ev.Type == "message_start" {
		var m struct {
			InputTransformations []inputTransformation `json:"input_transformations"`
		}
		if json.Unmarshal([]byte(ev.Message.RawJSON()), &m) == nil {
			c.transformations = m.InputTransformations
		}
	}
	if c.broken {
		return
	}
	if err := c.msg.Accumulate(ev); err != nil {
		c.broken = true
		tui.LogInfo("anthropic: reply blocks not kept: " + err.Error())
	}
}

// blocks returns the reply's content blocks, in order, under key when one
// of them is thinking, redacted_thinking or compaction; nil otherwise, or
// when any block could not be reproduced.
func (c *anthropicCapture) blocks(key string) *tui.ProviderBlocks {
	if c.broken {
		return nil
	}
	keep := false
	raws := make([]json.RawMessage, 0, len(c.msg.Content))
	for _, cb := range c.msg.Content {
		raw := cb.RawJSON()
		if raw == "" {
			return nil
		}
		keep = keep || keptBlockTypes[cb.Type]
		raws = append(raws, json.RawMessage(raw))
	}
	if !keep {
		return nil
	}
	pb, err := tui.NewProviderBlocks(key, raws)
	if err != nil {
		tui.LogInfo("anthropic: reply blocks not kept: " + err.Error())
		return nil
	}
	return pb
}

// prefixDropped logs every input transformation and reports whether the
// API dropped thinking because the history before it changed (ruling 7).
// Call it once per reply.
func (c *anthropicCapture) prefixDropped() bool {
	dropped := false
	for _, t := range c.transformations {
		tui.LogInfo(fmt.Sprintf("anthropic: input transformation %s at %s (%s)", t.Type, t.Path, t.Reason))
		if t.Type == "thinking_dropped" && t.Reason == "prefix_binding_mismatch" {
			dropped = true
		}
	}
	return dropped
}

// thinkingBindingBeta lets a request set thinking.block_binding and adds
// input_transformations to responses (2.0 W2 ruling 6).
const thinkingBindingBeta = "thinking-binding-controls-2026-08-01"

// anthropicBadRequest returns the lower-cased body of a 400 from the API.
func anthropicBadRequest(err error) (string, bool) {
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest {
		return strings.ToLower(apiErr.RawJSON()), true
	}
	return "", false
}

// isThinkingRejection reports a 400 about replayed thinking: the
// prefix-mismatch rejection ("Invalid `signature` in `thinking` block …
// bound to a different conversation"), a tampered signature, or thinking
// blocks the request may not carry (ruling 8). body is lower-cased. A
// signature complaint counts only beside "thinking": a tool parameter
// named signature is not one.
func isThinkingRejection(body string) bool {
	if strings.Contains(body, "signature") && strings.Contains(body, "thinking") {
		return true
	}
	for _, s := range []string{"thinking block", "`thinking` block", "redacted_thinking"} {
		if strings.Contains(body, s) {
			return true
		}
	}
	return false
}

// isBindingBetaRejection reports a 400 refusing the binding controls: the
// field, the header or the beta name (ruling 9). body is lower-cased;
// callers test isThinkingRejection first.
func isBindingBetaRejection(body string) bool {
	return strings.Contains(body, "block_binding") || strings.Contains(body, "anthropic-beta") || strings.Contains(body, thinkingBindingBeta)
}

// hasProviderBlocks reports whether any message carries blocks.
func hasProviderBlocks(messages []tui.ChatMessage) bool {
	for _, m := range messages {
		if m.ProviderBlocks != nil {
			return true
		}
	}
	return false
}
