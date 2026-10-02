package decide

import (
	"context"
	"errors"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/jev"
)

// Jev is the TypeSafe System One oracle. The state goes to a third party:
// the client redacts it before it is sent.
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
	answers, _, err := j.Client.Ask(ctx, wireState(state), wire)
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

// wireState is the state as Jev receives it: the parsed State (sent as a
// JSON object) or the plain text. It is not redacted here: jev.Client.Ask
// runs jev.RedactValue (secrets and paths) over the whole value.
func wireState(state string) any {
	st := ParseState(state)
	if st.Text == state && st.Goal == "" && len(st.Turns) == 0 && st.Call == nil {
		return state
	}
	return st
}
