package tui

import (
	"strings"
	"testing"
)

// V18: quitting takes two Ctrl+C presses, so the header must not promise one.
func TestHeaderQuitHintMatchesDoublePress(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			frame := auditView(m)
			header := strings.Split(frame, "\n")[0]
			if strings.Contains(header, "Press Ctrl+C to exit") || !strings.Contains(header, "Ctrl+C twice") {
				t.Errorf("header = %q", header)
			}
			assertFrameFits(t, frame, sz.w, sz.h)
		})
	}
}
