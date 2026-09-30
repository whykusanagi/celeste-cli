package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// searchCount indexes a workspace whose main.go holds twelve
// validateSessionTokenN functions (plus main and helper, 14 symbols in all),
// runs celeste_code_search for "validate session token" with args, and returns
// the result count from the "Found N symbols" header. Measured on this
// fixture: 13 symbols match the query when the count is not limited.
func searchCount(t *testing.T, args map[string]any) int {
	t.Helper()
	srv, dir := newTestServerWithWorkspace(t)
	var b strings.Builder
	b.WriteString("package main\n\nfunc main() {\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "\tvalidateSessionToken%d(\"x\")\n", i)
	}
	b.WriteString("}\n\n")
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "// validateSessionToken%d validates a session token.\nfunc validateSessionToken%d(token string) bool { return helper(token) }\n\n", i, i)
	}
	b.WriteString("func helper(s string) bool { return s != \"\" }\n")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _ = callTool(t, srv, "celeste_index", map[string]any{"operation": "rebuild"})
	args["query"] = "validate session token"
	_, payload := callTool(t, srv, "celeste_code_search", args)
	text := payload["content"].([]any)[0].(map[string]any)["text"].(string)
	var got int
	if _, err := fmt.Sscanf(text, "Found %d symbols", &got); err != nil {
		t.Fatalf("unexpected search output: %q", text)
	}
	return got
}

// #209: the MCP schema advertises top_k and the plugin's skills send it.
func TestCodeSearchHonoursTopK(t *testing.T) {
	for _, tc := range []struct {
		topK any
		want int
	}{{1, 1}, {3, 3}, {50, 13}} {
		if got := searchCount(t, map[string]any{"top_k": tc.topK}); got != tc.want {
			t.Errorf("top_k=%v returned %d results, want %d", tc.topK, got, tc.want)
		}
	}
}

func TestCodeSearchWithoutTopKUsesDefault(t *testing.T) {
	if got := searchCount(t, map[string]any{}); got != 10 {
		t.Fatalf("no top_k returned %d results, want the default 10", got)
	}
}

func TestSearchArgs(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   map[string]any
		want any // args["limit"] afterwards; nil = absent
	}{
		{"json number", map[string]any{"top_k": float64(3)}, 3},
		{"go int", map[string]any{"top_k": 4}, 4},
		{"top_k wins over limit", map[string]any{"top_k": float64(2), "limit": float64(9)}, 2},
		{"zero falls back to the default", map[string]any{"top_k": float64(0)}, nil},
		{"negative falls back", map[string]any{"top_k": float64(-5)}, nil},
		{"fraction falls back", map[string]any{"top_k": 2.5}, nil},
		{"string falls back", map[string]any{"top_k": "5"}, nil},
		{"top_k above the cap", map[string]any{"top_k": float64(500)}, maxSearchResults},
		{"top_k of a billion is capped", map[string]any{"top_k": float64(1e9)}, maxSearchResults},
		{"top_k past int32 falls back", map[string]any{"top_k": float64(1e12)}, nil},
		{"raw limit above the cap", map[string]any{"limit": float64(5000)}, maxSearchResults},
		{"raw limit kept", map[string]any{"limit": float64(7)}, float64(7)},
		// A limit that is not a whole number of at least 1 is dropped, so
		// the default applies: the builtin parses strings and truncates
		// floats itself, and a huge or negative value reached
		// make([]SearchResult, 0, TopK) and panicked the server.
		{"string limit dropped", map[string]any{"limit": "1000000000000000"}, nil},
		{"limit past int32 dropped", map[string]any{"limit": float64(1e12)}, nil},
		{"limit of 1e15 dropped", map[string]any{"limit": float64(1e15)}, nil},
		{"negative limit dropped", map[string]any{"limit": float64(-5)}, nil},
		{"fractional limit dropped", map[string]any{"limit": 2.5}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := searchArgs(tc.in)
			if _, ok := out["top_k"]; ok {
				t.Fatalf("top_k left in args: %v", out)
			}
			if got := out["limit"]; got != tc.want {
				t.Fatalf("limit = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// A string or huge limit from a client used to panic celeste serve.
func TestCodeSearchHugeLimitUsesDefault(t *testing.T) {
	for _, limit := range []any{"1000000000000000", 1e15, -5} {
		if got := searchCount(t, map[string]any{"limit": limit}); got != 10 {
			t.Errorf("limit=%v returned %d results, want the default 10", limit, got)
		}
	}
}
