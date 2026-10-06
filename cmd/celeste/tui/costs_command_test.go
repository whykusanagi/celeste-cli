package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// V13: before the first turn /costs must not report a "0 limit".
func TestCostsBeforeFirstTurn(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			// No tracker yet: the limit is unknown, and says so.
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			m = auditSend(t, m, "/costs")
			out := lastSystemRender(t, m, sz.w-4)
			assert.NotContains(t, out, "0 limit")
			assert.Contains(t, out, "limit known after the first reply")
			frame := auditView(m)
			assert.NotContains(t, frame, "/ 0 limit")
			assertFrameFits(t, frame, sz.w, sz.h)

			// A tracker knows the window before any reply.
			m.contextTracker = config.NewContextTracker(&config.Session{}, "test-model", 128_000)
			m = auditSend(t, m, "/costs")
			out = lastSystemRender(t, m, sz.w-4)
			assert.Contains(t, out, "0 used / 128000 limit")
			assert.False(t, strings.Contains(out, "/ 0 limit"))
		})
	}
}

// costClient reports a session cost, as the chat adapter does.
type costClient struct {
	fakeToolLLMClient
	cost SessionCost
}

func (c *costClient) SessionCost() SessionCost { return c.cost }

// #312: /costs shows the priced session cost and the cache reads and
// writes it was priced with.
func TestCostsShowsCacheTokensAndCost(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			client := &costClient{cost: SessionCost{Input: 23_444, Output: 300, CacheRead: 23_360, CacheWrite: 72, USD: 0.0118, Requests: 2}}
			m := newAuditApp(t, client, sz.w, sz.h)
			m = auditSend(t, m, "/costs")
			out := lastSystemRender(t, m, sz.w-4)
			assert.Contains(t, out, "Input: 23,444 tokens (cache read 23,360 · cache write 72)")
			assert.Contains(t, out, "Output: 300 tokens")
			assert.Contains(t, out, "Cost: $0.012 (2 requests)")
			assertFrameFits(t, auditView(m), sz.w, sz.h)
		})
	}
}

// A request on a model missing from the pricing table is not free: /costs
// says the total leaves it out.
func TestCostsSaysWhatIsUnpriced(t *testing.T) {
	client := &costClient{cost: SessionCost{Input: 500, Output: 20, Requests: 3, Unpriced: 3}}
	m := newAuditApp(t, client, 120, 40)
	m = auditSend(t, m, "/costs")
	out := lastSystemRender(t, m, 116)
	assert.Contains(t, out, "Input: 500 tokens")
	assert.NotContains(t, out, "cache read")
	assert.Contains(t, out, "3 of 3 requests ran on a model without pricing")
}
