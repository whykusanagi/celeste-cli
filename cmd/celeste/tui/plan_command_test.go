package tui

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// planClient is a turn client with plan mode (PlanModer).
type planClient struct {
	*fakeToolLLMClient
	on        bool
	goal      string
	shown     string
	untracked int
}

func (c *planClient) SetPlanMode(on bool, goal string) { c.on, c.goal = on, goal }
func (c *planClient) PlanMode() bool                   { return c.on }
func (c *planClient) ShowPlan() string                 { return c.shown }
func (c *planClient) UntrackPlan()                     { c.untracked++ }

func newPlanTestApp() (AppModel, *planClient) {
	client := &planClient{fakeToolLLMClient: &fakeToolLLMClient{skills: []SkillDefinition{{Name: "tool_a"}}}}
	m := NewApp(client)
	m.skillsEnabled = true
	m.statusLine = m.statusLine.SetWidth(160)
	return m, client
}

func lastSystemText(m AppModel) string {
	msgs := m.chat.messages
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "system" {
			return msgs[i].Content
		}
	}
	return ""
}

func TestPlanCommandTogglesPlanMode(t *testing.T) {
	m, client := newPlanTestApp()
	assert.NotContains(t, m.statusLineView(0), "PLAN")
	m, _ = step(t, m, SendMessageMsg{Content: "/plan"})
	assert.True(t, client.on)
	assert.Empty(t, client.goal)
	assert.Empty(t, client.turns, "/plan alone starts no turn")
	assert.Equal(t, "Plan mode on: read-only tools until you approve a plan (/plan off to leave)", lastSystemText(m))
	assert.Contains(t, m.statusLineView(0), "PLAN")

	m, _ = step(t, m, SendMessageMsg{Content: "/plan off"})
	assert.False(t, client.on)
	assert.Contains(t, lastSystemText(m), "Plan mode off")
	assert.NotContains(t, m.statusLineView(0), "PLAN")

	// The status follows the client: an approval mid-turn clears it.
	client.on = true
	assert.Contains(t, m.statusLineView(0), "PLAN")
	client.on = false
	assert.NotContains(t, m.statusLineView(0), "PLAN")
}

func TestPlanGoalSendsThePrompt(t *testing.T) {
	m, client := newPlanTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "/plan add caching"})
	assert.True(t, client.on)
	assert.Equal(t, "add caching", client.goal)
	require.Len(t, client.turns, 1)
	h := client.turns[0].req.History
	require.GreaterOrEqual(t, len(h), 2)
	last, before := h[len(h)-1], h[len(h)-2]
	assert.Equal(t, "user", last.Role)
	assert.Equal(t, "add caching", last.Content)
	assert.Equal(t, PlanModeInstruction, before.Content)
	hidden, _ := before.Metadata["hidden"].(bool)
	assert.True(t, hidden, "the plan-mode instruction is hidden")
	assert.Equal(t, "Planning...", m.status.text)
}

// Every prompt while plan mode is on carries the instruction; none after.
func TestPlanModePromptsCarryTheInstruction(t *testing.T) {
	m, client := newPlanTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "/plan"})
	m, _ = step(t, m, SendMessageMsg{Content: "look around"})
	require.Len(t, client.turns, 1)
	h := client.turns[0].req.History
	require.GreaterOrEqual(t, len(h), 2)
	assert.Equal(t, PlanModeInstruction, h[len(h)-2].Content)
	m, _ = feed(t, m, TurnDoneMsg{Stop: "done"})

	client.on = false // approved during the turn
	m, _ = step(t, m, SendMessageMsg{Content: "go on"})
	require.Len(t, client.turns, 2)
	h = client.turns[1].req.History
	assert.NotEqual(t, PlanModeInstruction, h[len(h)-2].Content)
	n := 0
	for _, msg := range h {
		if msg.Content == PlanModeInstruction {
			n++
		}
	}
	assert.Equal(t, 1, n, "only the plan-mode prompt carried the instruction")
	_ = m
}

func TestPlanShow(t *testing.T) {
	m, client := newPlanTestApp()
	client.shown = "Plan: ship\n[ ] 1. write tests"
	m, _ = step(t, m, SendMessageMsg{Content: "/plan show"})
	assert.Equal(t, client.shown, lastSystemText(m))
	assert.False(t, client.on, "/plan show does not enter plan mode")
	assert.Empty(t, client.turns)
}

