package tui

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// indexRunResult turns the result of /index rebuild or /index update into
// the message the TUI shows. Another celeste process writing the index
// (ErrIndexBusy) is a notice with the index as it is, not a failure (#392).
func indexRunResult(op, done string, err error, indexer *codegraph.Indexer) tea.Msg {
	busy := errors.Is(err, codegraph.ErrIndexBusy)
	if err != nil && !busy {
		return StreamErrorMsg{Err: fmt.Errorf("%s failed: %w", op, err)}
	}
	stats, _ := indexer.Stats()
	counts := fmt.Sprintf("%d files, %d symbols, %d edges", stats.TotalFiles, stats.TotalSymbols, stats.TotalEdges)
	if busy {
		return AgentProgressMsg{Kind: AgentProgressResponse,
			Text: fmt.Sprintf("Index %s skipped: another celeste process is indexing this project. The index as it is: %s", op, counts)}
	}
	return AgentProgressMsg{Kind: AgentProgressResponse, Text: fmt.Sprintf("Index %s: %s", done, counts)}
}
