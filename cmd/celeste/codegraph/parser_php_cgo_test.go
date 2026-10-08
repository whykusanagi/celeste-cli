//go:build cgo

package codegraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The PHP grammar is pinned to tree-sitter-php v0.23.12 (#323: v0.25.0
// needs the go-tree-sitter tag the upstream deleted). This pins down what
// the indexer extracts from it so a grammar change that renames a node
// type shows up here instead of as an emptier index.
func TestMultiLangParser_PHP(t *testing.T) {
	src := `<?php
namespace App\Auth;

use App\Store\TokenStore;

interface Validator {
    public function validate(string $token): bool;
}

final readonly class SessionValidator implements Validator {
    const int TTL = 60;

    public function validate(string $token): bool {
        return $this->check($token) && refresh_session($token);
    }

    private function check(string $t): bool {
        return strlen($t) > 0;
    }
}

function refresh_session(string $t): bool {
    return \App\Audit\log_refresh($t) && Clock::now() > 0;
}

enum Status: string {
    case Active = 'active';

    public function label(): string {
        return ucfirst($this->value);
    }
}

trait Loggable {
    public function log(string $m): void {
        $this?->writer?->write($m);
    }
}
`
	path := writeTempFile(t, "auth.php", src)
	p := NewMultiLangParser()
	defer p.Close()
	result, err := p.ParseFile(path)
	require.NoError(t, err)

	kinds := map[string]SymbolKind{}
	for _, s := range result.Symbols {
		kinds[s.Name] = s.Kind
	}
	assert.Equal(t, SymbolImport, kinds[`App\Store\TokenStore`])
	assert.Equal(t, SymbolInterface, kinds["Validator"])
	// PHP 8.2 readonly class and PHP 8.3 typed constant still parse.
	assert.Equal(t, SymbolClass, kinds["SessionValidator"])
	assert.Equal(t, SymbolMethod, kinds["check"])
	assert.Equal(t, SymbolFunction, kinds["refresh_session"])
	assert.Contains(t, unscoped(result.Edges), RawEdge{SourceName: "validate", TargetName: "check", Kind: EdgeCalls})

	// #347: enums and traits are declarations too.
	assert.Equal(t, SymbolType, kinds["Status"])
	assert.Equal(t, SymbolInterface, kinds["Loggable"])
	assert.Equal(t, SymbolMethod, kinds["label"])
	assert.Equal(t, SymbolMethod, kinds["log"])

	// #347: plain function calls produce edges; a namespaced call resolves
	// to its last segment, the name the declaration is indexed under.
	assert.Contains(t, unscoped(result.Edges), RawEdge{SourceName: "validate", TargetName: "refresh_session", Kind: EdgeCalls})
	assert.Contains(t, unscoped(result.Edges), RawEdge{SourceName: "check", TargetName: "strlen", Kind: EdgeCalls})
	assert.Contains(t, unscoped(result.Edges), RawEdge{SourceName: "refresh_session", TargetName: "log_refresh", Kind: EdgeCalls})
	assert.Contains(t, unscoped(result.Edges), RawEdge{SourceName: "label", TargetName: "ucfirst", Kind: EdgeCalls})
	// Static and nullsafe method calls.
	assert.Contains(t, unscoped(result.Edges), RawEdge{SourceName: "refresh_session", TargetName: "now", Kind: EdgeCalls})
	assert.Contains(t, unscoped(result.Edges), RawEdge{SourceName: "log", TargetName: "write", Kind: EdgeCalls})
	for _, e := range result.Edges {
		assert.NotEmpty(t, e.TargetName)
		assert.NotContains(t, e.TargetName, "$", "a variable is not a call target: %+v", e)
	}
}
