package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// maxEdits bounds one patch_file call's edits[] (2.0 W4 ruling 1).
const maxEdits = 50

// edit is one replacement of patch_file: Old becomes New, once or (All)
// everywhere.
type edit struct {
	Old, New string
	All      bool
}

// editOutcome is what applying one edit did.
type editOutcome struct {
	Count   int
	Decoded bool
	Fuzzy   bool
	Diff    string
}

// parseEdits reads patch_file's two shapes (ruling 1): the top-level
// old_string/new_string/replace_all, or edits[] of 1-50 such objects, never
// both. Models that fill every schema field send the shape they did not
// mean as empty values, so an empty edits[] is absent, and beside a
// non-empty edits[] so are an empty old_string and new_string.
func parseEdits(input map[string]any) ([]edit, error) {
	rawEdits, hasEdits := input["edits"]
	if hasEdits && rawEdits == nil {
		hasEdits = false
	}
	if hasEdits {
		if arr, err := editsArray(rawEdits); err == nil && len(arr) == 0 {
			hasEdits = false
		}
	}
	_, hasOld := input["old_string"]
	if hasEdits && hasOld && emptyArg(input, "old_string") && emptyArg(input, "new_string") {
		hasOld = false
	}
	switch {
	case hasEdits && hasOld:
		return nil, errors.New("use either old_string/new_string or edits[], not both")
	case hasEdits:
		raw, err := editsArray(rawEdits)
		if err != nil {
			return nil, err
		}
		if len(raw) == 0 || len(raw) > maxEdits {
			return nil, fmt.Errorf("edits must hold 1-%d edits", maxEdits)
		}
		out := make([]edit, 0, len(raw))
		for i, r := range raw {
			m, ok := r.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("edit %d: must be an object with old_string and new_string", i+1)
			}
			e, err := editFrom(m)
			if err != nil {
				return nil, fmt.Errorf("edit %d: %w", i+1, err)
			}
			out = append(out, e)
		}
		return out, nil
	case hasOld:
		e, err := editFrom(input)
		if err != nil {
			return nil, err
		}
		return []edit{e}, nil
	}
	return nil, errors.New("give old_string and new_string, or edits[]")
}

// emptyArg reports whether input[key] is missing, null or "".
func emptyArg(input map[string]any, key string) bool {
	v, ok := input[key]
	return !ok || v == nil || v == ""
}

// editsArray accepts edits[] as an array, or as the JSON text of one (some
// models send nested arrays as strings).
func editsArray(v any) ([]any, error) {
	switch t := v.(type) {
	case []any:
		return t, nil
	case string:
		var arr []any
		if err := json.Unmarshal([]byte(t), &arr); err == nil {
			return arr, nil
		}
	}
	return nil, errors.New("edits must be an array of {old_string, new_string, replace_all?} objects")
}

func editFrom(m map[string]any) (edit, error) {
	e := edit{Old: getStringArg(m, "old_string", ""), New: getStringArg(m, "new_string", ""), All: getBoolArg(m, "replace_all", false)}
	if e.Old == "" {
		return edit{}, errors.New("old_string is required")
	}
	if _, ok := m["new_string"]; !ok {
		return edit{}, errors.New("new_string is required")
	}
	return e, nil
}

