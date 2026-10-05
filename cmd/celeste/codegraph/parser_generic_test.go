package codegraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenericParser_Python(t *testing.T) {
	src := `import os
from pathlib import Path

class UserService:
    def __init__(self, db):
        self.db = db

    def get_user(self, user_id: int) -> dict:
        return self.db.find(user_id)

def standalone_func(x, y):
    return x + y
`
	path := writeTestFile(t, "service.py", src)
	parser := NewGenericParser("python")
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	// Should find class
	cls := findSymbol(result.Symbols, "UserService")
	require.NotNil(t, cls)
	assert.Equal(t, SymbolClass, cls.Kind)

	// Should find standalone function
	funcs := filterByKind(result.Symbols, SymbolFunction)
	assert.GreaterOrEqual(t, len(funcs), 1, "should find standalone_func")

	// Should find methods
	methods := filterByKind(result.Symbols, SymbolMethod)
	assert.GreaterOrEqual(t, len(methods), 2, "should find __init__ and get_user")

	// Should find imports
	imports := filterByKind(result.Symbols, SymbolImport)
	assert.GreaterOrEqual(t, len(imports), 1)
}

func TestGenericParser_JavaScript(t *testing.T) {
	src := `import { useState } from 'react';
const lodash = require('lodash');

function handleClick(event) {
    console.log(event);
}

class EventEmitter {
    constructor() {
        this.listeners = {};
    }

    emit(event, data) {
        // emit logic
    }
}

const arrowFunc = (x) => x * 2;

export default handleClick;
export { EventEmitter };
`
	path := writeTestFile(t, "app.js", src)
	parser := NewGenericParser("javascript")
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	// Should find function
	fn := findSymbol(result.Symbols, "handleClick")
	require.NotNil(t, fn)

	// Should find class
	cls := findSymbol(result.Symbols, "EventEmitter")
	require.NotNil(t, cls)
	assert.Equal(t, SymbolClass, cls.Kind)
}

func TestGenericParser_TypeScript(t *testing.T) {
	src := `import { Request, Response } from 'express';

interface UserService {
    getUser(id: string): Promise<User>;
}

type Config = {
    port: number;
    host: string;
};

function createServer(config: Config): void {
    // ...
}

export class Server implements UserService {
    async getUser(id: string): Promise<User> {
        return {} as User;
    }
}
`
	path := writeTestFile(t, "server.ts", src)
	parser := NewGenericParser("typescript")
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	// Should find interface
	iface := findSymbol(result.Symbols, "UserService")
	require.NotNil(t, iface)
	assert.Equal(t, SymbolInterface, iface.Kind)

	// Should find type alias
	cfg := findSymbol(result.Symbols, "Config")
	require.NotNil(t, cfg)
	assert.Equal(t, SymbolType, cfg.Kind)

	// Should find class
	srv := findSymbol(result.Symbols, "Server")
	require.NotNil(t, srv)
	assert.Equal(t, SymbolClass, srv.Kind)
}

func TestGenericParser_PythonCallEdges(t *testing.T) {
	src := `def helper(x):
    return x + 1

def process(items):
    for item in items:
        result = helper(item)
    return result

def unused():
    pass
`
	path := writeTestFile(t, "calls.py", src)
	parser := NewGenericParser("python")
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	// Should find call edge from process -> helper
	assert.NotEmpty(t, result.Edges, "should detect call edges")
	found := false
	for _, e := range result.Edges {
		if e.SourceName == "process" && e.TargetName == "helper" && e.Kind == EdgeCalls {
			found = true
		}
	}
	assert.True(t, found, "should find process -> helper edge")
}

func TestGenericParser_JSCallEdges(t *testing.T) {
	src := `function validate(input) {
    return input.length > 0;
}

function processForm(data) {
    if (validate(data.name)) {
        submit(data);
    }
}

function submit(payload) {
    fetch('/api', payload);
}
`
	path := writeTestFile(t, "form.js", src)
	parser := NewGenericParser("javascript")
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	assert.NotEmpty(t, result.Edges, "should detect call edges")

	// processForm -> validate
	foundValidate := false
	// processForm -> submit
	foundSubmit := false
	for _, e := range result.Edges {
		if e.SourceName == "processForm" && e.TargetName == "validate" {
			foundValidate = true
		}
		if e.SourceName == "processForm" && e.TargetName == "submit" {
			foundSubmit = true
		}
	}
	assert.True(t, foundValidate, "should find processForm -> validate edge")
	assert.True(t, foundSubmit, "should find processForm -> submit edge")
}

func TestGenericParser_Rust(t *testing.T) {
	src := `use std::collections::HashMap;

pub struct Config {
    pub port: u16,
}

pub trait Handler {
    fn handle(&self, req: Request) -> Response;
}

impl Handler for Config {
    fn handle(&self, req: Request) -> Response {
        Response::ok()
    }
}

pub fn create_server(config: Config) -> Server {
    Server::new(config)
}
`
	path := writeTestFile(t, "server.rs", src)
	parser := NewGenericParser("rust")
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	// Should find struct
	cfg := findSymbol(result.Symbols, "Config")
	require.NotNil(t, cfg)
	assert.Equal(t, SymbolStruct, cfg.Kind)

	// Should find trait as interface
	handler := findSymbol(result.Symbols, "Handler")
	require.NotNil(t, handler)
	assert.Equal(t, SymbolInterface, handler.Kind)

	// Should find function
	fn := findSymbol(result.Symbols, "create_server")
	require.NotNil(t, fn)
	assert.Equal(t, SymbolFunction, fn.Kind)
}

