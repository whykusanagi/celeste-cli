package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #350: a line wider than the chat keeps its indent, and its continuation
// rows hang under the text column, not at column 0.
func TestWrapTextHangingIndent(t *testing.T) {
	cases := []struct {
		name, in string
		width    int
		want     []string
	}{
		{
			name:  "two-column help hangs under the description",
			in:    "  /confirm           Toggle confirm mode (propose actions before executing)",
			width: 60,
			want: []string{
				"  /confirm           Toggle confirm mode (propose actions",
				"                     before executing)",
			},
		},
		{
			name:  "table row hangs under its last column",
			in:    "✓ vertex          [TOOLS]      gemini-2.0-flash (preferred) (unverified) [OAuth required]",
			width: 71,
			want: []string{
				"✓ vertex          [TOOLS]      gemini-2.0-flash (preferred)",
				"                               (unverified) [OAuth required]",
			},
		},
		{
			name:  "single-spaced line keeps its leading indent",
			in:    "  /agents kill <id|name> Cancel a specific in-flight subagent (id, task id, or on-screen name)",
			width: 72,
			want: []string{
				"  /agents kill <id|name> Cancel a specific in-flight subagent (id, task",
				"  id, or on-screen name)",
			},
		},
		{
			name:  "unindented prose wraps at column 0",
			in:    "one two three four five six seven",
			width: 14,
			want:  []string{"one two three", "four five six", "seven"},
		},
		{
			name:  "a line that fits is untouched",
			in:    "  /plan off       Leave plan mode",
			width: 76,
			want:  []string{"  /plan off       Leave plan mode"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := strings.Split(wrapText(tc.in, tc.width), "\n")
			assert.Equal(t, tc.want, got)
			for _, row := range got {
				assert.LessOrEqual(t, visibleWidth(row), tc.width, row)
			}
		})
	}
}

// A hanging column too far right to leave room for text falls back to the
// line's own indent instead of a one-word-per-row column.
func TestWrapTextHangingIndentTooWideFallsBack(t *testing.T) {
	in := "  " + strings.Repeat("x", 30) + "     tail words that need to wrap here"
	got := strings.Split(wrapText(in, 40), "\n")
	require.Greater(t, len(got), 1)
	for _, row := range got[1:] {
		assert.True(t, strings.HasPrefix(row, "  ") && !strings.HasPrefix(row, "   "), "%q", row)
	}
}

// helpRowsKeepColumns checks every row of rendered help text: none loses its
// indent to column 0 (headers excepted), and none is wider than width.
func helpRowsKeepColumns(t *testing.T, out string, width int) {
	t.Helper()
	rows := strings.Split(out, "\n")
	for _, r := range rows[1:] { // rows[0] is the "System hh:mm" header
		assert.LessOrEqual(t, visibleWidth(r), width, r)
		if strings.HasPrefix(r, "/") {
			t.Errorf("row lost its indent: %q", r)
		}
	}
}

// #350 at both audit sizes: /help, /providers and /plan keep their indent
// and column spacing when a row is wider than the chat.
func TestWideHelpRowsKeepIndentAndColumns(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeAgentLLMClient{}, sz.w, sz.h)
			w := m.chat.width - 4

			m = auditSend(t, m, "/help")
			help := lastSystemRender(t, m, w)
			helpRowsKeepColumns(t, help, w)
			for _, want := range []string{
				"  /confirm           Toggle confirm mode",
				"  /agents kill <id|name> Cancel a specific in-flight subagent",
				"  /set-model [model] List or set the chat model",
				"  /list-models       List models",
			} {
				assert.True(t, hasRowPrefix(help, want), "missing row starting %q in\n%s", want, help)
			}
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			assert.Contains(t, frame, "   /list-models       List models", "frame keeps the indent")

			m = auditSend(t, m, "/providers")
			prov := lastSystemRender(t, m, w)
			helpRowsKeepColumns(t, prov, w)
			for _, want := range []string{
				"✓ openrouter      [TOOLS]      openai/gpt-4.1-nano",
				"✓ vertex          [TOOLS]      gemini-2.0-flash",
			} {
				assert.True(t, hasRowPrefix(prov, want), "missing row starting %q in\n%s", want, prov)
			}

			m.chat = m.chat.AddSystemMessage(planUsage)
			plan := lastSystemRender(t, m, w)
			helpRowsKeepColumns(t, plan, w)
			assert.True(t, hasRowPrefix(plan, "  /plan           Enter plan mode"), plan)
			for _, r := range strings.Split(plan, "\n")[2:] {
				assert.True(t, strings.HasPrefix(r, "  "), "plan usage row lost its indent: %q", r)
			}
		})
	}
}

func hasRowPrefix(out, prefix string) bool {
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

// A plain system message (the session list) wraps the same way.
func TestPlainSystemMessageHangingIndent(t *testing.T) {
	m := newAuditApp(t, &fakeAgentLLMClient{}, 80, 24)
	m.chat = m.chat.AddPlainSystemMessage("Commands:\n  /session resume \"<name>\"   - Load a saved session by its name and continue where it left off\n")
	out := lastSystemRender(t, m, m.chat.width-4)
	rows := strings.Split(out, "\n")
	require.GreaterOrEqual(t, len(rows), 4, out)
	assert.True(t, strings.HasPrefix(rows[2], "  /session resume \"<name>\"   - Load"), out)
	hang := strings.Repeat(" ", 29)
	assert.True(t, strings.HasPrefix(rows[3], hang) && rows[3][29] != ' ', "continuation hangs under the description: %q", rows[3])
}
