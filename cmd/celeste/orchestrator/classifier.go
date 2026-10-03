package orchestrator

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/decide"
)

// laneKeywords maps keyword → lane. First match wins.
var laneKeywords = []struct {
	keywords []string
	lane     TaskLane
}{
	{[]string{"fix", "refactor", "debug", "test", "build", "compile", "implement", "lint", "patch", "bug", "script", "bash", "create", "code", "function", "program", "deploy", "automate", "python", "golang", "javascript", "typescript", "rust", ".sh", ".py", ".go", ".js", ".ts", "class", "struct", "method", "api", "endpoint", "write a script", "write a function", "write a program", "write code"}, LaneCode},
	{[]string{"draft", "blog", "docs", "document", "summarize", "explain", "describe", "article", "essay", "write a blog", "write a doc", "write an article"}, LaneContent},
	{[]string{"upscale", "image", "video", "render", "convert", "generate image", "generate video", "media"}, LaneMedia},
	{[]string{"review", "audit", "check", "critique", "blind review", "code review"}, LaneReview},
	{[]string{"research", "search", "compare", "what is", "how does", "investigate", "explore", "look up"}, LaneResearch},
}

// keywordPatterns are laneKeywords compiled once: whole words (2.0 W3), so
// "rust" no longer matches "trust" nor "api" "capital". A keyword may take
// a plural or verb ending (tests, fixed, rendering). A ".ext" keyword
// matches a file extension (auth_test.go).
var keywordPatterns = func() [][]*regexp.Regexp {
	out := make([][]*regexp.Regexp, len(laneKeywords))
	for i, e := range laneKeywords {
		for _, kw := range e.keywords {
			var re string
			if strings.HasPrefix(kw, ".") {
				re = `\w` + regexp.QuoteMeta(kw) + `\b`
			} else {
				re = `\b` + regexp.QuoteMeta(kw) + `(s|es|ed|d|ing)?\b`
			}
			out[i] = append(out[i], regexp.MustCompile(re))
		}
	}
	return out
}()

// ClassifyHeuristic returns the best-guess TaskLane and a confidence score (0.0–1.0)
// based purely on keyword matching. Confidence < 0.5 means the goal is ambiguous.
func ClassifyHeuristic(goal string) (TaskLane, float64) {
	lower := strings.ToLower(goal)

	best := LaneUnknown
	bestScore := 0.0

	for i, entry := range laneKeywords {
		score := 0.0
		for _, re := range keywordPatterns[i] {
			if re.MatchString(lower) {
				score += 1.0 / float64(len(entry.keywords))
			}
		}
		if score > bestScore {
			bestScore = score
			best = entry.lane
		}
	}

	// Normalise: max possible score per lane is 1.0; scale to 0.5–0.95 range.
	if best == LaneUnknown {
		return LaneUnknown, 0.1
	}
	confidence := 0.5 + bestScore*0.45
	if confidence > 0.95 {
		confidence = 0.95
	}
	return best, confidence
}

// laneOptions describe each lane for the choice question.
var laneOptions = map[string]string{
	string(LaneCode):     "Writing, fixing, testing or building software",
	string(LaneContent):  "Writing prose: posts, docs, summaries, explanations",
	string(LaneMedia):    "Images, video or audio: generating, converting, rendering",
	string(LaneReview):   "Reviewing, auditing or critiquing existing work",
	string(LaneResearch): "Finding, comparing or investigating information",
}

// routeQuestion is jev_route's choice, with the keyword heuristic as its
// fallback (Guarded uses it on any error).
func routeQuestion() decide.Question {
	return decide.Question{
		ID: "lane", Kind: decide.Choice, Text: "Which kind of work does `text` ask for?", Options: laneOptions,
		Heuristic: func(st decide.State) (decide.Answer, bool) {
			lane, conf := ClassifyHeuristic(st.Text)
			if lane == LaneUnknown {
				return decide.Answer{}, false
			}
			return decide.Answer{Choice: string(lane), Confidence: conf}, true
		},
	}
}

// routeOracle is Jev for routing; tests replace it. Paths in the goal are
// sent relative to the working directory (the lanes' workspace), or as
// <path>.
var routeOracle = func(logf func(string)) decide.Oracle {
	ws, _ := os.Getwd()
	return decide.NewJev("route", ws, logf)
}

// Classify picks the goal's lane (2.0 W3, jev_route). "on": a Jev choice
// question decides, falling back to the keyword heuristic on any error;
// "shadow": the heuristic decides and note says what Jev would pick; off:
// the heuristic alone. note is "" when there is nothing to add.
func Classify(ctx context.Context, goal, mode string, logf func(string)) (lane TaskLane, confidence float64, note string) {
	lane, confidence = ClassifyHeuristic(goal)
	if mode != config.ModeShadow && mode != config.ModeOn {
		return lane, confidence, ""
	}
	ans, _ := routeOracle(logf).Ask(ctx, decide.State{Text: goal}.String(), []decide.Question{routeQuestion()})
	a, ok := ans["lane"]
	if !ok || a.Source != "jev" {
		return lane, confidence, ""
	}
	jevLane := TaskLane(a.Choice)
	if mode == config.ModeShadow {
		if jevLane != lane {
			note = fmt.Sprintf("jev_route (shadow): Jev would pick %s (%s)", jevLane, probs(a.Probs))
		}
		return lane, confidence, note
	}
	return jevLane, a.Confidence, fmt.Sprintf("jev_route: %s (%s)", jevLane, probs(a.Probs))
}

func probs(p map[string]float64) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %.2f", k, p[k]))
	}
	return strings.Join(parts, ", ")
}
