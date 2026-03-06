package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmitEventIncludesRunStateMetadata(t *testing.T) {
	runner := &Runner{}
	state := NewRunState("test goal", DefaultOptions())
	state.Turn = 3
	state.Status = StatusRunning

	var got RunEvent
	runner.SetEventSink(func(event RunEvent) {
		got = event
	})

	runner.emitEvent(state, "turn_start", "Turn started", map[string]any{"turn": 3})

	require.Equal(t, state.RunID, got.RunID)
	assert.Equal(t, "turn_start", got.Type)
	assert.Equal(t, "Turn started", got.Message)
	assert.Equal(t, 3, got.Turn)
	assert.Equal(t, StatusRunning, got.Status)
	assert.Equal(t, 3, got.Data["turn"])
}
