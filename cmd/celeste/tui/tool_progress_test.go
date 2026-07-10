package tui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRenderEntry_DoneShowsMessageAndElapsed(t *testing.T) {
	start := time.Unix(1000, 0)
	m := ToolProgressModel{width: 60}
	e := toolProgressEntry{
		name:      "read_file",
		state:     "done",
		message:   "read internal/foo.go",
		startedAt: start,
		doneAt:    start.Add(1200 * time.Millisecond),
	}
	out := m.renderEntry(e)
	assert.Contains(t, out, "read_file")
	assert.Contains(t, out, "1.2s")                 // elapsed
	assert.Contains(t, out, "read internal/foo.go") // message now rendered
	assert.Contains(t, out, "─")                    // rounded border box
}

func TestToolProgressView_HasLegend(t *testing.T) {
	m := ToolProgressModel{
		width:   60,
		entries: []toolProgressEntry{{name: "bash", state: "executing", startedAt: time.Unix(1000, 0)}},
	}
	out := m.View()
	assert.Contains(t, out, "running")
	assert.Contains(t, out, "done")
	assert.Contains(t, out, "failed")
}
