package memories

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractCorrection(t *testing.T) {
	candidates := ExtractCandidates("No, don't use tabs, use spaces instead", "OK.")
	assert.NotEmpty(t, candidates)
	assert.Equal(t, "feedback", candidates[0].Type)
	assert.Contains(t, candidates[0].Reason, "correction")
}

func TestExtractExplicit(t *testing.T) {
	candidates := ExtractCandidates("Remember that I prefer dark mode", "Got it.")
	assert.Len(t, candidates, 1)
	assert.Equal(t, "user", candidates[0].Type)
	assert.Contains(t, candidates[0].Reason, "explicit")
}

func TestExtractDecision(t *testing.T) {
	candidates := ExtractCandidates("We decided to use PostgreSQL because it supports JSONB", "Good choice.")
	assert.NotEmpty(t, candidates)

	hasProject := false
	for _, c := range candidates {
		if c.Type == "project" {
			hasProject = true
		}
	}
	assert.True(t, hasProject)
}

func TestExtractNone(t *testing.T) {
	candidates := ExtractCandidates("Hello, how are you?", "I'm fine, thanks!")
	assert.Empty(t, candidates)
}

func TestExtractExplicitTakesPriority(t *testing.T) {
	// "remember" is explicit, should short-circuit and not also flag corrections.
	candidates := ExtractCandidates("Remember, don't use tabs", "OK.")
	assert.Len(t, candidates, 1)
	assert.Equal(t, "user", candidates[0].Type)
}

func TestExtractCaseInsensitive(t *testing.T) {
	candidates := ExtractCandidates("STOP doing that", "Sorry.")
	assert.NotEmpty(t, candidates)
	assert.Equal(t, "feedback", candidates[0].Type)
}

func TestExtractForNextTime(t *testing.T) {
	candidates := ExtractCandidates("For next time, always run tests first", "Noted.")
	assert.Len(t, candidates, 1)
	assert.Equal(t, "user", candidates[0].Type)
}
