package decide

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
)

// CompleteFunc is one small-model call (compact.SummarizeFunc's shape).
type CompleteFunc func(ctx context.Context, system, user string) (string, error)

// LLM asks the small model and reads a JSON object back. No backend at
// 2.0 sets a provider JSON mode, so the JSON is required by the prompt and
// parsed leniently: the first {...} in the reply. The small model may be a
// remote provider, so the state is redacted first, as for Jev: secrets,
// and file paths (workspace-relative inside Workspace, <path> elsewhere).
type LLM struct {
	Complete  CompleteFunc
	Workspace string
}

const llmSystem = `You answer typed questions about a state document. Reply with one JSON object and nothing else. Its keys are the question ids. For a yes/no question the value is the probability of yes, a number from 0 to 1. For a score question the value is the 0-based number of the level that fits best. For a choice question the value is one of the listed options, as a string.`

func (o LLM) Ask(ctx context.Context, state string, qs []Question) (map[string]Answer, error) {
	if o.Complete == nil {
		return nil, errors.New("llm oracle: no small model")
	}
	reply, err := o.Complete(ctx, llmSystem, llmPrompt(redactText(state, o.Workspace), qs))
	if err != nil {
		return nil, err
	}
	start, end := strings.Index(reply, "{"), strings.LastIndex(reply, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("llm oracle: no JSON object in the reply")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(reply[start:end+1]), &raw); err != nil {
		return nil, fmt.Errorf("llm oracle: %w", err)
	}
	out := make(map[string]Answer, len(qs))
	for _, q := range qs {
		v, ok := raw[q.ID]
		if !ok {
			continue
		}
		if a, ok := parseLLMAnswer(q, v); ok {
			a.Source = "llm"
			out[q.ID] = a
		}
	}
	return out, nil
}

// redactText is state as it may leave the machine: a JSON document with
// every string redacted (jev.RedactValue), or redacted plain text.
func redactText(state, workspace string) string {
	if t := strings.TrimSpace(state); strings.HasPrefix(t, "{") {
		var v any
		if json.Unmarshal([]byte(t), &v) == nil {
			var b strings.Builder
			enc := json.NewEncoder(&b)
			enc.SetEscapeHTML(false)
			if enc.Encode(jev.RedactValue(v, workspace)) == nil {
				return strings.TrimSpace(b.String())
			}
		}
	}
	return jev.RedactAll(state, workspace)
}

func llmPrompt(state string, qs []Question) string {
	var b strings.Builder
	b.WriteString("State:\n")
	b.WriteString(state)
	b.WriteString("\n\nQuestions:\n")
	for _, q := range qs {
		switch q.Kind {
		case YesNo:
			fmt.Fprintf(&b, "- %s (yes/no): %s", q.ID, q.Text)
			if q.True != "" {
				fmt.Fprintf(&b, " Yes means: %s", q.True)
			}
			if q.False != "" {
				fmt.Fprintf(&b, " No means: %s", q.False)
			}
		case Score:
			fmt.Fprintf(&b, "- %s (score): %s Levels:", q.ID, q.Text)
			for i, l := range q.Levels {
				fmt.Fprintf(&b, " %d = %s;", i, l)
			}
		case Choice:
			keys := make([]string, 0, len(q.Options))
			for k := range q.Options {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			fmt.Fprintf(&b, "- %s (choice): %s Options:", q.ID, q.Text)
			for _, k := range keys {
				if d := q.Options[k]; d != "" {
					fmt.Fprintf(&b, " %q (%s);", k, d)
				} else {
					fmt.Fprintf(&b, " %q;", k)
				}
			}
		}
		b.WriteString("\n")
	}
	return b.String()
}

func parseLLMAnswer(q Question, v json.RawMessage) (Answer, bool) {
	switch q.Kind {
	case YesNo:
		var p float64
		if json.Unmarshal(v, &p) == nil {
			return Answer{P: clamp(p, 0, 1)}, true
		}
		var yes bool
		if json.Unmarshal(v, &yes) == nil {
			if yes {
				return Answer{P: 1}, true
			}
			return Answer{P: 0}, true
		}
	case Score:
		var s float64
		if json.Unmarshal(v, &s) == nil && len(q.Levels) > 0 {
			return Answer{Score: clamp(s, 0, float64(len(q.Levels)-1))}, true
		}
	case Choice:
		var c string
		if json.Unmarshal(v, &c) == nil {
			if _, ok := q.Options[c]; ok {
				return Answer{Choice: c}, true
			}
		}
	}
	return Answer{}, false
}

func clamp(x, lo, hi float64) float64 {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}
