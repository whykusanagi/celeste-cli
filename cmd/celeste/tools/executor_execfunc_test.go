package tools

import (
	"context"
	"sort"
	"sync"
	"testing"
)

func TestExecutorUsesExecFunc(t *testing.T) {
	r := NewRegistry()
	r.Register(timeoutTool{})
	ex := NewStreamingToolExecutorWithContext(context.Background(), r)
	var mu sync.Mutex
	var seen []string
	ex.SetExecFunc(func(_ context.Context, callID string, tool Tool, input map[string]any) (ToolResult, error) {
		mu.Lock()
		seen = append(seen, callID+":"+tool.Name())
		mu.Unlock()
		return ToolResult{Content: "via exec func " + input["k"].(string)}, nil
	})
	ex.AddTool("a", "t", `{"k":"1"}`)
	ex.AddTool("b", "t", `{"k":"2"}`)
	ex.Done()
	res := ex.Wait()
	if res[0].Result.Content != "via exec func 1" || res[1].Result.Content != "via exec func 2" {
		t.Fatalf("results = %+v", res)
	}
	sort.Strings(seen)
	if len(seen) != 2 || seen[0] != "a:t" || seen[1] != "b:t" {
		t.Fatalf("exec func saw %v", seen)
	}
}
