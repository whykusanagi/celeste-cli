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
    return true;
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
	assert.Contains(t, result.Edges, RawEdge{SourceName: "validate", TargetName: "check", Kind: EdgeCalls})
}
