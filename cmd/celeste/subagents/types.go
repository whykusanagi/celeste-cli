package subagents

import (
	"fmt"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// Type is a subagent type (2.0 W4e): it picks the subagent's tools, model
// and whether its persona is composed.
type Type string

const (
	// TypeExplore investigates: read-only tools, no persona, the small model.
	TypeExplore Type = "explore"
	// TypeGeneral does the work: every tool, the persona, the agent model.
	TypeGeneral Type = "general"
	// TypeReview reviews code: read and code-graph tools, no persona, the
	// agent model.
	TypeReview Type = "review"
)

// Profile is what a Type sets on a subagent's run.
type Profile struct {
	Allow       func(tools.Tool) bool // nil: every tool
	SkipPersona bool
	Model       string
}

// ParseType reads spawn_agent's type argument. An empty one is general.
func ParseType(s string) (Type, error) {
	switch t := Type(strings.ToLower(strings.TrimSpace(s))); t {
	case "":
		return TypeGeneral, nil
	case TypeExplore, TypeGeneral, TypeReview:
		return t, nil
	}
	return "", fmt.Errorf("unknown subagent type %q: use explore, general or review", s)
}

// reviewTools are the review type's tools besides every code_* tool.
var reviewTools = map[string]bool{
	"read_file": true, "list_files": true, "search": true,
	"git_status": true, "git_log": true, submitResultName: true,
}

// exploreDenied are tools explore never gets even if they claim to be
// read-only. bash, todo and save_memory are not read-only anyway; they are
// listed so the set reads plainly.
var exploreDenied = map[string]bool{
	"spawn_agent": true, "post_message": true, "bash": true, "todo": true, "save_memory": true,
}

// profileFor returns t's profile. An unknown or empty type is general.
func profileFor(t Type, cfg *config.Config) Profile {
	switch t {
	case TypeExplore:
		return Profile{SkipPersona: true, Model: cfg.ResolveSmallModel(), Allow: func(tl tools.Tool) bool {
			if tl.Name() == submitResultName {
				return true
			}
			if exploreDenied[tl.Name()] {
				return false
			}
			// MCP tools are never read-only, so no MCP tool passes.
			return tl.IsReadOnly()
		}}
	case TypeReview:
		return Profile{SkipPersona: true, Model: cfg.ResolveAgentModel(), Allow: func(tl tools.Tool) bool {
			return reviewTools[tl.Name()] || strings.HasPrefix(tl.Name(), "code_")
		}}
	}
	return Profile{Model: cfg.ResolveAgentModel()}
}
