package memories

import (
	"strings"
)

// MemoryCandidate represents a potential memory extracted from conversation.
type MemoryCandidate struct {
	Type    string `json:"type"`    // feedback, project, user, reference
	Content string `json:"content"` // the extracted content
	Reason  string `json:"reason"`  // why this was flagged
}

// correctionPatterns match user corrections that should become feedback memories.
var correctionPatterns = []string{
	"no,",
	"no ",
	"don't",
	"dont",
	"stop ",
	"actually,",
	"actually ",
	"use x instead",
	"use this instead",
	"instead of",
	"not like that",
	"that's wrong",
	"thats wrong",
	"wrong,",
	"incorrect",
}

// explicitPatterns match explicit memory requests.
var explicitPatterns = []string{
	"remember ",
	"remember,",
	"note that",
	"for next time",
	"for future reference",
	"keep in mind",
	"don't forget",
	"dont forget",
}

// decisionPatterns match project decisions.
var decisionPatterns = []string{
	"because ",
	"we decided",
	"reason is",
	"the reason",
	"decision:",
	"agreed to",
	"let's go with",
	"lets go with",
	"going with",
}

// ExtractCandidates analyzes a user message and assistant response
// for potential memories to save.
func ExtractCandidates(userMsg, assistantMsg string) []MemoryCandidate {
	var candidates []MemoryCandidate
	userLower := strings.ToLower(userMsg)

	// Check for explicit save requests first (highest priority).
	for _, pattern := range explicitPatterns {
		if strings.Contains(userLower, pattern) {
			candidates = append(candidates, MemoryCandidate{
				Type:    "user",
				Content: userMsg,
				Reason:  "explicit memory request detected: '" + pattern + "'",
			})
			return candidates // Explicit requests are definitive.
		}
	}

	// Check for corrections / feedback.
	for _, pattern := range correctionPatterns {
		if strings.Contains(userLower, pattern) {
			candidates = append(candidates, MemoryCandidate{
				Type:    "feedback",
				Content: userMsg,
				Reason:  "correction pattern detected: '" + pattern + "'",
			})
			break // One feedback candidate is enough.
		}
	}

	// Check for decisions.
	for _, pattern := range decisionPatterns {
		if strings.Contains(userLower, pattern) {
			candidates = append(candidates, MemoryCandidate{
				Type:    "project",
				Content: userMsg,
				Reason:  "decision pattern detected: '" + pattern + "'",
			})
			break
		}
	}

	return candidates
}
