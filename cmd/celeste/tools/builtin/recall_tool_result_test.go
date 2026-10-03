package builtin

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
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

// #211: a result the loop spilled when it was recorded is recalled with the
// id its notice names, paged like a pruned one. The spill base is the
// store's parent (~/.celeste/tool-results by default).
func TestRecallToolResultFindsLoopSpills(t *testing.T) {
	base := t.TempDir()
	store := &compact.Store{Dir: filepath.Join(base, "pruned")}
	body := strings.Repeat("abcdefghij", 20_000) // 200 KB
	capped, _, err := ctxmgr.CapToolResult(body, ctxmgr.DefaultMaxToolResultBytes, "tui-7", "big-1", base)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(capped, `recall_tool_result with id "tui-7/big-1"`) {
		t.Fatalf("notice does not name the id: %q", capped[len(capped)-700:])
	}
	tool := NewRecallToolResultTool(store)
	var got strings.Builder
	for offset := 0; offset < len(body); offset += recallPageBytes {
		res, _ := tool.Execute(context.Background(), map[string]any{"id": "tui-7/big-1", "offset": float64(offset)}, nil)
		if res.Error {
			t.Fatalf("recall at %d: %s", offset, res.Content)
		}
		page := res.Content
		if i := strings.Index(page, "\n\n[bytes "); i >= 0 {
			page = page[:i]
		}
		got.WriteString(page)
	}
	if got.String() != body {
		t.Fatal("paging through the spilled result did not reassemble it")
	}
	res, _ := tool.Execute(context.Background(), map[string]any{"id": "tui-7/nope"}, nil)
	if !res.Error {
		t.Error("an unknown spill id should be an error result")
	}
}
