package llm

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/compact"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tui"
)

// bigTool is a tool with a ~250-token schema.
type bigTool struct{ modeStubTool }

func (b bigTool) Description() string {
	return "Does " + b.name + ". " + strings.Repeat("Explains itself at length. ", 30)
}

func bigRegistry() *tools.Registry {
	r := tools.NewRegistry()
	for _, n := range compact.CoreTools {
		r.Register(bigTool{modeStubTool{n}})
	}
	for i := range 40 {
		r.Register(bigTool{modeStubTool{fmt.Sprintf("extra_%02d", i)}})
	}
	return r
}

func fitClient(window int) *Client {
	c := &Client{registry: bigRegistry(), config: &Config{BaseURL: "https://api.example.com/v1", Model: "m", ContextLimit: window}}
	c.SetSystemPrompt(strings.Repeat("s", 3_800*4))
	return c
}

// #310: the client sends a fitted tool set on a small window and the full
// one on a large window, and leaves a one-time notice when it reduced.
func TestGetSkillsFitsTheWindow(t *testing.T) {
	small := fitClient(8_192)
	defs := small.GetSkills()
	if !shortened(defs) {
		t.Fatal("8,192: the tool set was sent as it is")
	}
	if prefix := 3_800 + compact.DefinitionTokens(defs); compact.HistoryBudget(8_192, prefix) < 8_192/4 {
		t.Fatalf("8,192: prefix %d leaves no room for history", prefix)
	}
	n := small.TakeToolNotice()
	if !strings.Contains(n, "context_limit") {
		t.Fatalf("notice %q", n)
	}
	if small.TakeToolNotice() != "" {
		t.Fatal("the notice was taken twice")
	}
	for _, w := range []int{32_768, 200_000} {
		big := fitClient(w)
		if defs := big.GetSkills(); len(defs) != 49 || shortened(defs) {
			t.Fatalf("%d: %d tools sent, want all 49 as they are", w, len(defs))
		}
		if big.TakeToolNotice() != "" {
			t.Fatalf("%d: a notice without a reduction", w)
		}
	}
}

// A tool find_tools activated is sent on the small window too.
func TestGetSkillsKeepsActivatedTools(t *testing.T) {
	c := fitClient(8_192)
	c.registry.Activate("extra_39")
	if !slices.ContainsFunc(c.GetSkills(), func(d tui.SkillDefinition) bool { return d.Name == "extra_39" }) {
		t.Fatal("the activated tool was dropped")
	}
}

func TestConfigFromCarriesContextLimit(t *testing.T) {
	if got := ConfigFrom(&config.Config{ContextLimit: 12_345}).ContextLimit; got != 12_345 {
		t.Fatalf("ContextLimit %d", got)
	}
}

// A window func, when set, wins over the config (the chat follows its
// base config's context_limit).
func TestSetWindowFunc(t *testing.T) {
	c := fitClient(200_000)
	c.SetWindowFunc(func() int { return 8_192 })
	if !shortened(c.GetSkills()) {
		t.Fatal("the window func was ignored")
	}
}

// shortened reports whether GetSkills sent short descriptions (a fit).
func shortened(defs []tui.SkillDefinition) bool {
	return len(defs) > 0 && !strings.Contains(defs[0].Description, "at length")
}

// #310 review: the notice is per session, not per process: in ACP one
// process serves many editor sessions, and each is told its tools were
// reduced (window 8,400 is used by no other test).
func TestToolNoticePerClient(t *testing.T) {
	for i := range 2 {
		c := fitClient(8_400)
		c.GetSkills()
		if c.TakeToolNotice() == "" {
			t.Fatalf("client %d was not told", i)
		}
	}
}

// The notice is the last fit's: a reduced fit seen in View before the
// window grew leaves nothing behind for the turn on the larger window.
func TestToolNoticeFollowsTheLastFit(t *testing.T) {
	c := fitClient(200_000)
	window := 8_450
	c.SetWindowFunc(func() int { return window })
	c.GetSkills()
	window = 200_000
	c.GetSkills()
	if n := c.TakeToolNotice(); n != "" {
		t.Fatalf("a stale notice: %q", n)
	}
}
