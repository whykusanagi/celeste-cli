package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/prompts/promptstest"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tui"
)

func requestSystem(t *testing.T, srv *fakeprovider.Server, i int) string {
	t.Helper()
	reqs := srv.Requests()
	if len(reqs) <= i {
		t.Fatalf("only %d requests", len(reqs))
	}
	msg := reqs[i].Body["messages"].([]any)[0].(map[string]any)
	if msg["role"] != "system" {
		t.Fatalf("request %d starts with %v", i, msg["role"])
	}
	return msg["content"].(string)
}

func profileBytes(t *testing.T, p prompts.Profile) string {
	t.Helper()
	pp, err := prompts.LoadProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	return pp.SystemPrompt
}

// countSystem counts the chat's system messages containing s.
func countSystem(m tea.Model, s string) int {
	n := 0
	for _, msg := range chatMessages(m) {
		if msg.Role == "system" && strings.Contains(msg.Content, s) {
			n++
		}
	}
	return n
}

// Review Focus 1: a local model at a small window runs on lite, and one
// system message at startup names lite and context_limit (window 9300 is
// used by no other test, so the one-time notice is this test's).
func TestChatStartupShowsTheSmallWindowNotice(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hello there"})
	m, _, _ := chatAppWithContextLimit(t, srv, 9300)
	if n := countSystem(m, "lite profile instead of full"); n != 1 {
		t.Fatalf("startup shows the notice %d times, want 1", n)
	}
	if countSystem(m, "context_limit") == 0 {
		t.Fatal("the notice does not name context_limit")
	}
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "hi"}},
		func(m tea.Model) bool { return lastAssistant(m) == "hello there" && turnIdle(m) }, 30*time.Second)
	if !strings.HasPrefix(requestSystem(t, srv, 0), profileBytes(t, prompts.ProfileLite)) {
		t.Fatal("the request is not on the lite profile")
	}
}

// Review Focus 2: moving to a smaller window mid-session steps the persona
// down at the next request, and the notice shows once, at that turn
// (window 9400 is used by no other test).
func TestChatShowsTheNoticeOnceAfterASwitch(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"}, fakeprovider.Turn{Text: "three"})
	m, deps, _ := chatAppWithContextLimit(t, srv, 200000)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "first"}},
		func(m tea.Model) bool { return lastAssistant(m) == "one" && turnIdle(m) }, 30*time.Second)
	if !strings.HasPrefix(requestSystem(t, srv, 0), profileBytes(t, prompts.ProfileFull)) {
		t.Fatal("a 200k window is not on the full profile")
	}
	deps.adapter.baseConfig.ContextLimit = 9400
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "second"}},
		func(m tea.Model) bool { return lastAssistant(m) == "two" && turnIdle(m) }, 30*time.Second)
	if !strings.HasPrefix(requestSystem(t, srv, 1), profileBytes(t, prompts.ProfileLite)) {
		t.Fatal("the turn after the switch is not on the lite profile")
	}
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "third"}},
		func(m tea.Model) bool { return lastAssistant(m) == "three" && turnIdle(m) }, 30*time.Second)
	if n := countSystem(m, "lite profile instead of full"); n != 1 {
		t.Fatalf("the notice shows %d times, want 1", n)
	}
	if requestSystem(t, srv, 2) != requestSystem(t, srv, 1) {
		t.Fatal("the prompt changed again with nothing changed")
	}
}
