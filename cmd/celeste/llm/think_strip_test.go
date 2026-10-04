package llm

import (
	"strings"
	"testing"
)

// splitAll feeds s to a fresh splitter in the given pieces and returns the
// reply text and the thinking text it produced.
func splitAll(pieces ...string) (content, thinking string) {
	var s thinkSplitter
	var c, t strings.Builder
	for _, p := range pieces {
		pc, pt := s.Write(p)
		c.WriteString(pc)
		t.WriteString(pt)
	}
	pc, pt := s.Flush()
	c.WriteString(pc)
	t.WriteString(pt)
	return c.String(), t.String()
}

func TestThinkSplitter(t *testing.T) {
	cases := []struct {
		name, in, content, thinking string
	}{
		{"no tags", "Hello, darling.", "Hello, darling.", ""},
		{"leading block", "<think>Okay, the user wants a greeting.</think>\n\nHi there.", "Hi there.", "Okay, the user wants a greeting."},
		{"leading whitespace then block", "\n <think>plan</think>Hi", "Hi", "plan"},
		{"empty block", "<think>\n\n</think>\n\nHi", "Hi", "\n\n"},
		{"unterminated leading block", "<think>still reasoning when the stream ended", "", "still reasoning when the stream ended"},
		{"block after reply text is literal", "Answer: <think>check</think>42", "Answer: <think>check</think>42", ""},
		{"block after a leading one is literal", "<think>a</think>One <think>b</think>two", "One <think>b</think>two", "a"},
		{"two leading blocks", "<think>a</think>\n<think>b</think>Hi", "Hi", "ab"},
		{"quoted tags in a code block", "```\n<think>example</think>\n```", "```\n<think>example</think>\n```", ""},
		{"text before tag on one line", "x <think>y</think> z", "x <think>y</think> z", ""},
		{"literal tag mid reply is kept when never closed", "Qwen writes <think> before it reasons.", "Qwen writes <think> before it reasons.", ""},
		{"lone close tag is kept", "a </think> b", "a </think> b", ""},
		{"partial tag at end is kept", "x <thi", "x <thi", ""},
		{"whitespace only reply", "  ", "  ", ""},
		{"unicode around tags", "<think>考え中</think>こんにちは", "こんにちは", "考え中"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, th := splitAll(tc.in)
			if c != tc.content || th != tc.thinking {
				t.Fatalf("whole: got content %q thinking %q, want %q / %q", c, th, tc.content, tc.thinking)
			}
			// Every split into two and three chunks gives the same result:
			// tags split across chunk boundaries are still recognised.
			for i := 0; i <= len(tc.in); i++ {
				c, th := splitAll(tc.in[:i], tc.in[i:])
				if c != tc.content || th != tc.thinking {
					t.Fatalf("split at %d: got %q / %q, want %q / %q", i, c, th, tc.content, tc.thinking)
				}
				for j := i; j <= len(tc.in); j++ {
					c, th := splitAll(tc.in[:i], tc.in[i:j], tc.in[j:])
					if c != tc.content || th != tc.thinking {
						t.Fatalf("split at %d,%d: got %q / %q, want %q / %q", i, j, c, th, tc.content, tc.thinking)
					}
				}
			}
		})
	}
}

// A leading block streams as thinking while it arrives; nothing reaches
// the reply until the block closes.
func TestThinkSplitterStreamsLeadingThinking(t *testing.T) {
	var s thinkSplitter
	if c, th := s.Write("<think>Okay, "); c != "" || th != "Okay, " {
		t.Fatalf("first chunk: content %q thinking %q", c, th)
	}
	if c, th := s.Write("let me see"); c != "" || th != "let me see" {
		t.Fatalf("second chunk: content %q thinking %q", c, th)
	}
	if c, th := s.Write("</think>\n\nHi"); c != "Hi" || th != "" {
		t.Fatalf("closing chunk: content %q thinking %q", c, th)
	}
}

// A "<think>" after reply text is literal: it streams as reply text at
// once and holds nothing back until a close tag or the end of the stream.
func TestThinkSplitterPassesLateTagThrough(t *testing.T) {
	var s thinkSplitter
	if c, th := s.Write("Hi "); c != "Hi " || th != "" {
		t.Fatalf("first chunk: content %q thinking %q", c, th)
	}
	if c, th := s.Write("<think>x"); c != "<think>x" || th != "" {
		t.Fatalf("second chunk: content %q thinking %q", c, th)
	}
	if c, th := s.Write(" and more"); c != " and more" || th != "" {
		t.Fatalf("third chunk: content %q thinking %q", c, th)
	}
}

func TestStripThink(t *testing.T) {
	if got := stripThink("<think>x</think>\n\nHello"); got != "Hello" {
		t.Fatalf("stripThink = %q", got)
	}
	if got := stripThink("Use `<think>x</think>` tags."); got != "Use `<think>x</think>` tags." {
		t.Fatalf("stripThink(literal) = %q", got)
	}
	if got := stripThink("plain"); got != "plain" {
		t.Fatalf("stripThink(plain) = %q", got)
	}
}
