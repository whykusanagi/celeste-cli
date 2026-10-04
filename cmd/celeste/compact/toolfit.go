package compact

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// The tool schemas are part of the fixed prefix too, and on a small window
// they are most of it: at 8,192 the chat's 49 tools were ~9.8k tokens on
// their own, so the first request overflowed before any history (#310).
// FitTools trims them the way the persona guard (prompts.selectProfile)
// steps the persona down: only when they do not fit, to a core set that
// can still read, edit, search, run and plan, and with a one-time notice.

// CoreTools are kept on every window: the tools a coding turn cannot do
// without, plan mode's submit_plan, and find_tools, which activates any
// dropped tool again (it searches the whole registry).
var CoreTools = []string{
	"read_file", "write_file", "patch_file", "list_files", "search",
	"bash", "todo", "submit_plan", "find_tools",
}

// historyShare: the prefix may take what is left of the window after the
// reply's reserve and a quarter of the window for history.
const historyShare = 4

// maxCompactDescription caps a reduced tool's description, in bytes.
const maxCompactDescription = 160

// maxCompactParamDescription caps a reduced parameter's description.
const maxCompactParamDescription = 80

// wireBytes is what a provider's request adds around each definition
// ({"type":"function","function":...} in the OpenAI format), so a fit
// counts what is sent, not only the definition.
const wireBytes = 32

// mcpPrefix starts every MCP tool's name (mcp.ToolName).
const mcpPrefix = "mcp__"

// ToolFit is what FitTools sends.
type ToolFit struct {
	Defs    []tui.SkillDefinition // the definitions to send
	Total   int                   // the definitions offered before fitting
	Dropped int                   // Total - len(Defs)
	Reduced bool                  // the set was trimmed and shortened
}

// ToolBudget is the room for tool schemas on window when the system prompt
// is systemTokens: the window less the reply's reserve (as HistoryBudget
// keeps it), a quarter for history, and the system prompt. It can be
// negative.
func ToolBudget(window, systemTokens int) int {
	return window - replyReserve(window) - window/historyShare - systemTokens
}

// FitTools returns defs as they are when they fit ToolBudget (or the window
// is unknown). Otherwise it sends shortened copies of the core tools and of
// every tool pinned reports true for (one the model activated this
// session), whatever they cost, then the other tools in order while they
// fit, builtins before MCP tools. The order of defs is kept, so the
// definitions a provider caches do not move. defs is never modified.
func FitTools(defs []tui.SkillDefinition, window, systemTokens int, pinned func(string) bool) ToolFit {
	fit := ToolFit{Defs: defs, Total: len(defs)}
	budget := ToolBudget(window, systemTokens)
	if window <= 0 || len(defs) == 0 || DefinitionTokens(defs)+len(defs)*wireBytes/4 <= budget {
		return fit
	}
	must := func(name string) bool {
		return slices.Contains(CoreTools, name) || (pinned != nil && pinned(name))
	}
	short := compactAll(defs, maxCompactParamDescription)
	if mustBytes(defs, short, must)/4 > budget {
		// The core set does not fit even shortened: its parameters go
		// without descriptions (their names and types stay).
		short = compactAll(defs, 0)
	}
	keep := make([]bool, len(defs))
	size := 2 // the JSON array's brackets, as DefinitionTokens counts them
	add := func(i int) {
		if size > 2 {
			size++ // the comma
		}
		size += defBytes(short[i])
		keep[i] = true
	}
	for i, d := range defs {
		if must(d.Name) {
			add(i)
		}
	}
	for _, mcp := range []bool{false, true} {
		for i, d := range defs {
			if keep[i] || strings.HasPrefix(d.Name, mcpPrefix) != mcp {
				continue
			}
			if (size+1+defBytes(short[i]))/4 <= budget {
				add(i)
			}
		}
	}
	out := make([]tui.SkillDefinition, 0, len(defs))
	for i := range defs {
		if keep[i] {
			out = append(out, short[i])
		}
	}
	return ToolFit{Defs: out, Total: len(defs), Dropped: len(defs) - len(out), Reduced: true}
}

// defBytes is d's size as sent: its JSON and the provider's wrapping.
func defBytes(d tui.SkillDefinition) int {
	b, err := json.Marshal(d)
	if err != nil {
		return 0
	}
	return len(b) + wireBytes
}

// mustBytes is the size of the short definitions must keeps.
func mustBytes(defs, short []tui.SkillDefinition, must func(string) bool) int {
	n := 2
	for i, d := range defs {
		if must(d.Name) {
			n += defBytes(short[i]) + 1
		}
	}
	return n
}

// compactAll returns shortened copies of defs: each description cut to its
// first sentence, and each description inside the parameters' schema cut
// to paramLimit bytes (0: removed).
func compactAll(defs []tui.SkillDefinition, paramLimit int) []tui.SkillDefinition {
	out := make([]tui.SkillDefinition, len(defs))
	for i, d := range defs {
		out[i] = tui.SkillDefinition{
			Name:        d.Name,
			Description: shortDescription(d.Description, maxCompactDescription),
		}
		if d.Parameters != nil {
			out[i].Parameters, _ = shortenSchema(d.Parameters, paramLimit).(map[string]any)
		}
	}
	return out
}

// shortenSchema copies a JSON schema value with every "description" string
// shortened to limit bytes, or removed when limit is 0. Other values are
// copied as they are.
func shortenSchema(v any, limit int) any {
	switch t := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(t))
		for k, x := range t {
			if s, ok := x.(string); ok && k == "description" {
				if limit > 0 {
					m[k] = shortDescription(s, limit)
				}
				continue
			}
			m[k] = shortenSchema(x, limit)
		}
		return m
	case []any:
		s := make([]any, len(t))
		for i, x := range t {
			s[i] = shortenSchema(x, limit)
		}
		return s
	default:
		return v
	}
}

// shortDescription is s's first line, cut after its first sentence (a
// period followed by a space), and at most limit bytes ("..." marks a cut
// inside the sentence).
func shortDescription(s string, limit int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	if len(s) <= limit {
		return s
	}
	cut := limit - 3
	for cut > 0 && !utf8Start(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// utf8Start reports whether b starts a UTF-8 sequence (is not a
// continuation byte), so a cut never splits a character.
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

var toolFitSeen sync.Map

// ToolFitNotice is the one-time notice for a reduced fit on window: "" when
// fit was not reduced, or once the same (kept, total, window) was noticed
// in this process. Like the persona guard's, the caller shows it.
func ToolFitNotice(fit ToolFit, window int) string {
	if !fit.Reduced {
		return ""
	}
	if _, seen := toolFitSeen.LoadOrStore(fmt.Sprintf("%d/%d@%d", len(fit.Defs), fit.Total, window), true); seen {
		return ""
	}
	size := config.FormatTokenCount(window)
	if fit.Dropped == 0 {
		return fmt.Sprintf("Tools: the context window (%s tokens) is too small for all the tool definitions next to the system prompt as they are, so all %d tools are sent with short descriptions. If the model's window is larger, set context_limit in your config.",
			size, fit.Total)
	}
	return fmt.Sprintf("Tools: the context window (%s tokens) is too small for all the tool definitions next to the system prompt, so %d of %d tools are sent, with short descriptions; find_tools activates any of the others. If the model's window is larger, set context_limit in your config.",
		size, len(fit.Defs), fit.Total)
}
