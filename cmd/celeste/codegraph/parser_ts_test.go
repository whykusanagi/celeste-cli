//go:build cgo

// Tree-sitter TypeScript parser tests. Only compiled when CGo is
// available — the CGO_ENABLED=0 stub in parser_ts_stub.go delegates
// everything to GenericParser and has no independent behavior to test.

package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeTempFile drops `content` to a temp file with the given name
// inside a fresh t.TempDir() and returns the absolute path.
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestTSParser_FunctionAndEdges(t *testing.T) {
	// Tree-sitter should extract both function declarations and call
	// edges between them. The regex GenericParser gets the symbols but
	// fails on cross-function edge resolution in TS's type system.
	src := `
export function validateSession(token: string): boolean {
  return checkToken(token) && refreshSession(token);
}

function checkToken(t: string): boolean {
  return t.length > 0;
}

function refreshSession(t: string): boolean {
  return true;
}
`
	path := writeTempFile(t, "auth.ts", src)

	p := NewTSParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	// Symbols: validateSession, checkToken, refreshSession.
	names := make(map[string]SymbolKind)
	for _, s := range result.Symbols {
		names[s.Name] = s.Kind
	}
	assert.Equal(t, SymbolFunction, names["validateSession"])
	assert.Equal(t, SymbolFunction, names["checkToken"])
	assert.Equal(t, SymbolFunction, names["refreshSession"])

	// Edges: validateSession → checkToken, validateSession → refreshSession.
	// This is the core zero-edge regression fix — GenericParser's shallow
	// regex edge extraction missed these.
	haveEdge := func(from, to string) bool {
		for _, e := range result.Edges {
			if e.SourceName == from && e.TargetName == to && e.Kind == EdgeCalls {
				return true
			}
		}
		return false
	}
	assert.True(t, haveEdge("validateSession", "checkToken"), "must extract call to checkToken")
	assert.True(t, haveEdge("validateSession", "refreshSession"), "must extract call to refreshSession")
}

func TestTSParser_ClassMethodsAndInterfaces(t *testing.T) {
	src := `
interface TokenStore {
  get(key: string): string | null;
  set(key: string, value: string): void;
}

export class RedisTokenStore implements TokenStore {
  private client: any;

  constructor(client: any) {
    this.client = client;
  }

  get(key: string): string | null {
    return this.client.get(key);
  }

  set(key: string, value: string): void {
    this.client.set(key, value);
  }
}
`
	path := writeTempFile(t, "store.ts", src)

	p := NewTSParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	names := make(map[string]SymbolKind)
	for _, s := range result.Symbols {
		names[s.Name] = s.Kind
	}
	assert.Equal(t, SymbolInterface, names["TokenStore"])
	assert.Equal(t, SymbolClass, names["RedisTokenStore"])
	assert.Equal(t, SymbolMethod, names["get"])
	assert.Equal(t, SymbolMethod, names["set"])

	// Method-to-member-call edge: RedisTokenStore.get calls this.client.get.
	// callTarget returns `this.client.get` — the unqualified-suffix
	// fallback in index.go then tries "get" which collides with the
	// interface method, so we get a useful resolved edge either way.
	haveTarget := func(target string) bool {
		for _, e := range result.Edges {
			if e.TargetName == target {
				return true
			}
		}
		return false
	}
	assert.True(t, haveTarget("this.client.get"), "get() body should emit member call edge")
	assert.True(t, haveTarget("this.client.set"), "set() body should emit member call edge")
}

func TestTSParser_ArrowFunctionBindings(t *testing.T) {
	// `const foo = () => ...` should produce a function symbol bound
	// to the variable name, with edges emitted from the arrow body.
	src := `
import { http } from 'lib/http';

const fetchUser = async (id: string) => {
  const resp = await http.get('/users/' + id);
  return resp.data;
};

const validateUser = (u: any) => fetchUser(u.id);
`
	path := writeTempFile(t, "users.ts", src)

	p := NewTSParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	names := make(map[string]SymbolKind)
	for _, s := range result.Symbols {
		names[s.Name] = s.Kind
	}
	assert.Equal(t, SymbolFunction, names["fetchUser"], "arrow fn bound to const should be a function symbol")
	assert.Equal(t, SymbolFunction, names["validateUser"], "inline arrow fn should be a function symbol")

	// Import: 'lib/http' should show up as an import symbol.
	hasImport := false
	for _, s := range result.Symbols {
		if s.Kind == SymbolImport && s.Name == "lib/http" {
			hasImport = true
			break
		}
	}
	assert.True(t, hasImport, "import statement should become an import symbol")

	// Edge: validateUser calls fetchUser.
	haveEdge := func(from, to string) bool {
		for _, e := range result.Edges {
			if e.SourceName == from && e.TargetName == to {
				return true
			}
		}
		return false
	}
	assert.True(t, haveEdge("validateUser", "fetchUser"),
		"validateUser arrow body should emit edge to fetchUser")
}

func TestTSParser_TypeAndEnumDeclarations(t *testing.T) {
	src := `
export type UserID = string;
export enum Status { Active, Inactive, Banned }
export interface Session { id: UserID; status: Status; }
`
	path := writeTempFile(t, "types.ts", src)

	p := NewTSParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	names := make(map[string]SymbolKind)
	for _, s := range result.Symbols {
		names[s.Name] = s.Kind
	}
	assert.Equal(t, SymbolType, names["UserID"])
	assert.Equal(t, SymbolType, names["Status"])
	assert.Equal(t, SymbolInterface, names["Session"])
}

