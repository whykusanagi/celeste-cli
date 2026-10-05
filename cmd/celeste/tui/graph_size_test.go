package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// R1: /graph opens at the terminal's size, so the title stays on screen at
// 80x24 and the view uses the full width at 120x40.
func TestGraphOpensAtTerminalSize(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 25; i++ {
		src := fmt.Sprintf("package p\n\nfunc F%d() { F%d() }\n", i, (i+1)%25)
		require.NoError(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%d.go", i)), []byte(src), 0o644))
	}
	idx, err := codegraph.NewIndexer(dir, filepath.Join(t.TempDir(), "graph.db"))
	require.NoError(t, err)
	require.NoError(t, idx.Build())

	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h).WithCodeGraphIndexer(idx)
			m = auditSend(t, m, "/graph")
			require.NotNil(t, m.graphModel)
			assert.Equal(t, sz.w, m.graphModel.width)
			assert.Equal(t, sz.h, m.graphModel.height)
			frame := auditView(m)
			assert.Contains(t, frame, "CODE GRAPH", "title scrolled off")
			assert.LessOrEqual(t, len(strings.Split(frame, "\n")), sz.h)
		})
	}
}
