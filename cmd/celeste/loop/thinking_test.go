package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// Reasoning from an OpenAI-compatible server, in either shape, streams as
// EventThinking and stays out of the reply, the history and the next
// request (L4).
func TestLoopThinkingStaysOutOfReplyAndHistory(t *testing.T) {
	shapes := map[string]fakeprovider.Turn{
		"inline think": {Deltas: []string{"<thi", "nk>Okay, the user ", "wants a greeting.</th", "ink>\n\nHi, ", "darling."}},
		"reasoning":    {ReasoningDeltas: []string{"Okay, the user ", "wants a greeting."}, Deltas: []string{"Hi, ", "darling."}},
		"reasoning_content": {ReasoningDeltas: []string{"Okay, the user ", "wants a greeting."}, ReasoningField: "reasoning_content",
			Deltas: []string{"Hi, ", "darling."}},
	}
	for name, turn := range shapes {
		t.Run(name, func(t *testing.T) {
			l, srv := fakeLoop(t, turn, fakeprovider.Turn{Text: "again"})
			wait := collect(l)
			msgs, res, err := l.Run(context.Background(), userMsg("hi"))
			if err != nil {
				t.Fatal(err)
			}
			evs := wait()
			var thinking, text strings.Builder
			for _, ev := range evs {
				switch ev.Kind {
				case EventThinking:
					if text.Len() > 0 {
						t.Fatal("thinking after reply text")
					}
					thinking.WriteString(ev.Text)
				case EventTextDelta:
					text.WriteString(ev.Text)
				}
			}
			if thinking.String() != "Okay, the user wants a greeting." {
				t.Fatalf("thinking = %q", thinking.String())
			}
			if text.String() != "Hi, darling." || res.FinalText != "Hi, darling." {
				t.Fatalf("reply = %q, final %q", text.String(), res.FinalText)
			}
			if msgs[len(msgs)-1].Content != "Hi, darling." {
				t.Fatalf("history = %+v", msgs)
			}
			wait = collect(l) // Run blocks on an unread events channel
			if _, _, err := l.Run(context.Background(), append(msgs, Message{Role: "user", Content: "more"})); err != nil {
				t.Fatal(err)
			}
			wait()
			if raw := string(srv.Requests()[1].Raw); strings.Contains(raw, "Okay, the user") || strings.Contains(raw, "think") {
				t.Fatalf("reasoning sent back: %s", raw)
			}
		})
	}
}