// Call targets defined in another file are still edges: the indexer
// resolves them by name once every file's symbols are stored, and drops
// the ones nothing declares. Keeping only same-file targets lost every
// cross-file edge in CGO_ENABLED=0 builds (#47, #347).
func TestGenericParser_CrossFileCallEdges(t *testing.T) {
	src := `def process(items):
    store = Store()
    return in_databricks(items)

def other():
    pass
`
	path := writeTestFile(t, "calls.py", src)
	result, err := NewGenericParser("python").ParseFile(path)
	require.NoError(t, err)
	assert.Contains(t, result.Edges, RawEdge{SourceName: "process", TargetName: "in_databricks", Kind: EdgeCalls})
	assert.Contains(t, result.Edges, RawEdge{SourceName: "process", TargetName: "Store", Kind: EdgeCalls})
	// The next declaration inside the body window is not a call.
	assert.NotContains(t, result.Edges, RawEdge{SourceName: "process", TargetName: "other", Kind: EdgeCalls})

	php := "<?php\nfunction make(): Thing {\n    return new Thing(load_thing());\n}\n"
	path = writeTestFile(t, "make.php", php)
	result, err = NewGenericParser("php").ParseFile(path)
	require.NoError(t, err)
	assert.Contains(t, result.Edges, RawEdge{SourceName: "make", TargetName: "load_thing", Kind: EdgeCalls})
	// `new Thing(` constructs; it is not a call to a function named Thing.
	assert.NotContains(t, result.Edges, RawEdge{SourceName: "make", TargetName: "Thing", Kind: EdgeCalls})
}

// A function's call scan must stop at the end of that function. Otherwise
// `first` inherits every call made by the functions declared below it, and
// the indexer turns those into false cross-file caller edges.
func TestGenericParser_CallScanStopsAtFunctionEnd(t *testing.T) {
	cases := []struct {
		lang, file, src string
	}{
		{"python", "adj.py", "def first():\n    return 1\n\ndef second():\n    helper()\n"},
		{"python", "adj_method.py", "class C:\n    def first(self):\n        return 1\n\n    def second(self):\n        helper()\n"},
		{"python", "adj_sig.py", "def first(\n    a,\n    b,\n):\n    return a\n\ndef second():\n    helper()\n"},
		{"typescript", "adj.ts", "function first(){return 1;}\nfunction second(){helper();}\n"},
		{"typescript", "adj_obj.ts", "function first(opts: {a: number}): number {\n  return opts.a;\n}\nfunction second() {\n  helper();\n}\n"},
		{"javascript", "adj.js", "function first() {\n  return 1;\n}\nfunction second() {\n  helper();\n}\n"},
		{"rust", "adj.rs", "fn first() -> i32 {\n    1\n}\n\nfn second() {\n    helper();\n}\n"},
		{"php", "adj.php", "<?php\nfunction first() { return 1; }\nfunction second() { helper(); }\n"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := writeTestFile(t, tc.file, tc.src)
			result, err := NewGenericParser(tc.lang).ParseFile(path)
			require.NoError(t, err)
			assert.Contains(t, result.Edges, RawEdge{SourceName: "second", TargetName: "helper", Kind: EdgeCalls})
			for _, e := range result.Edges {
				assert.NotEqual(t, "first", e.SourceName, "first calls nothing, got edge to %s", e.TargetName)
			}
		})
	}
}

// Braces and quotes inside strings and comments do not end a body early.
func TestGenericParser_BracedBodyIgnoresStringsAndComments(t *testing.T) {
	cases := []struct {
		lang, file, src string
	}{
		{"php", "str.php", "<?php\nfunction second() { $s = \"}\"; helper(); }\n"},
		{"php", "sq.php", "<?php\nfunction second() { $s = '\\'}'; helper(); }\n"},
		{"php", "hash.php", "<?php\nfunction second() {\n    # closing } here\n    helper();\n}\n"},
		{"javascript", "cmt.js", "function second() {\n  // a } in a comment\n  /* and } here */\n  const s = `}`;\n  helper();\n}\n"},
		{"rust", "str.rs", "fn second<'a>(x: &'a str) {\n    let s = \"}\";\n    helper();\n}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			path := writeTestFile(t, tc.file, tc.src)
			result, err := NewGenericParser(tc.lang).ParseFile(path)
			require.NoError(t, err)
			assert.Contains(t, result.Edges, RawEdge{SourceName: "second", TargetName: "helper", Kind: EdgeCalls})
		})
	}
}

// PHP language constructs are keywords only in PHP: a JS function named
// list() still gets call edges, and callers of it still get an edge.
func TestGenericParser_PHPKeywordsArePHPOnly(t *testing.T) {
	path := writeTestFile(t, "list.js", "function list() {\n  helper();\n}\nfunction main() {\n  list();\n  empty();\n}\n")
	result, err := NewGenericParser("javascript").ParseFile(path)
	require.NoError(t, err)
	assert.Contains(t, result.Edges, RawEdge{SourceName: "list", TargetName: "helper", Kind: EdgeCalls})
	assert.Contains(t, result.Edges, RawEdge{SourceName: "main", TargetName: "list", Kind: EdgeCalls})
	assert.Contains(t, result.Edges, RawEdge{SourceName: "main", TargetName: "empty", Kind: EdgeCalls})

	path = writeTestFile(t, "list.php", "<?php\nfunction main() {\n    list($a, $b) = pair();\n    if (empty($a)) { helper(); }\n}\n")
	result, err = NewGenericParser("php").ParseFile(path)
	require.NoError(t, err)
	assert.Contains(t, result.Edges, RawEdge{SourceName: "main", TargetName: "helper", Kind: EdgeCalls})
	assert.NotContains(t, result.Edges, RawEdge{SourceName: "main", TargetName: "list", Kind: EdgeCalls})
	assert.NotContains(t, result.Edges, RawEdge{SourceName: "main", TargetName: "empty", Kind: EdgeCalls})
}
