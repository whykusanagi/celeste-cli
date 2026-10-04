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
