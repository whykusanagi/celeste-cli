// Package decide asks typed questions about a run (2.0 W3, #175): yes/no,
// score and choice. Every Oracle answers the same questions; Guarded wraps
// a model-backed one so that any error, timeout or unanswered question falls
// back to the questions' own heuristics. Callers never see an error.
package decide

import (
	"context"
	"encoding/json"
	"strings"
)

// Kind is a question's type, named as TypeSafe names them.
type Kind string

const (
	YesNo  Kind = "noul"
	Score  Kind = "score"
	Choice Kind = "choice"
)

// Question is one typed question.
type Question struct {
	ID   string
	Kind Kind
	Text string // what to decide; may name fields of the state in `backticks`
	// YesNo: optional criteria for yes and no.
	True, False string
	// Score: the ordered level descriptions, 2 to 10. Answer.Score is the
	// 0-based level.
	Levels []string
	// Choice: option → description ("" for none).
	Options map[string]string
	// Heuristic answers without a model. Nil, or ok == false, is no opinion.
	Heuristic func(State) (Answer, bool)
}

// Answer is one typed answer.
type Answer struct {
	P          float64            // YesNo: P(yes)
	Score      float64            // Score: 0-based level, probability-weighted
	Choice     string             // Choice: the most likely option
	Probs      map[string]float64 // Choice and Score: the distribution, when known
	Confidence float64            // Choice and Score: 0..1, when known
	Source     string             // "heuristic", "llm" or "jev"
}

// Oracle answers questions about state, a JSON document (State.String) or
// plain text. The map holds the questions it could answer.
type Oracle interface {
	Ask(ctx context.Context, state string, qs []Question) (map[string]Answer, error)
}

// State is what the oracle sees: the goal, the latest user message, the
// recent turns, and free text for questions about one string (routing).
// Every string is redacted (jev.RedactAll: secrets and file paths) before
// it leaves the machine.
type State struct {
	Goal   string     `json:"goal,omitempty"`
	Latest string     `json:"latest_user_message,omitempty"`
	Turns  []TurnView `json:"recent_turns,omitempty"`
	Call   *CallView  `json:"pending_call,omitempty"`
	Text   string     `json:"text,omitempty"`
}

// TurnView is one model turn: what it said and the calls it made.
type TurnView struct {
	Assistant string     `json:"assistant,omitempty"`
	Calls     []CallView `json:"calls,omitempty"`
}

// CallView is one tool call and, once it ran, its result.
type CallView struct {
	Tool    string `json:"tool"`
	Args    string `json:"args,omitempty"`
	Result  string `json:"result,omitempty"`
	IsError bool   `json:"is_error,omitempty"`
}

// String is the state as JSON, the form every Oracle takes.
func (s State) String() string {
	b, _ := json.Marshal(s)
	return string(b)
}

// ParseState reads a State back. Text that is not a JSON object becomes
// State.Text.
func ParseState(s string) State {
	var st State
	if t := strings.TrimSpace(s); strings.HasPrefix(t, "{") && json.Unmarshal([]byte(t), &st) == nil {
		return st
	}
	return State{Text: s}
}

// Heuristic is the default Oracle: each question's Heuristic, no model.
type Heuristic struct{}

func (Heuristic) Ask(_ context.Context, state string, qs []Question) (map[string]Answer, error) {
	st := ParseState(state)
	out := make(map[string]Answer, len(qs))
	for _, q := range qs {
		if q.Heuristic == nil {
			continue
		}
		if a, ok := q.Heuristic(st); ok {
			a.Source = "heuristic"
			out[q.ID] = a
		}
	}
	return out, nil
}
