package llm

// Plain returns a copy of c for a one-shot completion that carries its own
// system prompt (a summary, an oracle question): no xAI collections or
// features. c is not modified.
func (c *Config) Plain() *Config {
	p := *c
	p.Collections = nil
	p.XAIFeatures = nil
	return &p
}