// applyEdit applies one edit to content through the matching ladder
// (ruling 2): the exact string, then #165's double-escape decode, then (not
// for replace_all) the whitespace-tolerant fuzzyMatch.
func applyEdit(content string, e edit, path string) (string, editOutcome, error) {
	// Route oversized literals to the deterministic path. A patch this
	// large is almost always a byte-move (relocating existing content);
	// regenerating it as a tool argument is slow and a silent-corruption
	// vector. splice_file moves the bytes on disk without routing them
	// through the model.
	if len(e.New) > maxPatchLiteralBytes {
		return content, editOutcome{}, fmt.Errorf(
			"new_string is %d bytes (> %d KiB). Literals this large route through the model and risk silent corruption. If you are relocating existing content, use splice_file (it moves bytes on disk by anchors/line-ranges). If this is genuinely new content, split it into smaller anchored patch_file edits.",
			len(e.New), maxPatchLiteralBytes/1024)
	}
	// old_string must match the content verbatim. Only when it doesn't, and
	// its unescaped form does, was the whole call double-escaped, so
	// new_string is decoded the same way. Otherwise both are used
	// byte-for-byte: `\n` in source code is text, not a line break (#165).
	old, decoded := matchNeedle(content, e.Old)
	newText := e.New
	if decoded {
		newText = unescapeSequences(newText)
	}
	count := strings.Count(content, old)
	if count == 0 {
		// replace_all never uses the tolerant match: a fuzzy replace-all
		// would be a guess repeated (ruling 2).
		if e.All {
			return content, editOutcome{}, fmt.Errorf("old_string not found in %s", path)
		}
		return fuzzyEdit(content, e, path)
	}
	if !e.All && count > 1 {
		return content, editOutcome{}, fmt.Errorf("old_string appears %d times in %s — set replace_all:true or make it more specific", count, path)
	}
	if e.All {
		return strings.ReplaceAll(content, old, newText), editOutcome{Count: count, Decoded: decoded}, nil
	}
	return strings.Replace(content, old, newText, 1), editOutcome{Count: 1, Decoded: decoded}, nil
}

// applyEdits applies edits in order, each to the content the previous ones
// left, all or nothing (ruling 1).
func applyEdits(content string, edits []edit, path string) (string, []editOutcome, error) {
	outcomes := make([]editOutcome, len(edits))
	for i, e := range edits {
		next, out, err := applyEdit(content, e, path)
		if err != nil {
			if len(edits) > 1 {
				return "", nil, fmt.Errorf("edit %d: %w (no edit was written)", i+1, err)
			}
			return "", nil, err
		}
		content, outcomes[i] = next, out
	}
	return content, outcomes, nil
}

// fuzzyEdit is the ladder's third rung: a unique whitespace-tolerant line
// window, replaced by new_string re-indented to the file, with the hunk's
// unified diff in the outcome (rulings 3-4).
func fuzzyEdit(content string, e edit, path string) (string, editOutcome, error) {
	start, end, n := 0, 0, 0
	if strings.TrimSpace(e.Old) != "" {
		start, end, n = fuzzyMatch(content, e.Old)
	}
	if n != 1 {
		return content, editOutcome{}, fmt.Errorf("old_string not found in %s (also tried a whitespace-tolerant match: %d matches)", path, n)
	}
	matched := content[start:end]
	repl := reindent(e.New, e.Old, matched)
	// TrimSpace let LF lines of old_string match CRLF lines of the file:
	// new_string gets the file's CRLF too, so the endings are not mixed.
	eol := "\n"
	if strings.Contains(matched, "\r\n") {
		eol = "\r\n"
		repl = strings.ReplaceAll(strings.ReplaceAll(repl, "\r\n", "\n"), "\n", eol)
	}
	// The window is replaced as whole lines, its final newline kept (an
	// empty new_string deletes the lines).
	if repl != "" && !strings.HasSuffix(repl, "\n") && strings.HasSuffix(matched, "\n") {
		repl += eol
	}
	out := content[:start] + repl + content[end:]
	return out, editOutcome{Count: 1, Fuzzy: true, Diff: unifiedHunk(path, content, out, start, end, start+len(repl))}, nil
}

