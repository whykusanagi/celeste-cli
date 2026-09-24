package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/compact"
)

func TestRecallToolResult(t *testing.T) {
	store := &compact.Store{Dir: t.TempDir()}
	body := strings.Repeat("0123456789", 10_000) // 100 KB: two-plus pages
	if err := store.Save("toolu_1", body); err != nil {
		t.Fatal(err)
	}
	tool := NewRecallToolResultTool(store)

	res, err := tool.Execute(context.Background(), map[string]any{"id": "toolu_1"}, nil)
	if err != nil || res.Error {
		t.Fatalf("recall failed: %v %+v", err, res)
	}
	if !strings.HasPrefix(res.Content, body[:recallPageBytes]) || !strings.Contains(res.Content, "offset 49152") {
		t.Error("first page should hold the first 48 KB and say how to continue")
	}

	var got strings.Builder
	for offset := 0; offset < len(body); offset += recallPageBytes {
		res, _ := tool.Execute(context.Background(), map[string]any{"id": "toolu_1", "offset": float64(offset)}, nil)
		page := res.Content
		if i := strings.Index(page, "\n\n[bytes "); i >= 0 {
			page = page[:i]
		}
		got.WriteString(page)
	}
	if got.String() != body {
		t.Error("paging through the result did not reassemble it")
	}

	res, _ = tool.Execute(context.Background(), map[string]any{"id": "missing"}, nil)
	if !res.Error {
		t.Error("an unknown id should be an error result")
	}
	res, _ = tool.Execute(context.Background(), map[string]any{"id": "../../etc/passwd"}, nil)
	if !res.Error {
		t.Error("a path-like id must be rejected")
	}
}
