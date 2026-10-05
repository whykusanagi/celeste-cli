package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
)

// treeSitterFixture is a tiny TypeScript/PHP/Python/Java repo. Every assertion in
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
	"src/Worker.java": `class Worker {
    void runTask() {
        prepareTask();
    }

    void prepareTask() {}
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
// extracts: TypeScript, PHP and Java methods, PHP interfaces, Python
// decorators and base classes, and a call edge in each language. It reads
// the graph only, so it proves the indexer reaches each parser too.
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
	// Each symbol is looked up once, so a missing one is reported once
	// even when several checks need it.
	seen := map[string]*codegraph.Symbol{}
	symbol := func(name string) *codegraph.Symbol {
		if s, ok := seen[name]; ok {
			return s
		}
		seen[name] = nil
		syms, err := store.SearchSymbolsByName(name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("look up %s: %v", name, err))
			return nil
		}
		for i := range syms {
			if syms[i].Name == name {
				seen[name] = &syms[i]
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

	// PHP
	wantKind("Validator", codegraph.SymbolInterface)
	wantKind("checkToken", codegraph.SymbolMethod)
	wantCall("validate", "checkToken")
	// Java
	wantKind("prepareTask", codegraph.SymbolMethod)
	wantCall("runTask", "prepareTask")

	if len(problems) > 0 {
		return errors.New("tree-sitter parsers missing or broken (a CGO_ENABLED=0 build uses the regex fallback): " + strings.Join(problems, "; "))
	}
	return nil
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
	fmt.Println("tree-sitter: ok (typescript, php, python, java)")
	return 0
}