// fuzzyMatch finds the line windows of content equal to old line by line
// after TrimSpace (ruling 3; blank lines must stay blank). It returns the
// byte range of the window when exactly one matches, and how many matched.
func fuzzyMatch(content, old string) (start, end, n int) {
	// One final newline ends the last line; more are blank lines that
	// must match blank lines.
	oldLines := strings.Split(strings.TrimSuffix(old, "\n"), "\n")
	lines := strings.SplitAfter(content, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	offsets := make([]int, len(lines)+1)
	for i, l := range lines {
		offsets[i+1] = offsets[i] + len(l)
	}
	for i := 0; i+len(oldLines) <= len(lines); i++ {
		ok := true
		for j, ol := range oldLines {
			if strings.TrimSpace(lines[i+j]) != strings.TrimSpace(ol) {
				ok = false
				break
			}
		}
		if ok {
			n++
			start, end = offsets[i], offsets[i+len(oldLines)]
		}
	}
	if n != 1 {
		return 0, 0, n
	}
	return start, end, 1
}

func leadingWS(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

func firstNonBlank(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			return l
		}
	}
	return ""
}

// reindent moves newText from oldText's indentation to matched's, line by
// line (ruling 3): a non-blank line i indented exactly like oldText's line
// i takes the matched window's line i indentation; any other line indented
// exactly like some old line takes that line's indentation in the window;
// any other line has oldText's base indentation (its first non-blank
// line's) replaced by the window's.
func reindent(newText, oldText, matched string) string {
	oldLines := strings.Split(oldText, "\n")
	winLines := strings.Split(strings.TrimSuffix(matched, "\n"), "\n")
	from, to := leadingWS(firstNonBlank(oldText)), leadingWS(firstNonBlank(matched))
	// Indentation map over the aligned non-blank lines; the first mapping
	// of an indentation wins.
	byIndent := map[string]string{}
	for j := 0; j < len(oldLines) && j < len(winLines); j++ {
		if strings.TrimSpace(oldLines[j]) == "" {
			continue
		}
		if _, ok := byIndent[leadingWS(oldLines[j])]; !ok {
			byIndent[leadingWS(oldLines[j])] = leadingWS(winLines[j])
		}
	}
	lines := strings.Split(newText, "\n")
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		ind := leadingWS(l)
		if i < len(oldLines) && i < len(winLines) && ind == leadingWS(oldLines[i]) {
			lines[i] = leadingWS(winLines[i]) + l[len(ind):]
			continue
		}
		if mapped, ok := byIndent[ind]; ok {
			lines[i] = mapped + l[len(ind):]
			continue
		}
		if strings.HasPrefix(ind, from) {
			lines[i] = to + l[len(from):]
		}
	}
	return strings.Join(lines, "\n")
}

// unifiedHunk renders one hunk: the lines of before in [start, oldEnd)
// (byte offsets on line boundaries) became after's [start, newEnd), with up
// to 3 lines of context (ruling 4).
func unifiedHunk(path, before, after string, start, oldEnd, newEnd int) string {
	lineOf := func(s string, off int) int { return strings.Count(s[:off], "\n") }
	// lineEnd is the index of the line after the one off ends in: a range
	// ending in an unterminated last line still covers that line.
	lineEnd := func(s string, off int) int {
		n := lineOf(s, off)
		if off > 0 && s[off-1] != '\n' {
			n++
		}
		return n
	}
	split := func(s string) []string {
		l := strings.SplitAfter(s, "\n")
		if len(l) > 0 && l[len(l)-1] == "" {
			l = l[:len(l)-1]
		}
		return l
	}
	bl, al := split(before), split(after)
	first, oldLast, newLast := lineOf(before, start), lineEnd(before, oldEnd), lineEnd(after, newEnd)
	ctxStart := max(first-3, 0)
	oldCtxEnd := min(oldLast+3, len(bl))
	newCtxEnd := newLast + (oldCtxEnd - oldLast)
	var b strings.Builder
	fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n@@ -%d,%d +%d,%d @@\n", path, path, ctxStart+1, oldCtxEnd-ctxStart, ctxStart+1, newCtxEnd-ctxStart)
	write := func(prefix string, ls []string) {
		for _, l := range ls {
			b.WriteString(prefix + strings.TrimSuffix(l, "\n") + "\n")
		}
	}
	write(" ", bl[ctxStart:first])
	write("-", bl[first:oldLast])
	write("+", al[first:newLast])
	write(" ", bl[oldLast:oldCtxEnd])
	return b.String()
}
