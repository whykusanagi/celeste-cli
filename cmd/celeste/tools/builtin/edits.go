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
// both.
func parseEdits(input map[string]any) ([]edit, error) {
	rawEdits, hasEdits := input["edits"]
	if hasEdits && rawEdits == nil {
		hasEdits = false
	}
	_, hasOld := input["old_string"]
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
// (ruling 2): the exact string, then #165's double-escape decode.
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
		return content, editOutcome{}, fmt.Errorf("old_string not found in %s", path)
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
