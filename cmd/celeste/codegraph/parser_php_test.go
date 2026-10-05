package codegraph

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const phpFixture = `<?php
namespace App\Auth;

use App\Store\TokenStore;

interface Validator
{
    public function validate(string $token): bool;
}

final readonly class SessionValidator implements Validator
{
    public function validate(string $token): bool {
        return $this->check($token) && refresh_session($token);
    }

    private static function check(string $t): bool {
        return strlen($t) > 0;
    }
}

function refresh_session(string $t): bool {
    return true;
}

enum Status: string {
    case Active = 'active';

    public function label(): string {
        return ucfirst($this->value);
    }
}

trait Loggable {
    abstract protected function writer(): object;

    public function log(string $m): void {
        $this->writer()->write($m);
    }
}
`

// The regex GenericParser is what CGO_ENABLED=0 release builds index PHP
// with (#347). It sees declarations line by line and call edges by name.
func TestGenericParser_PHP(t *testing.T) {
	path := writeTestFile(t, "auth.php", phpFixture)
	result, err := NewGenericParser("php").ParseFile(path)
	require.NoError(t, err)

	kinds := map[string]SymbolKind{}
	for _, s := range result.Symbols {
		kinds[s.Name] = s.Kind
	}
	assert.Equal(t, SymbolImport, kinds[`App\Store\TokenStore`])
	assert.Equal(t, SymbolInterface, kinds["Validator"])
	assert.Equal(t, SymbolClass, kinds["SessionValidator"])
	assert.Equal(t, SymbolType, kinds["Status"])
	assert.Equal(t, SymbolInterface, kinds["Loggable"])
	assert.Equal(t, SymbolMethod, kinds["validate"])
	assert.Equal(t, SymbolMethod, kinds["check"])
	assert.Equal(t, SymbolMethod, kinds["label"])
	assert.Equal(t, SymbolMethod, kinds["writer"])
	assert.Equal(t, SymbolMethod, kinds["log"])
	assert.Equal(t, SymbolFunction, kinds["refresh_session"])
	_, isSym := kinds["Active"]
	assert.False(t, isSym, "an enum case is not a declaration the graph indexes")

	assert.Contains(t, result.Edges, RawEdge{SourceName: "validate", TargetName: "check", Kind: EdgeCalls})
	assert.Contains(t, result.Edges, RawEdge{SourceName: "validate", TargetName: "refresh_session", Kind: EdgeCalls})
	assert.Contains(t, result.Edges, RawEdge{SourceName: "log", TargetName: "writer", Kind: EdgeCalls})
	// A body ends at its closing brace: check() does not "call" the
	// declarations that follow it.
	assert.NotContains(t, result.Edges, RawEdge{SourceName: "check", TargetName: "refresh_session", Kind: EdgeCalls})
	assert.NotContains(t, result.Edges, RawEdge{SourceName: "writer", TargetName: "log", Kind: EdgeCalls})
}

// A PHP workspace is indexed end to end in every build: the file walker
// used to skip .php entirely, so neither parser ever ran (#347).
func TestIndexer_BuildIndexesPHP(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "auth.php", phpFixture)
	writeFile(t, dir, "session.php", "<?php\nfunction start_session(): bool {\n    return refresh_session('x');\n}\n")

	idx, err := NewIndexer(dir, filepath.Join(dir, "codegraph-php.db"))
	require.NoError(t, err)
	defer idx.Close()
	require.NoError(t, idx.Build())

	funcs, err := idx.Store().FindAllFunctionsWithEdges()
	require.NoError(t, err)
	in := map[string]int{}
	for _, f := range funcs {
		in[f.Name] = f.InEdges
	}
	require.Contains(t, in, "refresh_session", "auth.php must be indexed")
	// validate() in auth.php and start_session() in session.php.
	assert.GreaterOrEqual(t, in["refresh_session"], 2, "same-file and cross-file plain calls must both be edges")
}
