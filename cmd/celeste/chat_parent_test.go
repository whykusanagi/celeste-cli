package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/agent"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/fakeprovider"
)

// Subagents nest under the chat's Env (2.0 F2e), not an environment of
// their own: once the chat's Env has closed, a spawn fails instead of
// building a second one.
func TestChatSubagentsNestUnderTheChatEnv(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t, fakeprovider.Turn{Text: "never sent"})
	_, deps, ws := chatApp(t, srv)
	deps.env.Close()
	_, err := deps.adapter.subMgr.Spawn(context.Background(), "do it", ws)
	if err == nil || !strings.Contains(err.Error(), "parent environment is closed") {
		t.Fatalf("spawn after the chat's Env closed: err = %v, want it to nest under that Env", err)
	}
}

// /agent runners nest under the chat's Env too.
func TestChatAgentCommandNestsUnderTheChatEnv(t *testing.T) {
	srv := fakeprovider.NewOpenAI(t)
	_, deps, _ := chatApp(t, srv)
	orig := newAgentRunnerForTUI
	t.Cleanup(func() { newAgentRunnerForTUI = orig })
	got := make(chan agent.Options, 1)
	newAgentRunnerForTUI = func(_ *config.Config, o agent.Options, _, _ io.Writer) (agentRunnerAPI, error) {
		got <- o
		return &fakeAgentRunner{runGoalFn: func(context.Context, string) (*agent.RunState, error) {
			return &agent.RunState{Status: agent.StatusCompleted}, nil
		}}, nil
	}
	if _, err := deps.adapter.executeAgentCommand([]string{"do", "it"}); err != nil {
		t.Fatal(err)
	}
	if o := <-got; o.ParentEnv != deps.env {
		t.Fatalf("/agent ParentEnv = %v, want the chat's Env", o.ParentEnv)
	}
}
