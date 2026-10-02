package compact

import (
	"fmt"
	"sort"
	"strings"
)

// Todo is one todo-list item as the state block shows it.
type Todo struct {
	ID     int
	Title  string
	Status string // pending, in_progress, done
}

const stateHeading = "## Authoritative state"

// RenderState renders what the model must not lose to a summary, from
// celeste's own records (#200): the todo list, the files this session
// changed, and the voice rule. Deterministic: todos by ID, files sorted and
// de-duplicated, fixed headings, the same bytes for the same state. Empty
// sections are left out; no state at all renders "".
func RenderState(todos []Todo, files []string, voiceRule string) string {
	voiceRule = strings.TrimSpace(voiceRule)
	files = sortedUnique(files)
	if len(todos) == 0 && len(files) == 0 && voiceRule == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(stateHeading + "\n")
	b.WriteString("From celeste's own records; where the summary above disagrees, this is correct.\n")
	if len(todos) > 0 {
		sorted := append([]Todo(nil), todos...)
		sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
		b.WriteString("\n### Todo list\n")
		for _, t := range sorted {
			mark, label := "[ ]", t.Status
			switch t.Status {
			case "done":
				mark = "[x]"
			case "in_progress":
				mark, label = "[~]", "in progress"
			}
			fmt.Fprintf(&b, "- %s %d. %s (%s)\n", mark, t.ID, strings.TrimSpace(t.Title), label)
		}
	}
	if len(files) > 0 {
		b.WriteString("\n### Files modified this session\n")
		for _, f := range files {
			fmt.Fprintf(&b, "- %s\n", f)
		}
	}
	if voiceRule != "" {
		b.WriteString("\n### Voice rule\n")
		b.WriteString(voiceRule + "\n")
	}
	return b.String()
}

func sortedUnique(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := append([]string(nil), in...)
	sort.Strings(out)
	n := 0
	for i, s := range out {
		if s == "" || (i > 0 && s == out[i-1]) {
			continue
		}
		out[n] = s
		n++
	}
	return out[:n]
}
