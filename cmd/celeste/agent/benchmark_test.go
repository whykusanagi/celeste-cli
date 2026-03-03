package agent

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadBenchmarkSuiteSupportsObjectAndArray(t *testing.T) {
	tmp := t.TempDir()
	objPath := filepath.Join(tmp, "suite.json")
	arrPath := filepath.Join(tmp, "array.json")

	obj := `{"name":"agent_suite","iterations":2,"cases":[{"name":"c1","goal":"g1"}]}`
	err := os.WriteFile(objPath, []byte(obj), 0644)
	require.NoError(t, err)

	arr := `[{"name":"c2","goal":"g2"}]`
	err = os.WriteFile(arrPath, []byte(arr), 0644)
	require.NoError(t, err)

	suite, err := LoadBenchmarkSuite(objPath)
	require.NoError(t, err)
	assert.Equal(t, "agent_suite", suite.Name)
	assert.Equal(t, 2, suite.Iterations)
	require.Len(t, suite.Cases, 1)

	suite, err = LoadBenchmarkSuite(arrPath)
	require.NoError(t, err)
	assert.Equal(t, "array", suite.Name)
	assert.Equal(t, 1, suite.Iterations)
	require.Len(t, suite.Cases, 1)
}

func TestAggregateBenchmarkCase(t *testing.T) {
	runs := []benchmarkIteration{
		{Passed: true, Status: StatusCompleted, Turns: 4, ToolCalls: 3, Duration: 100 * time.Millisecond},
		{Passed: false, Reason: "status=max_turns_reached", Status: StatusMaxTurnsReached, Turns: 8, ToolCalls: 5, Duration: 200 * time.Millisecond},
		{Passed: true, Status: StatusCompleted, Turns: 6, ToolCalls: 4, Duration: 300 * time.Millisecond},
	}

	result := aggregateBenchmarkCase("case_a", runs)
	assert.Equal(t, "case_a", result.CaseName)
	assert.Equal(t, 3, result.Iterations)
	assert.Equal(t, 2, result.PassedIterations)
	assert.Equal(t, 1, result.FailedIterations)
	assert.InDelta(t, 0.6666, result.PassRate, 0.001)
	assert.InDelta(t, 6.0, result.AverageTurns, 0.001)
	assert.InDelta(t, 4.0, result.AverageToolCalls, 0.001)
	assert.InDelta(t, 200.0, result.AverageDurationMS, 0.001)
	assert.Contains(t, result.FailureReasons, "status=max_turns_reached")
}
