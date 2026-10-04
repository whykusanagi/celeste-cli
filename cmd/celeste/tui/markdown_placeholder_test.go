package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// V2 / #315: "> " inside "<goal> " and "| " inside "<key> | /voice" must not
// make plain command text look like markdown.
func TestLooksLikeMarkdownQuoteAndTableOnlyAtLineStart(t *testing.T) {
	plain := []string{
		"  /agent <goal>      Run autonomous task loop\n  /agent resume <id> Resume",
		"Usage: /voice set-key <key> | /voice set-voice <id>",
		"Usage: /session rename <session-id> <new-name>\n\nExample:",
		"  /session resume <id>       - Load session by ID\n",
	}
	for _, s := range plain {
		assert.False(t, looksLikeMarkdown(s), "plain text read as markdown: %q", s)
	}
	md := []string{
		"> quoted reply",
		"intro\n> quoted line",
		"intro\n   > indented quote",
		"| a | b |\n|---|---|\n| 1 | 2 |",
		"## Heading",
		"some **bold** text",
		"```go\nx := 1\n```",
	}
	for _, s := range md {
		assert.True(t, looksLikeMarkdown(s), "markdown missed: %q", s)
	}
}

// Placeholders must also survive when the message really is markdown.
func TestRenderMarkdownKeepsAngleBracketPlaceholders(t *testing.T) {
	src := "## Usage\n\nRun `/agent <goal>` or type /session resume <id> and /session rename <id> <name>.\n\n```\n<raw> stays\n```"
	out := stripANSI(renderMarkdown(src, 160))
	assert.Contains(t, out, "/session resume <id>")
	assert.Contains(t, out, "/session rename <id> <name>")
	assert.Contains(t, out, "/agent <goal>", "code span keeps its text")
	assert.Contains(t, out, "<raw> stays", "fenced code keeps its text")
	assert.NotContains(t, out, `\<`, "escape must not leak into the output")
}

func TestEscapeHTMLLikePlaceholders(t *testing.T) {
	cases := map[string]string{
		"use <id> here":           `use \<id> here`,
		"keep `<id>` in code":     "keep `<id>` in code",
		"a < b and <3":            "a < b and <3",
		"```\n<x>\n```\nthen <y>": "```\n<x>\n```\nthen \\<y>",
		"close </div> too":        `close \</div> too`,
		"``two <ticks>`` and <z>": "``two <ticks>`` and \\<z>",
		// Indented code blocks are code: escapes would show.
		"Example:\n\n    <div>hi</div>\n\n**bold** <b>": "Example:\n\n    <div>hi</div>\n\n**bold** \\<b>",
		"Example:\n\n\t<div>hi</div>\n\t<p>":            "Example:\n\n\t<div>hi</div>\n\t<p>",
		// Four spaces with no blank line before is a paragraph continuation.
		"text\n    <id>": "text\n    \\<id>",
		// Inside a list, an indented paragraph is list content, not code.
		"- item\n\n    more <id>": "- item\n\n    more \\<id>",
		// A longer fence is not closed by a shorter one.
		"````\n```\nstill <code>\n````\nafter <x>": "````\n```\nstill <code>\n````\nafter \\<x>",
		"~~~~\n~~~ <a>\n~~~~~\n<b>":                "~~~~\n~~~ <a>\n~~~~~\n\\<b>",
		// A closing fence has nothing after its run but whitespace.
		"```\n```go <c>\n```  \n<d>": "```\n```go <c>\n```  \n\\<d>",
		// Autolinks stay links.
		"see <https://example.com> and <id>": "see <https://example.com> and \\<id>",
		"mail <someone@example.com> now":     "mail <someone@example.com> now",
		"not a link <a b:c>":                 "not a link \\<a b:c>",
	}
	for in, want := range cases {
		assert.Equal(t, want, escapeHTMLLikeTags(in), in)
	}
}

// The six outputs the audit listed must keep their line breaks and
// placeholders on screen, at both audit sizes.
func TestCommandOutputsKeepPlaceholdersAndLines(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeAgentLLMClient{}, sz.w, sz.h)

			m = auditSend(t, m, "/help")
			help := lastSystemRender(t, m, sz.w-4)
			for _, want := range []string{"/agent <goal>", "/agent resume <id> Resume an existing agent run", "/endpoint <name>", "/orch <goal>"} {
				assert.Contains(t, help, want)
			}
			// Line breaks kept: each command is on its own row.
			assert.True(t, hasRow(help, "  /agent list-runs   List checkpointed agent runs"), help)
			frame := auditView(m)
			assert.Contains(t, frame, "Tip: Type / and press Tab for command autocomplete.")
			assertFrameFits(t, frame, sz.w, sz.h)

			m = auditSend(t, m, "/voice")
			assert.Contains(t, lastSystemRender(t, m, sz.w-4), "/voice set-key <key> | /voice set-voice <id>")
			assert.Contains(t, auditView(m), "<key>")

			m = auditSend(t, m, "/session rename")
			ren := lastSystemRender(t, m, sz.w-4)
			assert.Contains(t, ren, "/session rename <session-id> <new-name>")
			assert.Contains(t, auditView(m), "<new-name>")

			m = auditSend(t, m, "/providers")
			assert.Contains(t, lastSystemRender(t, m, sz.w-4), "/providers info <name>")
		})
	}
}

func TestNSFWHelpAndSessionFooterKeepPlaceholders(t *testing.T) {
	m := newAuditApp(t, &fakeAgentLLMClient{}, 120, 40)
	m.nsfwMode = true
	m = auditSend(t, m, "/help")
	nsfw := lastSystemRender(t, m, 116)
	assert.Contains(t, nsfw, "image: <prompt>")
	assert.Contains(t, nsfw, "/set-model <model>")

	footer := "\n📋 Saved Sessions (1):\n\n  ★ s1\n\nCommands:\n" +
		"  /session resume <id>       - Load session by ID\n" +
		"  /session resume \"<name>\"   - Load session by name\n" +
		"  /session rename <id> <name> - Rename a session\n" +
		"  /session delete <id>       - Delete a session\n"
	m.chat = m.chat.AddSystemMessage(footer)
	out := lastSystemRender(t, m, 76)
	require.NotEmpty(t, out)
	for _, want := range []string{"/session resume <id>", `/session resume "<name>"`, "/session rename <id> <name>", "/session delete <id>"} {
		assert.Contains(t, out, want, "#315")
	}
	assert.True(t, strings.Count(out, "\n") >= 8, "lines kept")
}

// hasRow reports whether some row of out, trailing padding removed, equals want.
func hasRow(out, want string) bool {
	for _, l := range strings.Split(out, "\n") {
		if strings.TrimRight(l, " ") == want {
			return true
		}
	}
	return false
}
