// Package compact keeps a conversation inside the model's context window
// (#174). This file is the first rung of the ladder: prune old tool results,
// either because a later call superseded them or because they are the
// oldest, largest thing in the history. Pruned bodies are spilled to disk
// and the model can get any of them back with recall_tool_result.
package compact

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

const (
	// reserveTokens is the minimum room kept free for the next request and
	// its reply; the reserve grows to 10% of the window on large windows.
	reserveTokens = 16_000
	// protectTokens is how much of the newest history is never pruned.
	protectTokens = 40_000
	// minSavingsTokens is the least a proactive prune must save. Pruning
	// changes the prompt prefix and costs the prompt cache, so small wins
	// aren't worth it.
	minSavingsTokens = 20_000
	// minElideTokens is the smallest tool result worth eliding.
	minElideTokens = 200
	// perMessageOverhead approximates role and framing tokens per message.
	perMessageOverhead = 4
)

// EstimateTokens approximates the tokens a message costs: its text, tool-call
// arguments and a small per-message overhead (about 4 characters per token).
func EstimateTokens(msg tui.ChatMessage) int {
	n := len(msg.Content)
	for _, tc := range msg.ToolCalls {
		n += len(tc.Name) + len(tc.Arguments)
	}
	return n/4 + perMessageOverhead
}

// Estimate approximates the tokens a history costs.
func Estimate(msgs []tui.ChatMessage) int {
	total := 0
	for _, m := range msgs {
		total += EstimateTokens(m)
	}
	return total
}

// Threshold is the usage above which compaction runs: the window minus a
// reserve of max(16k, 10% of the window). Small windows keep at least half.
func Threshold(window int) int {
	reserve := window / 10
	if reserve < reserveTokens {
		reserve = reserveTokens
	}
	t := window - reserve
	if t < window/2 {
		t = window / 2
	}
	return t
}

// Options controls a prune.
type Options struct {
	// Window is the model's context window in tokens.
	Window int
	// Used is the best known current usage (system prompt, tools and
	// history). When zero, the history estimate is used.
	Used int
	// Force prunes even below the threshold and below the minimum saving:
	// the reactive path after a context-overflow error, and /context compact.
	Force bool
	// Score, when set, rates how likely each elision candidate is still
	// needed (0..1); the least-needed are elided first. A nil or empty
	// result keeps the default oldest-first order (#175).
	Score func([]Candidate) map[string]float64
}

// Candidate is an old tool result that pass 2 may elide, oldest first.
type Candidate struct {
	ToolCallID string
	Name       string
	Args       map[string]any
	Content    string
	Tokens     int
}

// Edit replaces one tool result's content.
type Edit struct {
	ToolCallID string
	Name       string
	Content    string // replacement shown to the model
	Original   string // full body, for spilling
	Tokens     int    // estimated tokens saved
	Superseded bool
}

// Result describes a prune.
type Result struct {
	Edits       []Edit
	SavedTokens int
	Superseded  int
	Elided      int
}

// Pruned reports whether the prune changed anything.
func (r Result) Pruned() bool { return len(r.Edits) > 0 }

// Summary is a one-line description for logs and the UI.
func (r Result) Summary() string {
	return fmt.Sprintf("pruned %d old tool results (%d superseded, %d elided), ~%d tokens freed",
		len(r.Edits), r.Superseded, r.Elided, r.SavedTokens)
}

type callInfo struct {
	name string
	args map[string]any
}

