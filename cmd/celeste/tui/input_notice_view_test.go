package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// C1: the paste-limit notice wraps to the terminal width instead of being
// cut, so its advice stays readable at 80 columns.
func TestAppPasteNoticeWraps(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			humanPace(t)
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			m, _ = step(t, m, bracketed(pasteText(inputCharLimit+1)))
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			flat := strings.Join(strings.Fields(frame), " ")
			assert.Contains(t, flat, "Paste not inserted: it is over the 262,144-character input limit. Save long text to a file and point to it instead.", frame)
		})
	}
}
