package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
)

// /session list, info, rename and delete leave no empty System bubble ahead
// of their output, at both audit sizes.
func TestSessionCommandsNoEmptyBubble(t *testing.T) {
	for _, sz := range auditSizes {
		for _, command := range []string{"/session list", "/session info", "/session rename 1 x", "/session delete 1"} {
			t.Run(sz.name+command, func(t *testing.T) {
				m := newAuditApp(t, &fakeToolLLMClient{}, sz.w, sz.h)
				mgr := &diskSessions{mgr: config.NewSessionManager()}
				m = m.SetSessionManager(mgr, mgr.mgr.NewSession())
				before := len(m.chat.GetMessages())
				m = auditSend(t, m, command)
				msgs := m.chat.GetMessages()
				require.Greater(t, len(msgs), before, "the command says something")
				for _, msg := range msgs[before:] {
					assert.NotEmpty(t, strings.TrimSpace(msg.Content), "an empty %s bubble", msg.Role)
				}
				frame := auditView(m)
				assertFrameFits(t, frame, sz.w, sz.h)
			})
		}
	}
}
