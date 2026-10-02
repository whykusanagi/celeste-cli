package decide

import "github.com/whykusanagi/celeste-cli/cmd/celeste/jev"

// New returns the oracle the config's `oracle` key names, Guarded for use:
// "llm" asks the small model through complete, "jev" asks TypeSafe (key
// from TYPESAFE_API_KEY or ~/.celeste/typesafe.key), anything else is the
// heuristic. A missing key or small model logs once and gives the
// heuristic. workspace is the run's: paths inside it are sent
// workspace-relative, every other path as <path> (jev.RedactPaths). logf
// may be nil.
func New(mode string, complete CompleteFunc, use, workspace string, logf func(string)) Oracle {
	switch mode {
	case "llm":
		if complete != nil {
			return Guarded(LLM{Complete: complete, Workspace: workspace}, use, logf)
		}
		say(logf, "oracle llm: no small model; using the heuristic")
	case "jev":
		return NewJev(use, workspace, logf)
	}
	return Guarded(nil, use, logf)
}

// NewJev is the Jev oracle, Guarded for use, whatever `oracle` says:
// jev_gate and jev_route always ask Jev. Without a key it is the heuristic.
func NewJev(use, workspace string, logf func(string)) Oracle {
	c, err := newJevClient()
	if err != nil {
		say(logf, "jev "+use+" disabled: "+err.Error())
		return Guarded(nil, use, logf)
	}
	c.Workspace = workspace
	return Guarded(Jev{Client: c}, use, logf)
}

// newJevClient is jev.NewFromEnv; tests point it at an httptest server.
var newJevClient = jev.NewFromEnv

func say(logf func(string), s string) {
	if logf != nil {
		logf(s)
	}
}
