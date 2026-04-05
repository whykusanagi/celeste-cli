// Package llm provides the LLM client for Celeste CLI.
package llm

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
