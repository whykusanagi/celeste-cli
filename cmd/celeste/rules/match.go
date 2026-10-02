package rules

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Facts are what the built-in guards know about the session, kept by the
// Matcher from tool results.
type Facts struct {
	TTSRan   bool // generate_speech returned without an error this session
	editSeq  int  // the last successful write_file / patch_file / splice_file
	checkSeq int  // the last successful bash call
	seq      int
	// RuntimeVerifies is set for an agent run with verification commands:
	// the runtime checks the work after TASK_COMPLETE, so the
	// task-complete-before-verify rule stands down.
	RuntimeVerifies bool
}

// Unverified reports a file changed after the last command that ran.
func (f *Facts) Unverified() bool { return f.editSeq > f.checkSeq }

// Hit is one rule firing.
type Hit struct {
	Rule  *Rule
	Scope Scope
	Text  string // what matched
	Call  *Call  // tool_args scope: the call
	Value string // tool_args scope: the whole argument value matched
}

// Call is one tool call as the matcher sees it.
type Call struct {
	ID    string
	Name  string
	Input map[string]any
}

// scanBack is how much of the reply the matcher keeps before each new
// delta, so a match split across deltas is found. It also bounds the text
// a scan reads (rules are RE2, linear in it) and the matcher's memory. A
// match longer than this that straddles many deltas can be missed.
const scanBack = 4096

// scanEvery batches text scans: the rules run once at least this many new
// bytes have arrived, or at a newline, and Flush runs them on what is left
// when the stream ends. A provider streaming a few bytes per delta would
// otherwise rescan the 4 KB tail with every rule on every delta.
const scanEvery = 256

// Matcher runs a Set over one session. Not safe for concurrent use: the
// caller (steer.Session) serializes it.
type Matcher struct {
	set     *Set
	facts   Facts
	request int            // requests started this session
	last    map[string]int // rule → request it last fired
	fired   map[string]bool
	tail    []byte // up to scanBack scanned bytes of this request's reply, then the unscanned ones
	pending int    // bytes at the end of tail not scanned yet
}

// NewMatcher returns a matcher over set (nil: no rules).
func NewMatcher(set *Set) *Matcher {
	return &Matcher{set: set, last: map[string]int{}, fired: map[string]bool{}}
}

// Facts returns the session facts, for RuntimeVerifies and the backstops.
func (m *Matcher) Facts() *Facts { return &m.facts }

// StartRequest begins a model request: the streamed text starts empty and
// each rule fires at most once per request.
func (m *Matcher) StartRequest() {
	m.request++
	m.tail, m.pending = m.tail[:0], 0
	m.fired = map[string]bool{}
}

// Text feeds one streamed text delta and returns the text rules it fires.
// Scans are batched (scanEvery, or a newline); call Flush when the stream
// ends so the last batch is scanned too.
func (m *Matcher) Text(delta string) []Hit {
	if m.set.Len() == 0 || delta == "" {
		return nil
	}
	m.tail = append(m.tail, delta...)
	m.pending += len(delta)
	if m.pending < scanEvery && !strings.Contains(delta, "\n") {
		return nil
	}
	return m.scan()
}

// Flush scans text the batching held back. Call it once the stream ends.
func (m *Matcher) Flush() []Hit {
	if m.set.Len() == 0 || m.pending == 0 {
		return nil
	}
	return m.scan()
}

// scan runs the text rules over the scanned context and the pending bytes,
// then keeps the last scanBack bytes as context for the next scan.
func (m *Matcher) scan() []Hit {
	window := string(m.tail)
	kept := keepTail(window, scanBack)
	m.tail, m.pending = append(m.tail[:0], kept...), 0
	var hits []Hit
	for _, r := range m.set.Rules {
		for _, sc := range r.Scopes {
			if sc.Kind != ScopeText {
				continue
			}
			if loc := r.Condition.FindStringIndex(window); loc != nil {
				if h, ok := m.fire(r, Hit{Rule: r, Scope: sc, Text: window[loc[0]:loc[1]]}); ok {
					hits = append(hits, h)
				}
			}
		}
	}
	return hits
}

// Calls checks a turn's complete tool calls and returns the tool_args
// rules they fire.
func (m *Matcher) Calls(calls []Call) []Hit {
	if m.set.Len() == 0 {
		return nil
	}
	var hits []Hit
	for i := range calls {
		c := &calls[i]
		for _, r := range m.set.Rules {
			for _, sc := range r.Scopes {
				if sc.Kind != ScopeToolArgs || sc.Tool != c.Name {
					continue
				}
				for _, val := range FieldValues(c.Input, sc.Field) {
					if loc := r.Condition.FindStringIndex(val); loc != nil {
						if h, ok := m.fire(r, Hit{Rule: r, Scope: sc, Text: val[loc[0]:loc[1]], Call: c, Value: val}); ok {
							hits = append(hits, h)
						}
					}
				}
			}
		}
	}
	return hits
}

// ToolResult records a call's outcome for the facts.
func (m *Matcher) ToolResult(name string, isError bool) {
	if isError {
		return
	}
	m.facts.seq++
	switch name {
	case "generate_speech":
		m.facts.TTSRan = true
	case "write_file", "patch_file", "splice_file":
		m.facts.editSeq = m.facts.seq
	case "bash":
		m.facts.checkSeq = m.facts.seq
	}
}

// fire applies the guard and the repeat policy, and records the fire.
func (m *Matcher) fire(r *Rule, h Hit) (Hit, bool) {
	if m.fired[r.Name] {
		return Hit{}, false
	}
	if last, ok := m.last[r.Name]; ok {
		if r.Once || m.request-last < r.Gap {
			return Hit{}, false
		}
	}
	if r.guard != nil && !r.guard(&m.facts, h) {
		return Hit{}, false
	}
	m.fired[r.Name] = true
	m.last[r.Name] = m.request
	return h, true
}

// FieldValues returns the values of a tool_args field. A dotted field walks
// into objects, and an array along the way fans out to each element, so
// "edits.new_string" is every patch_file edit's new_string (2.0 W4).
func FieldValues(input map[string]any, field string) []string {
	var out []string
	var walk func(v any, path []string)
	walk = func(v any, path []string) {
		if arr, ok := v.([]any); ok && len(path) > 0 {
			for _, el := range arr {
				walk(el, path)
			}
			return
		}
		if len(path) == 0 {
			if s, ok := fieldText(v); ok {
				out = append(out, s)
			}
			return
		}
		if m, ok := v.(map[string]any); ok {
			walk(m[path[0]], path[1:])
		}
	}
	walk(input, strings.Split(field, "."))
	return out
}

func fieldText(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	default:
		b, err := json.Marshal(t)
		return string(b), err == nil
	}
}

// keepTail returns at most n trailing bytes of s, starting on a rune.
func keepTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return s[i:]
}
