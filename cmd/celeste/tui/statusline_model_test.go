package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
)

// statusLineRow is the frame's status line row (the one with the "│"
// segment separators), or "" when there is none.
func statusLineRow(frame string) string {
	for _, l := range strings.Split(frame, "\n") {
		if strings.Contains(l, " │ ") && strings.Contains(l, "⚙") {
			return l
		}
	}
	return ""
}

// N4: the status line's model segment follows /set-model, a --force model
// included.
func TestStatusLineModelFollowsSetModelAt80And120(t *testing.T) {
	yes := true
	defer providers.SetCatalogForTest("sakana", []providers.CatalogModel{
		{ID: "fugu", Default: true, Tools: &yes}, {ID: "fugu-ultra", Tools: &yes},
	})()
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			client := &endpointClient{ep: ActiveEndpoint{Provider: "sakana", BaseURL: "https://api.sakana.ai/v1", Model: "fugu"}}
			m := newAuditApp(t, client, sz.w, sz.h)
			m.provider = "sakana"
			m = m.syncStatusLine()
			assert.Contains(t, statusLineRow(auditView(m)), "│ fugu │")

			m = auditSend(t, m, "/set-model fugu-ultra")
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			assert.Contains(t, statusLineRow(frame), "│ fugu-ultra │", "frame:\n%s", frame)

			m = auditSend(t, m, "/set-model my-model --force")
			assert.Contains(t, statusLineRow(auditView(m)), "│ my-model │")
		})
	}
}
