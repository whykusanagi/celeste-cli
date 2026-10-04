package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dispatchedSubcommands reads every case label of run()'s command switch
// from app_run.go, so a subcommand added to the dispatch without -h/--help
// handling fails TestRun_EverySubcommandHelpPrintsUsage.
func dispatchedSubcommands(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "app_run.go", nil, 0)
	require.NoError(t, err)
	var names []string
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
				for _, e := range stmt.(*ast.CaseClause).List {
					if lit, ok := e.(*ast.BasicLit); ok {
						s, _ := strconv.Unquote(lit.Value)
						names = append(names, s)
					}
				}
			}
			return false
		})
	}
	require.NotEmpty(t, names, "run()'s command switch not found")
	sort.Strings(names)
	return names
}

// helpExempt are the dispatch labels that are not subcommands taking
// arguments: help and version are themselves the answer.
var helpExempt = map[string]bool{
	"help": true, "-h": true, "--help": true,
	"version": true, "-v": true, "--version": true,
}

// W-C1: -h, --help and -help as the first argument of any subcommand print
// that subcommand's usage on stdout and exit 0, and the subcommand never
// runs: no memory saved, no index built, no session looked up.
func TestRun_EverySubcommandHelpPrintsUsage(t *testing.T) {
	for _, cmd := range dispatchedSubcommands(t) {
		if helpExempt[cmd] {
			continue
		}
		for _, flag := range []string{"-h", "--help", "-help"} {
			t.Run(cmd+" "+flag, func(t *testing.T) {
				r := &fakeRunner{}
				var out, errBuf bytes.Buffer
				code := run([]string{cmd, flag, "extra"}, r, &out, &errBuf)
				assert.Equal(t, 0, code)
				assert.Empty(t, r.lastCall, "the subcommand ran")
				assert.Empty(t, r.lastMessage, "a message was sent")
				assert.False(t, r.usageCalled, "the general usage printed instead")
				assert.Empty(t, errBuf.String())
				usage := strings.ReplaceAll(out.String(), "[-config <name>] ", "")
				want := "celeste " + canonicalSubcommand(cmd)
				assert.Contains(t, usage, "Usage:")
				assert.Contains(t, usage, want, "the usage names another subcommand")
			})
		}
	}
}

// canonicalSubcommand is the name a subcommand's usage uses for it.
func canonicalSubcommand(cmd string) string {
	switch cmd {
	case "sessions":
		return "session"
	case "msg":
		return "message"
	}
	return cmd
}

// Help is only the first argument: later words stay data, so a memory or
// a goal may mention --help.
func TestRun_HelpFlagLaterIsData(t *testing.T) {
	r := &fakeRunner{}
	var out, errBuf bytes.Buffer
	assert.Equal(t, 0, run([]string{"remember", "pass", "--help", "to", "tools"}, r, &out, &errBuf))
	assert.Equal(t, "remember", r.lastCall)
	assert.Equal(t, []string{"pass", "--help", "to", "tools"}, r.lastArgs)
	assert.Empty(t, out.String())
}

// The usage of a subcommand with a flag set lists every flag it parses, so
// the hand-written text cannot drift from the code. Hidden maintainer or
// removed flags are listed in skip.
func TestSubcommandUsage_ListsEveryFlag(t *testing.T) {
	for _, tc := range []struct {
		file, fn, cmd string
		skip          []string
	}{
		{"agent_run.go", "runAgentCommand", "agent", nil},
		{"main.go", "runServeCommand", "serve", nil},
		{"main.go", "sessionCLI", "session", nil},
		{"main.go", "runSkillsCommand", "skills", nil},
		{"main.go", "runConfigCommand", "config", []string{"set-mode", "skip-persona", "set-claw-max-iterations"}},
		{"grimoire_run.go", "initProject", "init", nil},
		{"mcp_install.go", "runMCPInstall", "mcp", nil},
		{"update_cmd.go", "runUpdateCommand", "update", []string{"verify-dist", "tag"}},
	} {
		flags := flagNamesIn(t, tc.file, tc.fn)
		require.NotEmpty(t, flags, "%s: no flags found in %s", tc.cmd, tc.fn)
		usage := subcommandUsage[tc.cmd]
		require.NotEmpty(t, usage, tc.cmd)
		skip := map[string]bool{}
		for _, s := range tc.skip {
			skip[s] = true
		}
		for _, name := range flags {
			if skip[name] {
				continue
			}
			assert.True(t, strings.Contains(usage, "--"+name+" ") || strings.Contains(usage, "--"+name+"\n") ||
				strings.Contains(usage, "--"+name+"]") || strings.Contains(usage, "--"+name+"="),
				"celeste %s usage does not list --%s", tc.cmd, name)
		}
	}
}

// flagNamesIn returns the names passed to fs.Bool/String/Int/Var calls in
// function fn of file.
func flagNamesIn(t *testing.T, file, fn string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	require.NoError(t, err)
	var names []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != fn {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			argIdx := 0
			switch sel.Sel.Name {
			case "Bool", "String", "Int":
			case "Var":
				argIdx = 1
			default:
				return true
			}
			if len(call.Args) <= argIdx {
				return true
			}
			if lit, ok := call.Args[argIdx].(*ast.BasicLit); ok && lit.Kind == token.STRING {
				s, _ := strconv.Unquote(lit.Value)
				names = append(names, s)
			}
			return true
		})
	}
	return names
}
