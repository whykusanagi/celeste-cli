package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// C3: a forced model name too long for the header is cut, but its "?"
// stays: the marker is the point of the header's model segment.
func TestHeaderKeepsUnverifiedMarkWhenCut(t *testing.T) {
	long := "some-very-long-experimental-model-name-x"
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			m.header = m.header.SetModel(long).SetModelUnverified(true).SetContextUsage(0, 1_000_000)
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			top := strings.Split(frame, "\n")[0]
			assert.Contains(t, top, "sakana • some-very-long", top)
			assert.Contains(t, top, " ? • ", "the marker survives the cut: %q", top)
			assert.Contains(t, top, "0/1.0M", top)
			if sz.w >= 120 {
				assert.Contains(t, top, long+" ?", top)
			} else {
				assert.Contains(t, top, "… ?", top)
			}

			m.header = m.header.SetModelUnverified(false).SetSkillsEnabled(false)
			top = strings.Split(auditView(m), "\n")[0]
			assert.Contains(t, top, " ⚠ • ", "the no-tools warning survives too: %q", top)
		})
	}
}
