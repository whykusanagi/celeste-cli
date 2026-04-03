package codegraph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitIdentifier(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"validateSession", []string{"validate", "session"}},
		{"HTTPServer", []string{"http", "server"}},
		{"get_user_by_id", []string{"get", "user", "by", "id"}},
		{"parseJSON", []string{"parse", "json"}},
		{"XMLParser", []string{"xml", "parser"}},
		{"ID", []string{"id"}},
		{"x", []string{"x"}},
		{"HTMLToMarkdown", []string{"html", "to", "markdown"}},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := splitIdentifier(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractTypeTokens(t *testing.T) {
	sig := "func HandleRequest(w http.ResponseWriter, r *http.Request) (string, error)"
	tokens := extractTypeTokens(sig)

	assert.Contains(t, tokens, "http")
	assert.Contains(t, tokens, "response")
	assert.Contains(t, tokens, "writer")
	assert.Contains(t, tokens, "request")
	assert.Contains(t, tokens, "string")
	assert.Contains(t, tokens, "error")
}

func TestShinglesForSymbol(t *testing.T) {
	sym := Symbol{
		Name:      "validateSession",
		Kind:      SymbolFunction,
		Package:   "auth",
		Signature: "func validateSession(token string, store SessionStore) (*User, error)",
	}
	source := []byte(`func validateSession(token string, store SessionStore) (*User, error) {
	if token == "" {
		return nil, ErrUnauthorized
	}
	user := store.GetByToken(token)
	return user, nil
}`)

	shingles := ShinglesForSymbol(sym, source)
	require.NotEmpty(t, shingles)

	// Should contain name parts
	assert.Contains(t, shingles, "validate")
	assert.Contains(t, shingles, "session")

	// Should contain package name
	assert.Contains(t, shingles, "auth")

	// Should contain type tokens
	assert.Contains(t, shingles, "token")
	assert.Contains(t, shingles, "string")
	assert.Contains(t, shingles, "user")
	assert.Contains(t, shingles, "error")
}

func TestDeduplicateLowercase(t *testing.T) {
	input := []string{"Hello", "world", "hello", "WORLD", "Go"}
	result := deduplicateLowercase(input)

	assert.Contains(t, result, "hello")
	assert.Contains(t, result, "world")
	assert.Contains(t, result, "go")
	assert.Len(t, result, 3)
}
