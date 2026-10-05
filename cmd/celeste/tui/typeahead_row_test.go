package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// typeaheadRow is the frame's typeahead row: the row after the input's
// last row that lists suggestions.
func typeaheadRow(t *testing.T, frame, first string) string {
	t.Helper()
	for _, r := range strings.Split(frame, "\n") {
		if strings.Contains(r, first) && strings.Contains(r, "·") {
			return r
		}
	}
	t.Fatalf("no typeahead row with %q in\n%s", first, frame)
	return ""
}

// The typeahead row is never cut mid-name without a mark: suggestions that
// do not fit end in "…", and the highlighted one stays on screen as Tab
// moves past the edge, at both audit sizes.
func TestTypeaheadRowMarksTheCut(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			for _, r := range "/session " {
				m, _ = step(t, m, runeKeyOrSpace(r))
			}
			m, _ = step(t, m, inputRedrawMsg{})
			require.True(t, m.input.HasSuggestions())
			subs := m.input.suggestions
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			row := strings.TrimRight(typeaheadRow(t, frame, "/session "), " ")
			full := "  /" + strings.Join(subs, " · /")
			if len(full) > sz.w {
				assert.True(t, strings.HasSuffix(row, "…"), "cut without a mark: %q", row)
			} else {
				assert.Equal(t, full, row)
			}
			for _, s := range strings.Split(strings.Trim(row, " …"), " · ") {
				if s == "" || s == "…" {
					continue
				}
				assert.Contains(t, subs, strings.TrimPrefix(s, "/"), "a name was cut: %q in %q", s, row)
			}

			// Tab to the last suggestion: it is shown.
			last := subs[len(subs)-1]
			for range len(subs) - 1 {
				m.input.suggestionIdx++
			}
			m, _ = step(t, m, inputRedrawMsg{})
			frame = auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			assert.Contains(t, frame, "/"+last, frame)
		})
	}
}

func TestSuggestionRowWindow(t *testing.T) {
	subs := []string{"session new", "session resume", "session list", "session clear", "session merge", "session info", "session rename", "session delete"}
	for active := range subs {
		row := stripANSI(suggestionRow(subs, active, 80))
		assert.LessOrEqual(t, visibleWidth(row), 80, row)
		assert.Contains(t, row, "/"+subs[active], row)
	}
	assert.Equal(t, "  … · /session info · /session rename · /session delete", stripANSI(suggestionRow(subs, 7, 60)))
	assert.Equal(t, "  /a · /b", stripANSI(suggestionRow([]string{"a", "b"}, 0, 0)))
}
