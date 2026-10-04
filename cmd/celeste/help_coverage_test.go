package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// runDispatchCases returns the string literals of each case clause of the
// `switch command` in run() (app_run.go).
func runDispatchCases(t *testing.T) [][]string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "app_run.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var clauses [][]string
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "run" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			if id, ok := sw.Tag.(*ast.Ident); !ok || id.Name != "command" {
				return true
			}
			for _, stmt := range sw.Body.List {
				var names []string
				for _, e := range stmt.(*ast.CaseClause).List {
					if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
						s, _ := strconv.Unquote(lit.Value)
						names = append(names, s)
					}
				}
				if len(names) > 0 {
					clauses = append(clauses, names)
				}
			}
			return false
		})
	}
	if len(clauses) == 0 {
		t.Fatal("no `switch command` found in run()")
	}
	return clauses
}

// usageSection returns the lines of usageText under heading up to the next
// blank line.
func usageSection(t *testing.T, heading string) string {
	t.Helper()
	_, rest, ok := strings.Cut(usageText, "\n"+heading+"\n")
	if !ok {
		t.Fatalf("celeste help has no %q section", heading)
	}
	section, _, _ := strings.Cut(rest, "\n\n")
	return section
}

// `celeste help` lists every top-level command run() dispatches (#313).
func TestUsageListsEveryDispatchedCommand(t *testing.T) {
	cmds := usageSection(t, "Commands:")
	for _, names := range runDispatchCases(t) {
		if !regexp.MustCompile(`(?m)^  ` + regexp.QuoteMeta(names[0]) + `\b`).MatchString(cmds) {
			t.Errorf("celeste help's Commands section lacks %q", names[0])
		}
		// Aliases are named too; -h/--help and -v/--version are flags.
		for _, alias := range names[1:] {
			if !strings.HasPrefix(alias, "-") && !regexp.MustCompile(`\b`+regexp.QuoteMeta(alias)+`\b`).MatchString(cmds) {
				t.Errorf("celeste help's Commands section lacks the alias %q", alias)
			}
		}
	}
}

// `celeste help` lists every slash command the TUI knows (#313).
func TestUsageListsEveryInteractiveCommand(t *testing.T) {
	interactive := usageSection(t, "Interactive Commands (in chat mode):")
	for _, name := range tui.KnownCommands() {
		if !regexp.MustCompile(`/` + regexp.QuoteMeta(name) + `\b`).MatchString(interactive) {
			t.Errorf("celeste help's Interactive Commands lacks /%s", name)
		}
	}
}
