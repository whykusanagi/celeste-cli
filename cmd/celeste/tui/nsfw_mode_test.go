package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// snapshotClient is an endpointClient that can snapshot and restore its
// endpoint, as the chat's adapter does.
type snapshotClient struct {
	endpointClient
	restored int
}

func (c *snapshotClient) SnapshotEndpoint() any { ep := c.ep; return &ep }
func (c *snapshotClient) RestoreEndpoint(s any) error {
	c.ep = *s.(*ActiveEndpoint)
	c.restored++
	return nil
}

func newSnapshotClient() *snapshotClient {
	c := &snapshotClient{}
	c.ep = ActiveEndpoint{Provider: "sakana", BaseURL: "https://api.sakana.ai/v1", APIKey: "sakana-key", Model: "fugu"}
	c.onSwitch = func(endpoint string) ActiveEndpoint {
		if endpoint == "venice" {
			return ActiveEndpoint{Provider: "venice", BaseURL: "https://api.venice.ai/api/v1", APIKey: "venice-key", Model: "venice-uncensored-1-2"}
		}
		// The old /safe path: a name with no profile keeps the current URL.
		return c.ep
	}
	return c
}

// N1: /safe restores the endpoint, key and model /nsfw left, and the header
// and status line show it again, at 80x24 and 120x40.
func TestSafeRestoresTheChatAt80And120(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			c := newSnapshotClient()
			m := newAuditApp(t, c, sz.w, sz.h)
			m.provider = "sakana"
			m = auditSend(t, m, "/nsfw")
			require.Equal(t, "venice", c.ep.Provider)
			m = auditSend(t, m, "/safe")
			assert.Equal(t, 1, c.restored)
			assert.Equal(t, "sakana-key", c.ep.APIKey)
			assert.Equal(t, "fugu", m.model)
			assert.Equal(t, "sakana", m.endpoint)
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			header := strings.SplitN(frame, "\n", 2)[0]
			assert.Contains(t, header, "sakana • fugu")
			assert.NotContains(t, header, "venice")
			assert.Contains(t, statusLineRow(frame), "│ fugu │")
		})
	}
}

// After /nsfw, an /endpoint switch leaves Venice: the saved endpoint is
// stale, so a later /safe does not restore it.
func TestSafeAfterAnotherEndpointDoesNotRestoreAStaleSnapshot(t *testing.T) {
	c := newSnapshotClient()
	m := newAuditApp(t, c, 120, 40)
	m = auditSend(t, m, "/nsfw")
	m, _ = m.switchEndpoint("grok")
	assert.Nil(t, m.safe)
	m = auditSend(t, m, "/safe")
	assert.Equal(t, 0, c.restored)
	// Not in NSFW mode, /safe has nothing to undo: the chat stays on grok
	// rather than going back to the endpoint /nsfw once left.
	assert.Equal(t, "grok", m.endpoint)
}
