package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// smallIndexer indexes a two-file Go module, like the audit's test project.
func smallIndexer(t *testing.T) *codegraph.Indexer {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":       "module example.com/p\n\ngo 1.22\n",
		"main.go":      "package main\n\nfunc greet() string { return \"hi\" }\n\nfunc main() { println(greet()) }\n",
		"main_test.go": "package main\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) { _ = greet() }\n",
	}
	for name, body := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644))
	}
	idx, err := codegraph.NewIndexer(dir, filepath.Join(t.TempDir(), "codegraph.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = idx.Close() })
	require.NoError(t, idx.Build())
	return idx
}

// N8: the /index banner rules fit the chat: each is one row, none spills
// "▀▀"/"▄▄" onto a row of its own, at 80x24 and 120x40.
func TestIndexBannerFitsTheChatAt80And120(t *testing.T) {
	idx := smallIndexer(t)
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h).WithCodeGraphIndexer(idx)
			m = auditSend(t, m, "/index")
			rendered := lastSystemRender(t, m, m.chat.width-4)
			require.Contains(t, rendered, "CODE GRAPH")
			var top, bottom int
			for _, l := range strings.Split(rendered, "\n") {
				body := strings.TrimSpace(l)
				if body == "" {
					continue
				}
				if strings.Trim(body, "▀") == "" {
					top++
					assert.Greater(t, len([]rune(body)), sz.w/2, "a short ▀ row is a spill: %q", l)
				}
				if strings.Trim(body, "▄") == "" {
					bottom++
					assert.Greater(t, len([]rune(body)), sz.w/2, "a short ▄ row is a spill: %q", l)
				}
			}
			assert.Equal(t, 1, top, "top rule rows:\n%s", rendered)
			assert.Equal(t, 1, bottom, "bottom rule rows:\n%s", rendered)
			assertFrameFits(t, auditView(m), sz.w, sz.h)
		})
	}
}
