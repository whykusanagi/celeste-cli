package tui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/commands"
)

// switchCaseNames returns, per case clause, the string literals of the first
// switch statement in file whose tag renders as tag (e.g. "cmd.Name").
func switchCaseNames(t *testing.T, file, tag string) [][]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var clauses [][]string
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || found || sw.Tag == nil || exprString(sw.Tag) != tag {
			return true
		}
		found = true
		for _, stmt := range sw.Body.List {
			cc := stmt.(*ast.CaseClause)
			var names []string
			for _, e := range cc.List {
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
	if !found {
		t.Fatalf("%s: no switch on %s", file, tag)
	}
	return clauses
}

func exprString(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return exprString(v.X) + "." + v.Sel.Name
	case *ast.CallExpr:
		return exprString(v.Fun) + "(" + func() string {
			if len(v.Args) == 1 {
				return exprString(v.Args[0])
			}
			return ""
		}() + ")"
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// Every slash command the TUI or commands.Execute dispatches is in
// knownCommands (an alias may stand in for its clause), so the help check
// below covers the real dispatch tables (#313).
func TestDispatchedCommandsAreKnown(t *testing.T) {
	tables := map[string][][]string{
		"app.go":               switchCaseNames(t, "app.go", "cmd.Name"),
		"commands/commands.go": switchCaseNames(t, "../commands/commands.go", "strings.ToLower(cmd.Name)"),
	}
	for file, clauses := range tables {
		for _, names := range clauses {
			ok := false
			for _, n := range names {
				if contains(knownCommands, n) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s dispatches /%s but knownCommands lacks it", file, names[0])
			}
		}
	}
}

// The in-chat /help lists every known slash command (#313).
func TestHelpListsEveryKnownCommand(t *testing.T) {
	res := commands.Execute(&commands.Command{Name: "help"}, &commands.CommandContext{})
	for _, name := range knownCommands {
		if !regexp.MustCompile(`/` + regexp.QuoteMeta(name) + `\b`).MatchString(res.Message) {
			t.Errorf("/help lacks /%s", name)
		}
	}
}