// Without plan-mode support (a client that is not a PlanModer), /plan says
// so and starts nothing.
func TestPlanCommandUnavailable(t *testing.T) {
	m, client := newQueueTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "/plan add caching"})
	assert.Empty(t, client.turns)
	assert.True(t, strings.Contains(lastSystemText(m), "unavailable"), lastSystemText(m))
}

// /rewind takes the plan-mode instruction back with its prompt.
func TestRewindTakesThePlanInstructionWithItsPrompt(t *testing.T) {
	msgs := []ChatMessage{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: PlanModeInstruction, Metadata: map[string]any{"hidden": true}},
		{Role: "user", Content: "plan it"},
		{Role: "assistant", Content: "planning"},
	}
	idx, err := rewindTarget(msgs, 1)
	require.NoError(t, err)
	assert.Equal(t, 2, idx)
}

// /plan cancel was 1.x's way out of a plan; it must not enter plan mode
// and send "cancel" to the model.
func TestPlanCancelIsGone(t *testing.T) {
	for _, on := range []bool{false, true} {
		m, client := newPlanTestApp()
		client.on, client.goal = on, "keep me"
		m, _ = step(t, m, SendMessageMsg{Content: "/plan cancel"})
		assert.Equal(t, on, client.on, "/plan cancel leaves the mode as it was")
		assert.Equal(t, "keep me", client.goal)
		assert.Empty(t, client.turns, "/plan cancel sends nothing to the model")
		assert.Equal(t, "/plan cancel is gone: use /plan off (delete .celeste/plan.json to drop an approved plan)", lastSystemText(m))
	}
}

// /plan with no goal while plan mode is on keeps the goal /plan <goal> set.
func TestPlanAgainKeepsTheGoal(t *testing.T) {
	m, client := newPlanTestApp()
	client.on, client.goal = true, "add caching" // set by /plan add caching
	m, _ = step(t, m, SendMessageMsg{Content: "/plan"})
	assert.True(t, client.on)
	assert.Equal(t, "add caching", client.goal)
	assert.Equal(t, "Plan mode is already on.", lastSystemText(m))
}

// A session saved mid-plan says so on resume (plan mode itself starts off),
// so the model's plan-mode instructions are not left unexplained.
func TestResumedPlanSessionSaysPlanModeWasOn(t *testing.T) {
	hidden := ChatMessage{Role: "user", Content: PlanModeInstruction, Metadata: map[string]any{"hidden": true}}
	prompt := ChatMessage{Role: "user", Content: "add caching"}
	reply := ChatMessage{Role: "assistant", Content: "looking"}
	approved := ChatMessage{Role: "tool", Name: "submit_plan", Content: "Plan approved: 2 todo items created (ids 1–2). Plan mode is off; start with step 1."}
	for _, tc := range []struct {
		name string
		msgs []ChatMessage
		want bool
	}{
		{"mid-plan", []ChatMessage{hidden, prompt, reply}, true},
		{"approved", []ChatMessage{hidden, prompt, approved, reply}, false},
		{"later prompt outside plan mode", []ChatMessage{hidden, prompt, reply, {Role: "user", Content: "thanks"}}, false},
		{"never planned", []ChatMessage{prompt, reply}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _ := newPlanTestApp()
			m = m.WithMessages(tc.msgs)
			assert.Equal(t, tc.want, lastSystemText(m) == planWasOnText, lastSystemText(m))
		})
	}
}

// Replacing the conversation stops the approved plan's progress reminder:
// a new or resumed chat is not carrying out the old plan (#325).
func TestReplacingTheChatUntracksThePlan(t *testing.T) {
	for _, tc := range []struct {
		name, cmd string
		resume    bool
	}{
		{"/clear", "/clear", false},
		{"legacy clear", "clear", false},
		{"/session new", "/session new", false},
		{"/session clear", "/session clear", false},
		{"/session resume", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, _, other := newSessionTestApp(t)
			client := &planClient{fakeToolLLMClient: &fakeToolLLMClient{}}
			m.llmClient = client
			cmd := tc.cmd
			if tc.resume {
				cmd = "/session resume " + other.ID
			}
			m, _ = step(t, m, SendMessageMsg{Content: cmd})
			assert.Positive(t, client.untracked, sessChatText(m))
		})
	}
	// Commands that keep the conversation keep the plan.
	m, client := newPlanTestApp()
	m, _ = step(t, m, SendMessageMsg{Content: "/plan show"})
	_ = m
	assert.Zero(t, client.untracked)
}
