package tui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
)

// go-openai's API error text, as a 404 model returns it.
const apiError404 = `error, status code: 404, status: 404 Not Found, message: Model "nonexistent-model-xyz" not found`

func TestErrorTextDropsTheProvidersErrorPrefix(t *testing.T) {
	assert.Equal(t, `status code: 404, status: 404 Not Found, message: Model "nonexistent-model-xyz" not found`,
		errorText(errors.New(apiError404)))
	// Wrapped: the prefix sits after the wrapper's own text.
	assert.Equal(t, "summary: status code: 500, status: 500, message: down",
		errorText(fmt.Errorf("summary: %w", errors.New("error, status code: 500, status: 500, message: down"))))
	assert.Equal(t, "boom", errorText(errors.New("boom")))
	assert.Equal(t, "", errorText(nil))
}

// V5: a failed turn (the path a 404 model takes) says "Error: status code:
// 404 …" in the chat and the status bar, never "Error: error, …".
func TestFailedTurnErrorTextAt80And120(t *testing.T) {
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			client := &fakeToolLLMClient{}
			m := newAuditApp(t, client, sz.w, sz.h)
			m = auditSend(t, m, "hi")
			m, _ = feed(t, m, TurnDoneMsg{Stop: "error", Err: errors.New(apiError404)})
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			assert.NotContains(t, frame, "Error: error,")
			assert.Contains(t, frame, "Error: status code: 404")
			assert.NotContains(t, m.status.text, "error, status")
		})
	}
}

// ErrorMsg shows a provider error the same way.
func TestErrorMsgDropsTheProviderPrefix(t *testing.T) {
	m := newAuditApp(t, &fakeToolLLMClient{}, 120, 40)
	m, _ = step(t, m, ErrorMsg{Err: errors.New(apiError404)})
	assert.NotContains(t, m.status.text, "error, status")
}

// V5: the header does not show ✓ for a --force model nothing validated.
func TestHeaderMarksUnverifiedForceModelAt80And120(t *testing.T) {
	yes := true
	defer providers.SetCatalogForTest("sakana", []providers.CatalogModel{{ID: "fugu", Default: true, Tools: &yes}})()
	for _, sz := range auditSizes {
		t.Run(sz.name, func(t *testing.T) {
			client := &endpointClient{ep: ActiveEndpoint{Provider: "sakana", BaseURL: "https://api.sakana.ai/v1", Model: "fugu"}}
			m := newAuditApp(t, client, sz.w, sz.h)
			m.provider = "sakana"
			m = auditSend(t, m, "/set-model my-private-model --force")
			if m.model != "my-private-model" {
				t.Fatalf("model = %q", m.model)
			}
			frame := auditView(m)
			assertFrameFits(t, frame, sz.w, sz.h)
			header := strings.SplitN(frame, "\n", 2)[0]
			assert.NotContains(t, header, "my-private-model ✓", "header: %s", header)
			assert.Contains(t, header, "my-private-model ?", "header: %s", header)

			// A served model picked afterwards is verified again.
			m = auditSend(t, m, "/set-model fugu")
			header = strings.SplitN(auditView(m), "\n", 2)[0]
			assert.Contains(t, header, "fugu ✓", "header: %s", header)
		})
	}
}
