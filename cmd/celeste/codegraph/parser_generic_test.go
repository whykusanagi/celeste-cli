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
