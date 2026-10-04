package llm

import "strings"

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

// thinkSplitter separates <think>…</think> reasoning from the reply text of
// an OpenAI-compatible chat-completions stream. Servers without a reasoning
// parser (llama.cpp, LM Studio, vLLM, some MLX servers running qwen3 or
// deepseek-r1) put the model's reasoning in content; it must reach neither
// the reply nor the history.
//
// Write takes the next content delta and returns the reply text and the
// thinking text it completes; Flush returns what is still held at the end
// of the stream. Tags split across deltas are recognised. Servers only put
// reasoning at the start of a message, so only a block that opens before
// any reply text (whitespace aside) is reasoning: it streams as thinking at
// once, and is still reasoning when the stream ends before it closes. A
// "<think>" after reply text is literal text and streams as reply text.
// Whitespace before the first reply text that follows a leading block is
// dropped.
//
// It is used by one stream at a time; the zero value is ready.
type thinkSplitter struct {
	pending  string // a possible partial tag at the end of the last delta
	inThink  bool   // inside a leading block
	visible  bool   // reply text other than whitespace has been returned
	ws       string // whitespace before the first reply text, not yet returned
	sawThink bool   // a leading block was seen
}

// Write consumes one content delta.
func (s *thinkSplitter) Write(delta string) (content, thinking string) {
	var c, t strings.Builder
	buf := s.pending + delta
	s.pending = ""
	for buf != "" {
		if s.inThink {
			if i := strings.Index(buf, thinkClose); i >= 0 {
				t.WriteString(buf[:i])
				s.inThink = false
				buf = buf[i+len(thinkClose):]
				continue
			}
			keep := partialSuffix(buf, thinkClose)
			t.WriteString(buf[:len(buf)-keep])
			s.pending = buf[len(buf)-keep:]
			break
		}
		if s.visible {
			c.WriteString(buf)
			break
		}
		if i := strings.Index(buf, thinkOpen); i >= 0 && strings.TrimSpace(s.ws+buf[:i]) == "" {
			s.ws = ""
			s.sawThink = true
			s.inThink = true
			buf = buf[i+len(thinkOpen):]
			continue
		}
		keep := partialSuffix(buf, thinkOpen)
		s.reply(&c, buf[:len(buf)-keep])
		if s.visible {
			c.WriteString(buf[len(buf)-keep:])
		} else {
			s.pending = buf[len(buf)-keep:]
		}
		break
	}
	return c.String(), t.String()
}

// Flush ends the stream and returns what was still held.
func (s *thinkSplitter) Flush() (content, thinking string) {
	var c, t strings.Builder
	if s.inThink {
		t.WriteString(s.pending) // reasoning cut off by the end of the stream
	} else {
		s.reply(&c, s.pending)
	}
	s.pending, s.inThink = "", false
	if !s.visible && !s.sawThink {
		c.WriteString(s.ws) // a whitespace-only reply stays as it was
	}
	s.ws = ""
	return c.String(), t.String()
}

// reply adds reply text, holding whitespace until the first other
// character.
func (s *thinkSplitter) reply(c *strings.Builder, text string) {
	if text == "" {
		return
	}
	if s.visible {
		c.WriteString(text)
		return
	}
	s.ws += text
	if strings.TrimSpace(s.ws) == "" {
		return
	}
	s.visible = true
	if s.sawThink {
		c.WriteString(strings.TrimLeft(s.ws, " \t\r\n"))
	} else {
		c.WriteString(s.ws)
	}
	s.ws = ""
}

// partialSuffix is the length of the longest proper prefix of tag that buf
// ends with.
func partialSuffix(buf, tag string) int {
	for n := min(len(tag)-1, len(buf)); n > 0; n-- {
		if strings.HasSuffix(buf, tag[:n]) {
			return n
		}
	}
	return 0
}

// stripThink is s without its leading <think> reasoning: for a whole reply, and
// for history sent back to an OpenAI-compatible server.
func stripThink(s string) string {
	if !strings.Contains(s, thinkOpen) {
		return s
	}
	var sp thinkSplitter
	c, _ := sp.Write(s)
	rest, _ := sp.Flush()
	return c + rest
}
