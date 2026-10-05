package main

import (
	"os"
	"strings"
	"testing"
)

// #361: Anthropic keeps a separate prompt cache per effort setting, so a
// /effort change re-writes the whole prefix (tools, system prompt and
// conversation) once, and switching back reads the earlier entry. The guide
// must say so, not that only the conversation is re-read.
func TestEffortCacheDocDescribesPerSettingCache(t *testing.T) {
	data, err := os.ReadFile("../../docs/LLM_PROVIDERS.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(data)
	start := strings.Index(doc, "**Anthropic prompt caching and `/effort`:**")
	if start < 0 {
		t.Fatal("the /effort caching paragraph is missing")
	}
	para := doc[start:]
	if end := strings.Index(para, "\n\n"); end >= 0 {
		para = para[:end]
	}
	for _, must := range []string{
		"separate prompt cache for each effort setting",
		"tools, the system prompt and the conversation",
		"switch back",
	} {
		if !strings.Contains(para, must) {
			t.Errorf("the /effort paragraph does not say %q", must)
		}
	}
	if strings.Contains(para, "reads the conversation from scratch") {
		t.Error("the /effort paragraph still says only the conversation is re-read")
	}
}