// Plan works out which tool results to prune; it doesn't change msgs.
//
// Superseded results go first and need no model call: a file read again or
// edited later, or a read-only command run again with the same arguments.
// Then, if still over target, large results are elided: oldest first, or
// least-needed first when Options.Score rates them. The
// newest ~40k tokens are never touched, and a result is never removed, only
// replaced, so every tool call keeps its result.
func Plan(msgs []tui.ChatMessage, opts Options) Result {
	if opts.Window <= 0 {
		return Result{}
	}
	used := opts.Used
	if used <= 0 {
		used = Estimate(msgs)
	}
	threshold := Threshold(opts.Window)
	if !opts.Force && used <= threshold {
		return Result{}
	}
	// Aim well under the threshold so the next few turns don't prune again.
	target := threshold - opts.Window/10
	needed := used - target

	protect := protectTokens
	if q := opts.Window / 4; q < protect {
		protect = q
	}
	if opts.Force {
		protect /= 2
	}
	boundary := protectedBoundary(msgs, protect)

	calls := indexCalls(msgs)
	var res Result
	pruned := map[int]bool{}

	// Pass 1: superseded results.
	for i := 0; i < boundary; i++ {
		m := msgs[i]
		if m.Role != "tool" || isElided(m.Content) {
			continue
		}
		c, ok := calls[m.ToolCallID]
		if !ok || !supersededLater(msgs, calls, i, c) {
			continue
		}
		tokens := EstimateTokens(m) - perMessageOverhead
		if tokens < 20 {
			continue
		}
		res.Edits = append(res.Edits, Edit{
			ToolCallID: m.ToolCallID, Name: m.Name, Original: m.Content, Tokens: tokens, Superseded: true,
			Content: fmt.Sprintf("[superseded: a later %s call returned newer output. This result (~%d tokens) was removed to save context; recall_tool_result with id %q restores it.]", c.name, tokens, m.ToolCallID),
		})
		res.SavedTokens += tokens
		res.Superseded++
		pruned[i] = true
	}

	// Pass 2: oldest large results until the target is met.
	type cand struct{ idx, tokens int }
	var cands []cand
	for i := 0; i < boundary; i++ {
		m := msgs[i]
		if m.Role != "tool" || pruned[i] || isElided(m.Content) {
			continue
		}
		if t := EstimateTokens(m) - perMessageOverhead; t >= minElideTokens {
			cands = append(cands, cand{i, t})
		}
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].idx < cands[b].idx })
	// Skip the scorer (a third-party call) when supersession already met the target.
	if opts.Score != nil && len(cands) > 0 && (opts.Force || res.SavedTokens < needed) {
		in := make([]Candidate, len(cands))
		for k, c := range cands {
			m := msgs[c.idx]
			in[k] = Candidate{ToolCallID: m.ToolCallID, Name: m.Name, Args: calls[m.ToolCallID].args, Content: m.Content, Tokens: c.tokens}
		}
		if scores := opts.Score(in); len(scores) > 0 {
			need := func(c cand) float64 {
				if p, ok := scores[msgs[c.idx].ToolCallID]; ok {
					return p
				}
				return 0.5 // ponytail: unrated sits mid-pack; revisit if scorers skip results often
			}
			// Stable: equal scores stay oldest-first.
			sort.SliceStable(cands, func(a, b int) bool { return need(cands[a]) < need(cands[b]) })
		}
	}
	for _, c := range cands {
		if res.SavedTokens >= needed && !opts.Force {
			break
		}
		m := msgs[c.idx]
		res.Edits = append(res.Edits, Edit{
			ToolCallID: m.ToolCallID, Name: m.Name, Original: m.Content, Tokens: c.tokens,
			Content: fmt.Sprintf("[%s result elided to save context (~%d tokens); recall_tool_result with id %q restores it.]", m.Name, c.tokens, m.ToolCallID),
		})
		res.SavedTokens += c.tokens
		res.Elided++
	}

	if !opts.Force && res.SavedTokens < minSavingsTokens && res.SavedTokens < needed {
		// Not worth breaking the prompt cache for.
		return Result{}
	}
	// The replacement text costs a little.
	for _, e := range res.Edits {
		res.SavedTokens -= len(e.Content) / 4
	}
	return res
}

