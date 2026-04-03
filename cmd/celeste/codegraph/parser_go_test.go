package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoParser_ParseFile_Functions(t *testing.T) {
	src := `package example

import "fmt"

// Greet returns a greeting message.
func Greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

func privateHelper() {}
`
	path := writeTestFile(t, "example.go", src)
	parser := NewGoParser()
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	// Should find two functions
	funcs := filterByKind(result.Symbols, SymbolFunction)
	assert.Len(t, funcs, 2)

	greet := findSymbol(result.Symbols, "Greet")
	require.NotNil(t, greet)
	assert.Equal(t, SymbolFunction, greet.Kind)
	assert.Equal(t, "example", greet.Package)
	assert.Contains(t, greet.Signature, "name string")
	assert.Contains(t, greet.Signature, "string")
}

func TestGoParser_ParseFile_TypesAndMethods(t *testing.T) {
	src := `package server

type Handler struct {
	name string
}

func (h *Handler) ServeHTTP(w Writer, r Reader) {
	// handler logic
}

type Writer interface {
	Write([]byte) (int, error)
}
`
	path := writeTestFile(t, "server.go", src)
	parser := NewGoParser()
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	// Should find struct, method, interface
	handler := findSymbol(result.Symbols, "Handler")
	require.NotNil(t, handler)
	assert.Equal(t, SymbolStruct, handler.Kind)

	serve := findSymbol(result.Symbols, "ServeHTTP")
	require.NotNil(t, serve)
	assert.Equal(t, SymbolMethod, serve.Kind)

	writer := findSymbol(result.Symbols, "Writer")
	require.NotNil(t, writer)
	assert.Equal(t, SymbolInterface, writer.Kind)
}

func TestGoParser_ParseFile_ConstsAndVars(t *testing.T) {
	src := `package config

const MaxRetries = 3

var DefaultTimeout = 30
`
	path := writeTestFile(t, "config.go", src)
	parser := NewGoParser()
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	maxRetries := findSymbol(result.Symbols, "MaxRetries")
	require.NotNil(t, maxRetries)
	assert.Equal(t, SymbolConst, maxRetries.Kind)

	defaultTimeout := findSymbol(result.Symbols, "DefaultTimeout")
	require.NotNil(t, defaultTimeout)
	assert.Equal(t, SymbolVar, defaultTimeout.Kind)
}

func TestGoParser_ParseFile_Edges(t *testing.T) {
	src := `package main

import "fmt"

func main() {
	helper()
	fmt.Println("hello")
}

func helper() {}
`
	path := writeTestFile(t, "main.go", src)
	parser := NewGoParser()
	result, err := parser.ParseFile(path)
	require.NoError(t, err)

	// Should have call edges
	assert.NotEmpty(t, result.Edges, "should detect at least one edge")
}

// writeTestFile creates a temporary Go source file for testing.
func writeTestFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}

// filterByKind filters symbols by kind.
func filterByKind(syms []Symbol, kind SymbolKind) []Symbol {
	var result []Symbol
	for _, s := range syms {
		if s.Kind == kind {
			result = append(result, s)
		}
	}
	return result
}

// findSymbol finds a symbol by name.
func findSymbol(syms []Symbol, name string) *Symbol {
	for i, s := range syms {
		if s.Name == name {
			return &syms[i]
		}
	}
	return nil
}
