package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadEvalCasesSupportsSuiteAndArray(t *testing.T) {
	tmpDir := t.TempDir()
	suitePath := filepath.Join(tmpDir, "suite.json")
	arrayPath := filepath.Join(tmpDir, "array.json")

	suite := `{"cases":[{"name":"a","goal":"do x"}]}`
	err := os.WriteFile(suitePath, []byte(suite), 0644)
	require.NoError(t, err)

	arr := `[{"name":"b","goal":"do y"}]`
	err = os.WriteFile(arrayPath, []byte(arr), 0644)
	require.NoError(t, err)

	cases, err := LoadEvalCases(suitePath)
	require.NoError(t, err)
	require.Len(t, cases, 1)
	assert.Equal(t, "a", cases[0].Name)

	cases, err = LoadEvalCases(arrayPath)
	require.NoError(t, err)
	require.Len(t, cases, 1)
	assert.Equal(t, "b", cases[0].Name)
}

func TestEvaluateCase(t *testing.T) {
	passed, reason := evaluateCase(EvalCase{
		MustContain:    []string{"hello"},
		MustNotContain: []string{"error"},
	}, StatusCompleted, "TASK_COMPLETE: hello world")
	assert.True(t, passed)
	assert.Equal(t, "ok", reason)

	passed, reason = evaluateCase(EvalCase{MustContain: []string{"missing"}}, StatusCompleted, "done")
	assert.False(t, passed)
	assert.Contains(t, reason, "missing required")

	passed, reason = evaluateCase(EvalCase{}, StatusFailed, "done")
	assert.False(t, passed)
	assert.Contains(t, reason, "status=")
}