// Apply returns a copy of msgs with the edits applied.
func Apply(msgs []tui.ChatMessage, edits []Edit) []tui.ChatMessage {
	if len(edits) == 0 {
		return msgs
	}
	byID := make(map[string]string, len(edits))
	for _, e := range edits {
		byID[e.ToolCallID] = e.Content
	}
	out := make([]tui.ChatMessage, len(msgs))
	copy(out, msgs)
	for i := range out {
		if out[i].Role != "tool" {
			continue
		}
		if c, ok := byID[out[i].ToolCallID]; ok {
			out[i].Content = c
			out[i].Metadata = nil // drop inline image data with the body
		}
	}
	return out
}

// protectedBoundary returns the index where the protected tail starts: the
// newest messages worth protect tokens. It never splits an assistant turn
// from its tool results, because only tool results are ever edited.
func protectedBoundary(msgs []tui.ChatMessage, protect int) int {
	total := 0
	for i := len(msgs) - 1; i >= 0; i-- {
		total += EstimateTokens(msgs[i])
		if total > protect {
			return i + 1
		}
	}
	return 0
}

func indexCalls(msgs []tui.ChatMessage) map[string]callInfo {
	calls := map[string]callInfo{}
	for _, m := range msgs {
		for _, tc := range m.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Arguments), &args)
			calls[tc.ID] = callInfo{name: tc.Name, args: args}
		}
	}
	return calls
}

// rereadable tools return the current state of something, so a later
// identical call makes an earlier result stale.
var rereadable = map[string]bool{
	"read_file": true, "list_files": true, "search": true, "bash": true,
	"git_status": true, "git_log": true,
}

// editTools change a file; the paths they touch.
func editedPaths(c callInfo) []string {
	switch c.name {
	case "write_file", "patch_file":
		return []string{argPath(c.args, "path")}
	case "splice_file":
		return []string{argPath(c.args, "source"), argPath(c.args, "dest")}
	}
	return nil
}

func argPath(args map[string]any, key string) string {
	s, _ := args[key].(string)
	if s == "" {
		return ""
	}
	return filepath.Clean(s)
}

// supersededLater reports whether a call after msgs[i] makes its result stale.
func supersededLater(msgs []tui.ChatMessage, calls map[string]callInfo, i int, c callInfo) bool {
	readPath := ""
	if c.name == "read_file" {
		readPath = argPath(c.args, "path")
	}
	for j := i + 1; j < len(msgs); j++ {
		for _, tc := range msgs[j].ToolCalls {
			later := calls[tc.ID]
			if rereadable[c.name] && later.name == c.name && sameArgs(later.args, c.args) {
				return true
			}
			if readPath == "" {
				continue
			}
			for _, p := range editedPaths(later) {
				if p != "" && p == readPath {
					return true
				}
			}
		}
	}
	return false
}

func sameArgs(a, b map[string]any) bool {
	ja, err1 := json.Marshal(a)
	jb, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && string(ja) == string(jb)
}

// IsElided reports whether content is a prune placeholder, so the same
// result isn't pruned twice.
func isElided(content string) bool {
	return strings.HasPrefix(content, "[") && strings.Contains(content, "recall_tool_result with id")
}

// Prune plans a prune, spills the bodies to store, and returns the history
// with the pruned results replaced. With a nil store nothing is pruned: a
// placeholder must never point at a body that wasn't saved.
func Prune(msgs []tui.ChatMessage, opts Options, store *Store) ([]tui.ChatMessage, Result) {
	if store == nil {
		return msgs, Result{}
	}
	plan := Plan(msgs, opts)
	if !plan.Pruned() {
		return msgs, plan
	}
	kept := store.Commit(plan.Edits)
	res := Result{Edits: kept}
	for _, e := range kept {
		res.SavedTokens += e.Tokens - len(e.Content)/4
		if e.Superseded {
			res.Superseded++
		} else {
			res.Elided++
		}
	}
	return Apply(msgs, kept), res
}
