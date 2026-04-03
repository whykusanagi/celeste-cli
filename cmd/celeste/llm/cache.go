// Package llm provides the LLM client for Celeste CLI.
package llm

import "strings"

// CacheablePrompt separates the system prompt into a static prefix (suitable
// for provider-side prompt caching) and a dynamic suffix that changes between
// turns.  The static prefix typically contains the persona definition, tool
// schemas, and grimoire content. The dynamic suffix holds git status snapshots,
// current date, memories, and other volatile context.
type CacheablePrompt struct {
	StaticPrefix  string // persona + tool schemas + grimoire (cacheable)
	DynamicSuffix string // git status + date + memories (not cached)
}

// FullPrompt returns the complete system prompt by joining the static and
// dynamic sections with a clear separator.
func (cp CacheablePrompt) FullPrompt() string {
	if cp.DynamicSuffix == "" {
		return cp.StaticPrefix
	}
	if cp.StaticPrefix == "" {
		return cp.DynamicSuffix
	}
	return cp.StaticPrefix + "\n\n---\n\n" + cp.DynamicSuffix
}

// BuildCacheablePrompt constructs a CacheablePrompt from its constituent parts.
// The static prefix (persona + grimoire) stays stable across turns and can be
// cached by providers that support prompt caching (Anthropic cache_control,
// Gemini cachedContent). The dynamic suffix changes per turn.
func BuildCacheablePrompt(persona, grimoire, gitSnapshot, memories, date string) CacheablePrompt {
	// Static: persona + grimoire (rarely changes within a session)
	var staticParts []string
	if persona != "" {
		staticParts = append(staticParts, persona)
	}
	if grimoire != "" {
		staticParts = append(staticParts, grimoire)
	}

	// Dynamic: git status + memories + date (changes between turns)
	var dynamicParts []string
	if gitSnapshot != "" {
		dynamicParts = append(dynamicParts, "## Current Git Status\n"+gitSnapshot)
	}
	if memories != "" {
		dynamicParts = append(dynamicParts, "## Memories\n"+memories)
	}
	if date != "" {
		dynamicParts = append(dynamicParts, "## Current Date\n"+date)
	}

	return CacheablePrompt{
		StaticPrefix:  strings.Join(staticParts, "\n\n"),
		DynamicSuffix: strings.Join(dynamicParts, "\n\n"),
	}
}
