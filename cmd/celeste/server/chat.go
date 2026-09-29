package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/grimoire"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/loop"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// chatMaxTurns is MCP chat's turn cap, unchanged from the pre-loop server.
const chatMaxTurns = 25

// chatLimits are MCP chat's loop limits: the spec defaults (25 turns,
// identical-call guard 3, progress guard 6, spill at 128 KiB, 45 s per tool)
// with no per-turn call cap, no invalid-argument cap and no <tool_call> text
// parsing, none of which MCP chat ever had.
func chatLimits() loop.Limits {
	lim := loop.DefaultLimits()
	lim.MaxTurns = chatMaxTurns
	return lim
}

// runChatMode runs MCP `celeste` mode:"chat": one prompt through the unified
// loop (2.0 F2b), on the workspace's cached ModeMCPChat Env. That Env runs
// Trust mode with the user's deny rules, hooks without repo-hook approval,
// and global MCP servers only. The call gets its own Loop and history. The
// result is one text block, as before, with any warnings after the reply.
func (s *Server) runChatMode(ctx context.Context, cfg *config.Config, prompt, workspace string) ([]ContentBlock, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("chat error: %w", err)
	}
	initGrimoire(workspace)
	var warns warnSink
	ce, err := s.chatEnvs.acquire(cfg, workspace, &warns)
	if err != nil {
		return nil, fmt.Errorf("chat setup: %w%s", err, warns.section())
	}
	defer s.chatEnvs.release(ce, &warns)
	env := ce.env

	system := env.SystemPrompt("", nil)
	sessionID := fmt.Sprintf("mcp-chat-%d", time.Now().UnixNano())
	l := newChatLoop(newChatClient(cfg, env.Registry, system), env, sessionID)
	text, err := runChat(ctx, l, prompt)
	if err != nil {
		return nil, fmt.Errorf("chat error: %w%s", err, warns.section())
	}
	return []ContentBlock{{Type: "text", Text: text + warns.section()}}, nil
}

// grimoireInitMu serializes the grimoire auto-init. Two first calls on one
// workspace would otherwise both write .grimoire, and a call that stamps the
// Env inputs between those writes sees a different .grimoire and builds a
// second Env.
var grimoireInitMu sync.Mutex

// initGrimoire writes a .grimoire into workspace if it has none (kept until
// W4). It returns once the file exists, so the chat Env stamp that follows
// sees it.
func initGrimoire(workspace string) {
	grimoireInitMu.Lock()
	defer grimoireInitMu.Unlock()
	if _, err := os.Stat(filepath.Join(workspace, ".grimoire")); os.IsNotExist(err) {
		_, _ = grimoire.Init(workspace)
	}
}

// newChatClient is the pre-loop server's client (same config fields) on the
// Env's registry. The tool mode stays tools.ModeChat.
func newChatClient(cfg *config.Config, reg *tools.Registry, system string) *llm.Client {
	client := llm.NewClient(&llm.Config{
		APIKey:  cfg.APIKey,
		BaseURL: cfg.BaseURL,
		Model:   cfg.Model,
		Timeout: cfg.GetTimeout(),
	}, reg)
	client.SetSystemPrompt(system)
	return client
}

// newChatLoop builds one call's loop. There is no Gate: an Ask (only a
// hook-forced one, since Trust mode asks for nothing else) is denied
// headless, as before. Tool hooks already run in env.Registry.
func newChatLoop(client *llm.Client, env *loop.Env, sessionID string) *loop.Loop {
	return &loop.Loop{
		Client:    client,
		Tools:     env.Registry,
		Limits:    chatLimits(),
		SessionID: sessionID, // names this call's spill directory
	}
}

// runChat runs the loop for one prompt and returns the tool-result text.
func runChat(ctx context.Context, l *loop.Loop, prompt string) (string, error) {
	lim := l.Limits
	var claims chatClaims
	_, res, err := runObserved(ctx, l, []loop.Message{{Role: "user", Content: prompt, Timestamp: time.Now()}}, &claims)
	if err != nil {
		return "", err
	}
	return chatText(res, lim, &claims), nil
}

// runObserved runs l once and feeds its events to claims. The event channel
// is unbuffered, so the consumer reads until EventDone. Only the consumer
// touches claims while Run executes, and <-done orders that before return.
func runObserved(ctx context.Context, l *loop.Loop, history []loop.Message, claims *chatClaims) ([]loop.Message, loop.Result, error) {
	events := l.Events()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range events {
			claims.observe(ev)
			if ev.Kind == loop.EventDone {
				return
			}
		}
	}()
	msgs, res, err := l.Run(ctx, history)
	<-done
	return msgs, res, err
}

// chatText is the tool result for a run that ended without an error. The
// stop texts are the pre-loop server's, verbatim (the plugin and the F1
// tests match on them).
func chatText(res loop.Result, lim loop.Limits, claims *chatClaims) string {
	switch res.StopReason {
	case loop.StopIdentical:
		return fmt.Sprintf("Stopped: the model made the identical tool call %d times in a row (stuck loop).", lim.IdenticalCalls)
	case loop.StopProgress:
		return fmt.Sprintf("Stopped: the model called the same tool with no new result %d turns in a row (stuck loop).", lim.NoProgressTurns)
	case loop.StopCap:
		return "Tool loop limit reached"
	default:
		return claims.strip(strings.TrimSpace(res.FinalText))
	}
}

// chatClaims records whether generate_speech and spawn_agent really ran
// (and succeeded) in this call, so a fabricated "Audio saved:" or "subagent
// spawned (id: …)" claim in the final reply can be replaced. These are the
// pre-loop server's ttsRan/spawnRan flags, now derived from loop events.
type chatClaims struct{ tts, spawn bool }

func (c *chatClaims) observe(ev loop.Event) {
	if ev.Kind != loop.EventToolResult || ev.IsError {
		return
	}
	switch ev.Call.Name {
	case "generate_speech":
		c.tts = true
	case "spawn_agent":
		c.spawn = true
	}
}

func (c *chatClaims) strip(text string) string {
	text = llm.StripUnbackedAudioClaim(text, c.tts)
	return llm.StripUnbackedSpawnClaim(text, c.spawn)
}
