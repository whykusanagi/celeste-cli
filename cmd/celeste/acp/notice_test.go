package acp

import (
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/promptstest"
)

// A small window steps the persona down (W5 guard); the guard's notice
// reaches the editor once, as agent text at the start of the first
// prompt, and never the model.
func TestSmallWindowNoticeShownOnce(t *testing.T) {
	promptstest.Install(t) // realistic profile sizes
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"})
	// A window no other test uses: the guard returns each notice once
	// per process.
	const window = 8117
	c := newTestClient(t, func() (*config.Config, error) {
		return &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10, ContextLimit: window}, nil
	})
	sid := c.newSession(t.TempDir())
	s := c.agent.session(sid)
	if strings.Contains(s.systemPrompt, prompts.NoticePrefix) {
		t.Fatal("the notice leaked into the system prompt")
	}
	if _, err := c.call("session/prompt", textPrompt(sid, "hi")); err != nil {
		t.Fatal(err)
	}
	first := c.agentText()
	if !strings.HasPrefix(first, prompts.NoticePrefix+"Persona: ") || !strings.HasSuffix(first, "one") {
		t.Fatalf("first prompt's agent text = %q, want the notice then the reply", first)
	}
	if _, err := c.call("session/prompt", textPrompt(sid, "again")); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(c.agentText(), prompts.NoticePrefix); got != 1 {
		t.Fatalf("notice shown %d times, want once: %q", got, c.agentText())
	}
	for i, r := range srv.Requests() {
		if strings.Contains(string(r.Raw), "Persona: using") || strings.Contains(string(r.Raw), "Persona: even") {
			t.Fatalf("request %d carries the notice to the model", i)
		}
	}
}

// A session whose window fits the full persona shows no notice.
func TestLargeWindowNoNotice(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
	c := newTestClient(t, func() (*config.Config, error) {
		return &config.Config{APIKey: "k", BaseURL: srv.BaseURL(), Model: "fake-model", Timeout: 10, ContextLimit: 200000}, nil
	})
	sid := c.newSession(t.TempDir())
	if _, err := c.call("session/prompt", textPrompt(sid, "hi")); err != nil {
		t.Fatal(err)
	}
	if got := c.agentText(); got != "ok" {
		t.Fatalf("agent text = %q, want no notice", got)
	}
}
