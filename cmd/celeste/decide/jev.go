package decide

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
)

// Jev is the TypeSafe System One oracle. The state is redacted before it is
// sent: it goes to a third party.
type Jev struct{ Client *jev.Client }

func (j Jev) Ask(ctx context.Context, state string, qs []Question) (map[string]Answer, error) {
	if j.Client == nil {
		return nil, errors.New("jev: no client")
	}
	wire := make(map[string]jev.Question, len(qs))
	for _, q := range qs {
		w := jev.Question{Type: string(q.Kind), Instructions: q.Text}
		switch q.Kind {
		case YesNo:
			if q.True != "" || q.False != "" {
				w.Criteria = map[string]string{"true": q.True, "false": q.False}
			}
		case Score:
			w.Criteria = q.Levels
		case Choice:
			opts := make(map[string]any, len(q.Options))
			for k, v := range q.Options {
				if v == "" {
					opts[k] = nil
				} else {
					opts[k] = v
				}
			}
			w.Criteria = opts
		}
		wire[q.ID] = w
	}
	answers, _, err := j.Client.Ask(ctx, redactState(state), wire)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Answer, len(answers))
	for id, a := range answers {
		switch {
		case a.Noul != nil:
			out[id] = Answer{P: *a.Noul, Source: "jev"}
		case a.Score != nil:
			out[id] = Answer{Score: *a.Score, Probs: a.Probabilities, Confidence: a.Confidence, Source: "jev"}
		case a.Choice != "":
			out[id] = Answer{Choice: a.Choice, Probs: a.Probabilities, Confidence: a.Confidence, Source: "jev"}
		}
	}
	return out, nil
}

// redactState returns the state as Jev receives it: a State with every
// string redacted, as a JSON object, or redacted plain text.
func redactState(state string) any {
	st := ParseState(state)
	if st.Text == state && st.Goal == "" && len(st.Turns) == 0 && st.Call == nil {
		return jev.Redact(state)
	}
	st.Goal, st.Latest, st.Text = jev.Redact(st.Goal), jev.Redact(st.Latest), jev.Redact(st.Text)
	redactCall := func(c CallView) CallView {
		c.Args, c.Result = jev.Redact(c.Args), jev.Redact(c.Result)
		return c
	}
	for i := range st.Turns {
		st.Turns[i].Assistant = jev.Redact(st.Turns[i].Assistant)
		for k := range st.Turns[i].Calls {
			st.Turns[i].Calls[k] = redactCall(st.Turns[i].Calls[k])
		}
	}
	if st.Call != nil {
		c := redactCall(*st.Call)
		st.Call = &c
	}
	var obj map[string]any
	b, _ := json.Marshal(st)
	_ = json.Unmarshal(b, &obj)
	return obj
}