func TestTSParser_TSXSupport(t *testing.T) {
	// TSX files must parse via the TSX grammar. A React component with
	// JSX in the body would fail under plain TS grammar.
	src := `
import { useState } from 'react';

export function Counter(props: { initial: number }) {
  const [count, setCount] = useState(props.initial);
  const increment = () => setCount(count + 1);
  return <button onClick={increment}>{count}</button>;
}
`
	path := writeTempFile(t, "Counter.tsx", src)

	p := NewTSParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	names := make(map[string]SymbolKind)
	for _, s := range result.Symbols {
		names[s.Name] = s.Kind
	}
	assert.Equal(t, SymbolFunction, names["Counter"])
	assert.Equal(t, SymbolFunction, names["increment"])
}

func TestDecoratorEdges(t *testing.T) {
	// @otel_traced def foo(): bar() should produce an edge foo → otel_traced
	// in addition to the regular body edge foo → bar. Decorators are function
	// calls at import time; without this fix they reported incoming_edges: 0.
	src := "@otel_traced\ndef foo():\n    bar()\n"
	path := writeTempFile(t, "decorated.py", src)

	p := NewMultiLangParser()
	defer p.Close()
	res, err := p.ParseFile(path)
	require.NoError(t, err)

	if !hasDecoratorEdge(res.Edges, "foo", "otel_traced") {
		t.Fatalf("expected edge foo -> otel_traced, edges=%v", res.Edges)
	}
	// Body call edge must still be present.
	if !hasDecoratorEdge(res.Edges, "foo", "bar") {
		t.Fatalf("expected body edge foo -> bar, edges=%v", res.Edges)
	}

	// @a.b attribute decorator.
	src2 := "@a.b\ndef baz():\n    pass\n"
	path2 := writeTempFile(t, "attr_dec.py", src2)
	res2, err := p.ParseFile(path2)
	require.NoError(t, err)
	if !hasDecoratorEdge(res2.Edges, "baz", "a.b") {
		t.Fatalf("expected edge baz -> a.b, edges=%v", res2.Edges)
	}

	// @foo(arg) call decorator.
	src3 := "@router.get('/path')\ndef handler():\n    pass\n"
	path3 := writeTempFile(t, "call_dec.py", src3)
	res3, err := p.ParseFile(path3)
	require.NoError(t, err)
	if !hasDecoratorEdge(res3.Edges, "handler", "router.get") {
		t.Fatalf("expected edge handler -> router.get, edges=%v", res3.Edges)
	}
}

func TestSetterEdges(t *testing.T) {
	// Attribute assignment `c.x = 5` must emit an edge from the enclosing
	// function to the property tail name so @property.setter methods are not
	// reported as having zero incoming edges. (#45)
	src := []byte("class C:\n    @property\n    def x(self): return self._x\n    @x.setter\n    def x(self, v): self._x = v\n\ndef use(c):\n    c.x = 5\n")
	path := writeTempFile(t, "setter.py", string(src))

	p := NewMultiLangParser()
	defer p.Close()
	res, err := p.ParseFile(path)
	require.NoError(t, err)

	if !hasDecoratorEdge(res.Edges, "use", "x") {
		t.Fatalf("expected edge use -> x (setter), edges=%v", res.Edges)
	}
}

func TestAttrAssignStillCapturesRHSCall(t *testing.T) {
	// Regression guard: RHS call edges must still be emitted even when the
	// LHS is an attribute assignment (the assignment branch must not return
	// early before recursing into children). (#45)
	src := []byte("def use(c):\n    c.x = make()\n")
	path := writeTempFile(t, "rhs.py", string(src))

	p := NewMultiLangParser()
	defer p.Close()
	res, err := p.ParseFile(path)
	require.NoError(t, err)

	if !hasDecoratorEdge(res.Edges, "use", "make") {
		t.Fatalf("RHS call edge use -> make must still be captured, edges=%v", res.Edges)
	}
}

func hasDecoratorEdge(edges []RawEdge, src, tgt string) bool {
	for _, e := range edges {
		if e.SourceName == src && e.TargetName == tgt && e.Kind == EdgeCalls {
			return true
		}
	}
	return false
}

func TestTSParser_NestedCallEdgeAttribution(t *testing.T) {
	// Nested functions: an inner function's calls should attribute to
	// the inner name, not the outer one.
	src := `
function outer() {
  inner1();
  function inner1() {
    inner2();
  }
  function inner2() {}
}
`
	path := writeTempFile(t, "nested.ts", src)

	p := NewTSParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	haveEdge := func(from, to string) bool {
		for _, e := range result.Edges {
			if e.SourceName == from && e.TargetName == to {
				return true
			}
		}
		return false
	}
	assert.True(t, haveEdge("outer", "inner1"), "outer calls inner1")
	assert.True(t, haveEdge("inner1", "inner2"), "inner1 calls inner2")
	assert.False(t, haveEdge("outer", "inner2"), "outer should NOT be attributed inner1's body call")
}
