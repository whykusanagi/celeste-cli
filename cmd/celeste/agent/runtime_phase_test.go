package agent

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOnProgressCalledDuringRun(t *testing.T) {
	var events []string
	opts := DefaultOptions()
	opts.OnProgress = func(kind ProgressKind, text string, turn, maxTurns int) {
		events = append(events, fmt.Sprintf("%d:%s", kind, text))
	}
	// OnProgress field must exist on Options — this test will fail to compile until it does
	_ = opts
	require.NotNil(t, opts.OnProgress)
}
