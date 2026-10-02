package llm

import (
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

// buildParams is the params build makes, for tests of what goes into a
// request body (cache breakpoints, thinking config). Production requests go
// through prepare, which may add the binding beta and drop_block as request
// options; tests of replayed history use prepared or preparedParams.
func (b *AnthropicBackend) buildParams(messages []tui.ChatMessage, tools []tui.SkillDefinition) anthropic.MessageNewParams {
	return b.build(messages, tools).params
}

// preparedParams is the request a send would make: prepare's params and its
// per-request options.
func (b *AnthropicBackend) preparedParams(messages []tui.ChatMessage, tools []tui.SkillDefinition) (anthropic.MessageNewParams, []option.RequestOption) {
	req, opts := b.prepare(messages, tools)
	return req.params, opts
}

// prepared is preparedParams' params for messages with no tools.
func prepared(t *testing.T, b *AnthropicBackend, messages []tui.ChatMessage) anthropic.MessageNewParams {
	t.Helper()
	params, _ := b.preparedParams(messages, nil)
	return params
}
