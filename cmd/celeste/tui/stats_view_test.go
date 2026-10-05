package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// C6: /stats' closing rule starts at the message's indent and fits on one
// row, at both audit sizes.
func TestStatsFooterRuleFits(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			m = auditSend(t, m, "/stats")
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			rules := 0
			for _, row := range strings.Split(frame, "\n") {
				if strings.Contains(row, "═══") {
					rules++
					assert.True(t, strings.HasPrefix(row, " ▓▒░ ═"), "rule pushed right: %q", row)
					assert.True(t, strings.HasSuffix(strings.TrimRight(row, " "), "░▒▓"), "rule wrapped: %q", row)
				}
			}
			require.Positive(t, rules, frame)
		})
	}
}
