package subagents

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// submitResultName is the typed result tool's name.
const submitResultName = "submit_result"

// Result limits (2.0 W4e ruling 5).
const (
	maxSummaryChars = 4000
	maxFindings     = 50
	maxFiles        = 200
)

// severities are the allowed Finding.Severity values.
var severities = map[string]bool{"info": true, "low": true, "medium": true, "high": true}

// Finding is one thing a typed subagent found.
type Finding struct {
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Severity string `json:"severity,omitempty"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
}

// Result is what a typed subagent hands its parent: {summary, findings[],
// files[]}, plus a warning when the run never called submit_result.
type Result struct {
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
	Files    []string  `json:"files"`
	Warning  string    `json:"warning,omitempty"`
}

// ValidateResult checks raw against the result schema and returns the
// result, or every problem found, each naming its field. Unknown keys are
// rejected. Findings and Files are never nil in a valid result.
func ValidateResult(raw map[string]any) (Result, []string) {
	var errs []string
	addf := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	r := Result{Findings: []Finding{}, Files: []string{}}

	for _, k := range sortedKeys(raw) {
		if k != "summary" && k != "findings" && k != "files" {
			addf("%s: unknown field (allowed: summary, findings, files)", k)
		}
	}

	switch s, ok := raw["summary"].(string); {
	case raw["summary"] == nil:
		addf("summary: required")
	case !ok:
		addf("summary: must be a string")
	case strings.TrimSpace(s) == "":
		addf("summary: must not be empty")
	case utf8.RuneCountInString(s) > maxSummaryChars:
		addf("summary: %d characters, at most %d", utf8.RuneCountInString(s), maxSummaryChars)
	default:
		r.Summary = s
	}

	if v, present := raw["findings"]; present && v != nil {
		list, ok := v.([]any)
		switch {
		case !ok:
			addf("findings: must be an array of objects {title, detail, severity, file, line}")
		case len(list) > maxFindings:
			addf("findings: %d items, at most %d", len(list), maxFindings)
		default:
			for i, item := range list {
				f, ferrs := validateFinding(fmt.Sprintf("findings[%d]", i), item)
				errs = append(errs, ferrs...)
				r.Findings = append(r.Findings, f)
			}
		}
	}

	if v, present := raw["files"]; present && v != nil {
		list, ok := v.([]any)
		switch {
		case !ok:
			addf("files: must be an array of strings")
		case len(list) > maxFiles:
			addf("files: %d items, at most %d", len(list), maxFiles)
		default:
			for i, item := range list {
				s, ok := item.(string)
				if !ok {
					addf("files[%d]: must be a string", i)
					continue
				}
				r.Files = append(r.Files, s)
			}
		}
	}

	if len(errs) == 0 {
		// The parent gets the result as JSON; keep it within what a
		// subagent may hand back at all.
		if b, err := json.Marshal(r); err == nil && len(b) > maxResultBytes {
			addf("result: %d bytes as JSON, at most %d; shorten summary and details", len(b), maxResultBytes)
		}
	}
	if len(errs) > 0 {
		return Result{}, errs
	}
	return r, nil
}

func validateFinding(path string, item any) (Finding, []string) {
	var errs []string
	addf := func(format string, a ...any) { errs = append(errs, fmt.Sprintf(format, a...)) }
	m, ok := item.(map[string]any)
	if !ok {
		return Finding{}, []string{path + ": must be an object {title, detail, severity, file, line}"}
	}
	var f Finding
	for _, k := range sortedKeys(m) {
		switch k {
		case "title", "detail", "severity", "file", "line":
		default:
			addf("%s.%s: unknown field (allowed: title, detail, severity, file, line)", path, k)
		}
	}
	str := func(key string, dst *string) {
		v, present := m[key]
		if !present || v == nil {
			return
		}
		s, ok := v.(string)
		if !ok {
			addf("%s.%s: must be a string", path, key)
			return
		}
		*dst = s
	}
	str("title", &f.Title)
	str("detail", &f.Detail)
	str("severity", &f.Severity)
	str("file", &f.File)
	if strings.TrimSpace(f.Title) == "" {
		if _, isStr := m["title"].(string); isStr || m["title"] == nil {
			addf("%s.title: required", path)
		}
	}
	if f.Severity != "" && !severities[f.Severity] {
		addf("%s.severity: %q is not one of info, low, medium, high", path, f.Severity)
	}
	if v, present := m["line"]; present && v != nil {
		n, ok := wholeNumber(v)
		switch {
		case !ok:
			addf("%s.line: must be a whole number", path)
		case n < 1:
			addf("%s.line: must be 1 or more", path)
		default:
			f.Line = n
		}
	}
	return f, errs
}

// wholeNumber reads a JSON number that is a whole number.
func wholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != math.Trunc(n) || math.IsInf(n, 0) || n > math.MaxInt32 || n < math.MinInt32 {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	case int64:
		if n > math.MaxInt32 || n < math.MinInt32 {
			return 0, false
		}
		return int(n), true
	case json.Number:
		i, err := n.Int64()
		if err != nil || i > math.MaxInt32 || i < math.MinInt32 {
			return 0, false
		}
		return int(i), true
	}
	return 0, false
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// resultHolder keeps one run's submitted result. The latest valid
// submission wins.
type resultHolder struct {
	mu sync.Mutex
	r  *Result
}

func (h *resultHolder) set(r Result) {
	h.mu.Lock()
	h.r = &r
	h.mu.Unlock()
}

// get returns a copy of the submitted result, or nil.
func (h *resultHolder) get() *Result {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.r == nil {
		return nil
	}
	c := *h.r
	return &c
}

// SubmitResultTool records a typed subagent's result (2.0 W4e). It only
// records, so it is read-only: every type keeps it and it never prompts.
type SubmitResultTool struct {
	holder *resultHolder
}

// NewSubmitResultTool returns a submit_result tool that stores into h.
func NewSubmitResultTool(h *resultHolder) *SubmitResultTool {
	return &SubmitResultTool{holder: h}
}

func (t *SubmitResultTool) Name() string { return submitResultName }

func (t *SubmitResultTool) Description() string {
	return "Hand your result to the agent that spawned you: a summary, findings (each with a title, and optionally detail, " +
		"severity info|low|medium|high, file and line) and the files involved. Call it once you are done; a later call " +
		"replaces an earlier one. Then reply with TASK_COMPLETE as the last line."
}

func (t *SubmitResultTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"additionalProperties": false,
		"properties": {
			"summary": {"type": "string", "minLength": 1, "maxLength": 4000, "description": "What you found or did, for the agent that spawned you"},
			"findings": {
				"type": "array",
				"maxItems": 50,
				"items": {
					"type": "object",
					"additionalProperties": false,
					"properties": {
						"title": {"type": "string", "minLength": 1},
						"detail": {"type": "string"},
						"severity": {"type": "string", "enum": ["info", "low", "medium", "high"]},
						"file": {"type": "string"},
						"line": {"type": "integer", "minimum": 1}
					},
					"required": ["title"]
				}
			},
			"files": {"type": "array", "maxItems": 200, "items": {"type": "string"}, "description": "Files you read, changed or refer to"}
		},
		"required": ["summary"]
	}`)
}

func (t *SubmitResultTool) IsConcurrencySafe(map[string]any) bool { return true }
func (t *SubmitResultTool) IsReadOnly() bool                      { return true }
func (t *SubmitResultTool) InterruptBehavior() tools.InterruptBehavior {
	return tools.InterruptCancel
}

// ValidateInput accepts everything: Execute validates, so the model gets
// every problem back as a tool result it can fix.
func (t *SubmitResultTool) ValidateInput(map[string]any) error { return nil }

func (t *SubmitResultTool) Execute(_ context.Context, input map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	r, errs := ValidateResult(input)
	if len(errs) > 0 {
		return tools.ToolResult{
			Content: "submit_result rejected; nothing was recorded:\n- " + strings.Join(errs, "\n- ") + "\nFix these and call submit_result again.",
			Error:   true,
		}, nil
	}
	t.holder.set(r)
	return tools.ToolResult{Content: "Result recorded. Reply with TASK_COMPLETE as the last line to finish."}, nil
}
