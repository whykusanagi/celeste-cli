package orchestrator

import (
	"strings"
)

// laneKeywords maps keyword → lane. First match wins.
var laneKeywords = []struct {
	keywords []string
	lane     TaskLane
}{
	{[]string{"fix", "refactor", "debug", "test", "build", "compile", "implement", "lint", "patch", "bug", "script", "bash", "create", "code", "function", "program", "deploy", "automate"}, LaneCode},
	{[]string{"write", "draft", "blog", "docs", "document", "summarize", "explain", "describe", "article"}, LaneContent},
	{[]string{"upscale", "image", "video", "render", "convert", "generate image", "generate video", "media"}, LaneMedia},
	{[]string{"review", "audit", "check", "critique", "blind review", "code review"}, LaneReview},
	{[]string{"research", "find", "search", "compare", "what is", "how does", "investigate", "explore"}, LaneResearch},
}

// ClassifyHeuristic returns the best-guess TaskLane and a confidence score (0.0–1.0)
// based purely on keyword matching. Confidence < 0.5 means the goal is ambiguous.
func ClassifyHeuristic(goal string) (TaskLane, float64) {
	lower := strings.ToLower(goal)

	best := LaneUnknown
	bestScore := 0.0

	for _, entry := range laneKeywords {
		score := 0.0
		for _, kw := range entry.keywords {
			// Support multi-word keywords (e.g. "blind review")
			if strings.Contains(lower, kw) {
				score += 1.0 / float64(len(entry.keywords))
			}
		}
		if score > bestScore {
			bestScore = score
			best = entry.lane
		}
	}

	// Normalise: max possible score per lane is 1.0; scale to 0.5–0.95 range.
	if best == LaneUnknown {
		return LaneUnknown, 0.1
	}
	confidence := 0.5 + bestScore*0.45
	if confidence > 0.95 {
		confidence = 0.95
	}
	return best, confidence
}
