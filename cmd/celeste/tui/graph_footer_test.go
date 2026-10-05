package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// The /graph overview's key footer wraps to the width instead of being
// cut at 80 columns ("[Q/Esc] B"), at both audit sizes.
func TestGraphFooterFits(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
			var nodes []graphNode
			for i := range 60 { // more files than the screen holds
				nodes = append(nodes, graphNode{File: fmt.Sprintf("pkg/file_%02d.go", i), Package: "go"})
			}
			g := GraphModel{nodes: nodes, expanded: map[int]bool{}, width: sz.w, height: sz.h, viewMode: "overview"}
			m.graphModel = &g
			m.viewMode = "graph"
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			flat := strings.Join(strings.Fields(frame), " ")
			for _, key := range []string{"[↑/↓] Navigate", "[Enter] Expand", "[D] Detail", "[/] Search", "[Tab] Filter", "[Q/Esc] Back"} {
				assert.Contains(t, flat, key, frame)
			}
		})
	}
}
