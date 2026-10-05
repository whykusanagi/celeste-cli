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
	// midLine: tail starts inside a line, because keepFrom cut the reply
	// there. A scan then prefixes a non-newline byte so (?m)^ does not
	// take the cut for a line start (#330 review I1).
	midLine bool
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
	m.tail, m.pending, m.midLine = m.tail[:0], 0, false
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
	return m.scan(false)
}

// Flush scans text the batching held back. Call it once the stream ends.
func (m *Matcher) Flush() []Hit {
	if m.set.Len() == 0 || m.pending == 0 {
		return nil
	}
	return m.scan(true)
}

// scan runs the text rules over the scanned context and the pending bytes,
// then keeps the last scanBack bytes as context for the next scan. Unless
// final (Flush), a word still streaming at the end of the batch is held
// back for the next scan, so TASK_COMPLETE followed by a D in the next
// delta is judged as TASK_COMPLETED, not as TASK_COMPLETE\b (#330 review I2).
func (m *Matcher) scan(final bool) []Hit {
	text := string(m.tail)
	held := ""
	if !final {
		switch w := trailingWord(text); {
		case w > scanBack:
			// A run longer than the context is no marker: scan it all.
		case w == len(text):
			return nil // one word so far: wait for its end
		default:
			text, held = text[:len(text)-w], text[len(text)-w:]
		}
	}
	window := text
	if m.midLine {
		window = "\x00" + text
	}
	cut := keepFrom(text, scanBack)
	if cut > 0 {
		m.midLine = text[cut-1] != '\n'
	}
	m.tail = append(append(m.tail[:0], text[cut:]...), held...)
	m.pending = len(held)
	var hits []Hit
	for _, r := range m.set.Rules {
		for _, sc := range r.Scopes {
			if sc.Kind != ScopeText {
				continue
			}
			if loc := r.Condition.FindStringIndex(window); loc != nil {
				hit := strings.TrimPrefix(window[loc[0]:loc[1]], "\x00")
				if h, ok := m.fire(r, Hit{Rule: r, Scope: sc, Text: hit}); ok {
					hits = append(hits, h)
				}
			}
		}
	}
	return hits
}

// trailingWord returns the length of the run of word bytes
// ([A-Za-z0-9_]) that ends s.
func trailingWord(s string) int {
	n := 0
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c != '_' && (c < '0' || c > '9') && (c < 'A' || c > 'Z') && (c < 'a' || c > 'z') {
			break
		}
		n++
	}
	return n
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
	switch {
	case name == "generate_speech":
		m.facts.TTSRan = true
	case IsEdit(name):
		m.facts.editSeq = m.facts.seq
	case IsCheck(name):
		m.facts.checkSeq = m.facts.seq
	}
}

// IsEdit reports a tool that changes a file. An edit with no successful
// check after it leaves the work unverified (task-complete-before-verify,
// and the watchdog's unverified heuristic).
func IsEdit(tool string) bool {
	switch tool {
	case "write_file", "patch_file", "splice_file":
		return true
	}
	return false
}

// IsCheck reports a tool whose successful run counts as checking the work
// (bash: a build, a test run).
func IsCheck(tool string) bool { return tool == "bash" }

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
	if r.wholeValue && h.Call != nil {
		h.Text, _ = fieldText(h.Call.Input[h.Scope.Field])
	}
	m.fired[r.Name] = true
	m.last[r.Name] = m.request
	return h, true
}

// FieldValues returns the values of a tool_args field. A dotted field walks
// into objects, and an array along the way fans out to each element, so
// "edits.new_string" is every patch_file edit's new_string (2.0 W4). A
// string met before the path ends is decoded as JSON.
func FieldValues(input map[string]any, field string) []string {
	var out []string
	var walk func(v any, path []string)
	walk = func(v any, path []string) {
		if str, ok := v.(string); ok && len(path) > 0 {
			// A nested argument sent as JSON text (patch_file accepts
			// edits[] that way) is read as what it encodes.
			var parsed any
			if json.Unmarshal([]byte(str), &parsed) == nil {
				walk(parsed, path)
			}
			return
		}
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

// keepFrom returns the offset in s from which at most n trailing bytes
// remain, moved forward to a rune start.
func keepFrom(s string, n int) int {
	if len(s) <= n {
		return 0
	}
	i := len(s) - n
	for i < len(s) && !utf8.RuneStart(s[i]) {
		i++
	}
	return i
}
