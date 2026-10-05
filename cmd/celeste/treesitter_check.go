package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// treeSitterFixture is a tiny TypeScript/PHP/Python repo. Every assertion in
// treeSitterSelfCheck holds for the tree-sitter parsers and fails for the
// regex fallback a CGO_ENABLED=0 build uses, so the release smoke step
// (`celeste index selfcheck`) can tell the two apart (#376).
var treeSitterFixture = map[string]string{
	"src/greeter.ts": `export class Greeter {
  greet(who: string): string {
    return formatGreeting(who);
  }
}

export function formatGreeting(who: string): string {
  return "hello " + who;
}
`,
	"src/auth.php": `<?php
namespace App\Auth;

interface Validator {
    public function verify(string $token): bool;
}

final class SessionValidator implements Validator {
    public function validate(string $token): bool {
        return $this->checkToken($token);
    }

    private function checkToken(string $t): bool {
        return strlen($t) > 0;
    }
}
`,
	"src/jobs.py": `from abc import ABC, abstractmethod


class BaseJob(ABC):
    @abstractmethod
    def perform(self):
        ...


def load_jobs():
    return []


def run_jobs():
    return load_jobs()
`,
}

// treeSitterSelfCheck writes the fixture under dir, indexes it with a
// throwaway database there, and checks the graph for what only tree-sitter
// extracts: TypeScript and PHP methods, PHP interfaces, Python decorators
// and base classes, and a call edge in each language.
func treeSitterSelfCheck(dir string) error {
	repo := filepath.Join(dir, "repo")
	for name, src := range treeSitterFixture {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			return err
		}
	}

	idx, err := codegraph.NewIndexer(repo, filepath.Join(dir, "codegraph.db"))
	if err != nil {
		return err
	}
	defer idx.Close()
	if err := idx.Build(); err != nil {
		return err
	}
	store := idx.Store()

	var problems []string
	symbol := func(name string) *codegraph.Symbol {
		syms, err := store.SearchSymbolsByName(name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("look up %s: %v", name, err))
			return nil
		}
		for i := range syms {
			if syms[i].Name == name {
				return &syms[i]
			}
		}
		problems = append(problems, fmt.Sprintf("symbol %s not indexed", name))
		return nil
	}
	wantKind := func(name string, kind codegraph.SymbolKind) {
		if s := symbol(name); s != nil && s.Kind != kind {
			problems = append(problems, fmt.Sprintf("%s is a %s, want %s", name, s.Kind, kind))
		}
	}
	wantCall := func(from, to string) {
		src, dst := symbol(from), symbol(to)
		if src == nil || dst == nil {
			return
		}
		edges, err := store.GetEdgesFrom(src.ID)
		if err != nil {
			problems = append(problems, fmt.Sprintf("edges from %s: %v", from, err))
			return
		}
		for _, e := range edges {
			if e.TargetID == dst.ID && e.Kind == codegraph.EdgeCalls {
				return
			}
		}
		problems = append(problems, fmt.Sprintf("no call edge %s -> %s", from, to))
	}

	// TypeScript
	wantKind("greet", codegraph.SymbolMethod)
	wantCall("greet", "formatGreeting")
	// Python
	if s := symbol("perform"); s != nil {
		if !strings.Contains(s.Decorators, "abstractmethod") {
			problems = append(problems, fmt.Sprintf("perform decorators %q, want abstractmethod", s.Decorators))
		}
		if !strings.Contains(s.BaseClasses, "ABC") {
			problems = append(problems, fmt.Sprintf("perform base classes %q, want ABC", s.BaseClasses))
		}
	}
	wantCall("run_jobs", "load_jobs")

	// PHP. Once the indexer's file walk takes .php files, the graph must
	// show what tree-sitter extracts; until then the parser is checked
	// directly, so the parsers are proven either way.
	if codegraph.IsIndexableFile("auth.php") {
		wantKind("Validator", codegraph.SymbolInterface)
		wantKind("checkToken", codegraph.SymbolMethod)
		wantCall("validate", "checkToken")
	}
	php, err := parseFixture(repo, "src/auth.php")
	if err != nil {
		problems = append(problems, fmt.Sprintf("parse auth.php: %v", err))
	} else {
		kinds := map[string]codegraph.SymbolKind{}
		for _, s := range php.Symbols {
			kinds[s.Name] = s.Kind
		}
		if kinds["Validator"] != codegraph.SymbolInterface {
			problems = append(problems, fmt.Sprintf("php Validator is %q, want interface", kinds["Validator"]))
		}
		if kinds["checkToken"] != codegraph.SymbolMethod {
			problems = append(problems, fmt.Sprintf("php checkToken is %q, want method", kinds["checkToken"]))
		}
		found := false
		for _, e := range php.Edges {
			if e.SourceName == "validate" && e.TargetName == "checkToken" && e.Kind == codegraph.EdgeCalls {
				found = true
			}
		}
		if !found {
			problems = append(problems, "php: no call edge validate -> checkToken")
		}
	}

	if len(problems) > 0 {
		return errors.New("tree-sitter parsers missing or broken (a CGO_ENABLED=0 build uses the regex fallback): " + strings.Join(problems, "; "))
	}
	return nil
}

// parseFixture runs the multi-language parser on one fixture file.
func parseFixture(repo, rel string) (*codegraph.ParseResult, error) {
	p := codegraph.NewMultiLangParser()
	defer p.Close()
	return p.ParseFile(filepath.Join(repo, filepath.FromSlash(rel)))
}

// runIndexSelfCheck is `celeste index selfcheck`, left out of the help: the
// release workflow runs it on every built binary to prove tree-sitter shipped.
func runIndexSelfCheck() int {
	dir, err := os.MkdirTemp("", "celeste-selfcheck-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)
	if err := treeSitterSelfCheck(dir); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	fmt.Println("tree-sitter: ok (typescript, php, python)")
	return 0
}
