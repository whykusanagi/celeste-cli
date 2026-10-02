package hooks

import "testing"

func TestStopContinuation(t *testing.T) {
	chat := StopScope{Event: "Stop", Actor: "the chat", Unit: "turn"}
	agent := StopScope{Event: "SubagentStop", Actor: "the agent", Unit: "run"}
	cases := []struct {
		name      string
		out       Outcome
		continued bool
		turnsLeft int
		scope     StopScope
		instr     string
		warning   string
	}{
		{"allow ends", Outcome{Decision: Allow, Reason: "x"}, false, 3, chat, "", ""},
		{"ask ends", Outcome{Decision: Ask, Reason: "x"}, false, 3, chat, "", ""},
		{"deny continues with reason", Outcome{Decision: Deny, Reason: "  run the tests "}, false, 3, chat, "  run the tests ", ""},
		{"deny without reason", Outcome{Decision: Deny, Reason: " \n"}, false, 1, chat, "Continue.", ""},
		{"second deny ignored", Outcome{Decision: Deny, Reason: "again"}, true, 3, chat, "",
			"a Stop hook asked the chat to continue again; ignored (one continuation per turn)"},
		{"no turns left", Outcome{Decision: Deny, Reason: "more"}, false, 0, chat, "",
			"a Stop hook asked the chat to continue, but the turn has no turns left"},
		{"subagent wording", Outcome{Decision: Deny}, true, 0, agent, "",
			"a SubagentStop hook asked the agent to continue again; ignored (one continuation per run)"},
		{"subagent no turns", Outcome{Decision: Deny}, false, -1, agent, "",
			"a SubagentStop hook asked the agent to continue, but the run has no turns left"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			instr, warning := StopContinuation(c.out, c.continued, c.turnsLeft, c.scope)
			if instr != c.instr || warning != c.warning {
				t.Fatalf("got (%q, %q), want (%q, %q)", instr, warning, c.instr, c.warning)
			}
		})
	}
}
