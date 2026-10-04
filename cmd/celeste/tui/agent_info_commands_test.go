package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// isAgentInfoCommand must name exactly the words RunAgentCommand (in the
// main package's tui_agent.go) answers without running the agent: every
// case of its `switch sub` except resume, which runs it again. A new info
// command added there but not here would bring back the stale
// "Agent running: <word>" line.
func TestAgentInfoCommandsMatchRunAgentCommand(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "../tui_agent.go", nil, 0)
	require.NoError(t, err)

	var info, runs []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "RunAgentCommand" || fn.Recv == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			if id, ok := sw.Tag.(*ast.Ident); !ok || id.Name != "sub" {
				return true
			}
			for _, stmt := range sw.Body.List {
				clause := stmt.(*ast.CaseClause)
				var words []string
				for _, e := range clause.List {
					lit, ok := e.(*ast.BasicLit)
					require.True(t, ok, "non-literal case in RunAgentCommand")
					w, err := strconv.Unquote(lit.Value)
					require.NoError(t, err)
					words = append(words, w)
				}
				isResume := false
				for _, w := range words {
					if strings.Contains(w, "resume") {
						isResume = true
					}
				}
				if isResume {
					runs = append(runs, words...)
				} else {
					info = append(info, words...)
				}
			}
			return false
		})
	}
	require.NotEmpty(t, info, "found no `switch sub` in RunAgentCommand")
	require.NotEmpty(t, runs, "found no resume case in RunAgentCommand")

	for _, w := range info {
		assert.True(t, isAgentInfoCommand(w), "RunAgentCommand answers %q without running, but isAgentInfoCommand does not list it", w)
	}
	for _, w := range runs {
		assert.False(t, isAgentInfoCommand(w), "%q runs the agent, but isAgentInfoCommand hides its banner", w)
	}
	// The reverse: every word isAgentInfoCommand lists is one
	// RunAgentCommand answers without running (not a goal).
	app, err := parser.ParseFile(fset, "app.go", nil, 0)
	require.NoError(t, err)
	var listed []string
	for _, decl := range app.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "isAgentInfoCommand" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if lit, ok := n.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				w, err := strconv.Unquote(lit.Value)
				require.NoError(t, err)
				listed = append(listed, w)
			}
			return true
		})
	}
	require.NotEmpty(t, listed, "found no words in isAgentInfoCommand")
	for _, w := range listed {
		assert.Contains(t, info, w, "isAgentInfoCommand lists %q, which RunAgentCommand runs as a goal", w)
	}
}
