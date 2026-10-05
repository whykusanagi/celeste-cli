package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/prompts/promptstest"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
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

// A step-down that no longer applies leaves no notice behind: a compose at a
// small window queues the lite notice, a compose back at a large one drops
// it, and the next turn runs on full with no lite notice (window 9500 is
// used by no other test).
func TestChatDropsAStaleNotice(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"})
	m, deps, _ := chatAppWithContextLimit(t, srv, 200000)
	deps.adapter.baseConfig.ContextLimit = 9500
	deps.adapter.RefreshSystemPrompt()
	deps.adapter.baseConfig.ContextLimit = 200000
	deps.adapter.RefreshSystemPrompt()
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "first"}},
		func(m tea.Model) bool { return lastAssistant(m) == "one" && turnIdle(m) }, 30*time.Second)
	if !strings.HasPrefix(requestSystem(t, srv, 0), profileBytes(t, prompts.ProfileFull)) {
		t.Fatal("the request is not on the full profile")
	}
	if n := countSystem(m, "lite profile instead of full"); n != 0 {
		t.Fatalf("a stale lite notice shows %d times", n)
	}
}

// The persona notice reads the same on every TUI path: an info line, not a
// hook warning.
func TestChatPersonaNoticeIsInfo(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"})
	m, deps, _ := chatAppWithContextLimit(t, srv, 200000)
	deps.adapter.baseConfig.ContextLimit = 9600
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "first"}},
		func(m tea.Model) bool { return lastAssistant(m) == "one" && turnIdle(m) }, 30*time.Second)
	if countSystem(m, prompts.NoticePrefix+"Persona: using the lite profile") != 1 {
		t.Fatal("the turn's persona notice is not one info line")
	}
	if countSystem(m, "⚠ Persona:") != 0 {
		t.Fatal("the persona notice shows as a warning")
	}
}

// requestPrefix is request i's system prompt and tool schemas, in
// estimated tokens, and how many tools it carried.
func requestPrefix(t *testing.T, srv *fakeprovider.Server, i int) (tokens, tools int) {
	t.Helper()
	sys := requestSystem(t, srv, i)
	defs, _ := srv.Requests()[i].Body["tools"].([]any)
	b, err := json.Marshal(defs)
	if err != nil {
		t.Fatal(err)
	}
	return len(sys)/4 + len(b)/4, len(defs)
}

// #310: at 8,192 the chat's first request (persona and tool schemas) fits
// and leaves the history room; the core tools are among those sent.
func TestChatFirstRequestFitsAt8192(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "hello there"})
	m, _, _ := chatAppWithContextLimit(t, srv, 8192)
	drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "hi"}},
		func(m tea.Model) bool { return lastAssistant(m) == "hello there" && turnIdle(m) }, 30*time.Second)
	prefix, n := requestPrefix(t, srv, 0)
	if room := compact.HistoryBudget(8192, prefix); room < 8192/4 {
		t.Fatalf("the first request's prefix is %d tokens (%d tools), leaving %d for history", prefix, n, room)
	}
	defs, _ := srv.Requests()[0].Body["tools"].([]any)
	var sent []string
	for _, d := range defs {
		sent = append(sent, d.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	for _, core := range []string{"read_file", "write_file", "patch_file", "search", "bash", "todo", "find_tools"} {
		if !slices.Contains(sent, core) {
			t.Errorf("core tool %s was not sent: %v", core, sent)
		}
	}
}

// At 32K and 200K the chat sends every tool, as before.
func TestChatSendsAllToolsOnLargeWindows(t *testing.T) {
	promptstest.Install(t)
	for _, w := range []int{32_768, 200_000} {
		srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "ok"})
		m, deps, _ := chatAppWithContextLimit(t, srv, w)
		drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "hi"}},
			func(m tea.Model) bool { return lastAssistant(m) == "ok" && turnIdle(m) }, 30*time.Second)
		_, n := requestPrefix(t, srv, 0)
		if all := len(deps.adapter.registry.GetTools(tools.ModeChat)) - 1; n != all { // submit_plan is plan mode's
			t.Fatalf("window %d: %d tools sent, want %d", w, n, all)
		}
		if countSystem(m, "too small for all the tool definitions") != 0 {
			t.Fatalf("window %d: a tool notice", w)
		}
	}
}

// The reduced tool set is announced once, at startup, naming context_limit
// (window 8300 is used by no other test).
func TestChatToolNoticeOnce(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "one"}, fakeprovider.Turn{Text: "two"})
	m, _, _ := chatAppWithContextLimit(t, srv, 8300)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "first"}},
		func(m tea.Model) bool { return lastAssistant(m) == "one" && turnIdle(m) }, 30*time.Second)
	m = drive(t, m, []tea.Msg{tui.SendMessageMsg{Content: "second"}},
		func(m tea.Model) bool { return lastAssistant(m) == "two" && turnIdle(m) }, 30*time.Second)
	if n := countSystem(m, "too small for all the tool definitions"); n != 1 {
		t.Fatalf("the tool notice shows %d times, want 1", n)
	}
}

// #310 review: GetSkills runs on a turn's goroutine and in View, while the
// Update goroutine replaces baseConfig (a loaded catalog, an endpoint
// switch). The tool fit must not read baseConfig there: under -race this
// reported a data race on it.
func TestGetSkillsDoesNotRaceEndpointUpdates(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t)
	_, deps, _ := chatAppWithContextLimit(t, srv, 8192)
	a := deps.adapter
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				a.client.GetSkills()
			}
		}
	}()
	for range 20 {
		a.RefreshServedModels()
		if err := a.SwitchEndpoint("openai"); err != nil {
			t.Error(err)
		}
	}
	close(stop)
	<-done
}

// The tool fit follows the window the prompt was composed for: a smaller
// context_limit reaches GetSkills once the window is followed, with no
// I/O and no baseConfig read on the caller's goroutine.
func TestToolWindowFollowsThePrompt(t *testing.T) {
	promptstest.Install(t)
	srv := fakeprovider.NewOpenAI(t)
	_, deps, _ := chatAppWithContextLimit(t, srv, 200000)
	a := deps.adapter
	all := len(a.client.GetSkills())
	a.baseConfig.ContextLimit = 8192
	a.FollowWindow()
	if n := len(a.client.GetSkills()); n >= all {
		t.Fatalf("after following 8,192: %d tools, want fewer than %d", n, all)
	}
}
