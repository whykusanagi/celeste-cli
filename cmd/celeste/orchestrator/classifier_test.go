package orchestrator_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/orchestrator"
)

func TestClassifyKeywords(t *testing.T) {
	cases := []struct {
		goal string
		want orchestrator.TaskLane
	}{
		{"fix the flaky test in auth_test.go", orchestrator.LaneCode},
		{"refactor the database layer", orchestrator.LaneCode},
		{"write a blog post about Go generics", orchestrator.LaneContent},
		{"summarize this document", orchestrator.LaneContent},
		{"upscale this image to 4k", orchestrator.LaneMedia},
		{"convert the video to mp4", orchestrator.LaneMedia},
		{"review my pull request", orchestrator.LaneReview},
		{"blind audit of main.go", orchestrator.LaneReview},
		{"research the best Go ORMs", orchestrator.LaneResearch},
		{"find all mentions of deprecated functions", orchestrator.LaneResearch},
	}
	for _, tc := range cases {
		t.Run(tc.goal, func(t *testing.T) {
			got, confidence := orchestrator.ClassifyHeuristic(tc.goal)
			assert.Equal(t, tc.want, got, "goal: %q", tc.goal)
			assert.Greater(t, confidence, 0.5, "expected confidence > 0.5 for: %q", tc.goal)
		})
	}
}

func TestClassifyUnknownReturnsLowConfidence(t *testing.T) {
	lane, confidence := orchestrator.ClassifyHeuristic("do the thing")
	assert.Equal(t, orchestrator.LaneUnknown, lane)
	assert.Less(t, confidence, 0.5)
}
