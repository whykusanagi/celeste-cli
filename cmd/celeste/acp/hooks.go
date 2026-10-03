package acp

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/hooks"
)

// askHooks asks the editor's user about the untrusted repo hooks Setup
// skipped (ruling 9), at the session's first prompt, one
// session/request_permission per hook file. A trusted source is approved
// in the trust store (F0) and the Env is rebuilt with it before the prompt
// runs. Every source is asked about once per session; a cancel during the
// asks leaves the unanswered ones for the next prompt.
func (s *session) askHooks(ctx context.Context, a *Agent) {
	s.mu.Lock()
	if s.askedHooks {
		s.mu.Unlock()
		return
	}
	pending := append([]hooks.Source(nil), s.pendingHooks...)
	s.mu.Unlock()

	var approved []hooks.Source
	answered := 0
	for _, src := range pending {
		trust, ok := s.askHook(ctx, a, src)
		if !ok {
			break
		}
		answered++
		s.mu.Lock()
		if trust {
			s.trusted = append(s.trusted, src)
		} else {
			s.skipped = append(s.skipped, src)
		}
		s.mu.Unlock()
		if trust {
			approved = append(approved, src)
		}
	}
	s.mu.Lock()
	s.pendingHooks = append([]hooks.Source(nil), s.pendingHooks[answered:]...)
	s.askedHooks = len(s.pendingHooks) == 0
	s.mu.Unlock()
	if len(approved) == 0 {
		return
	}
	store := hooks.LoadTrust(a.deps.Home)
	for _, src := range approved {
		if err := store.Approve(src); err != nil {
			a.logf("acp: session %s: storing trust for %s: %v (trusted for this session only)", s.id, src.Path, err)
		}
	}
	if rerr := s.rebuildEnv(context.WithoutCancel(ctx), a); rerr != nil {
		a.logf("acp: session %s: rebuilding the session with the trusted hooks: %s", s.id, rerr.Message)
	}
}

// askHook sends one hook file's question. ok is false when the question
// went unanswered (the prompt was cancelled or the editor went away).
func (s *session) askHook(ctx context.Context, a *Agent, src hooks.Source) (trust, ok bool) {
	rules := src.Kind == hooks.KindRepoStreamRules
	path := strings.TrimSuffix(src.Path, "#stream-rules")
	if rel, err := filepath.Rel(s.cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
		path = rel
	}
	var desc strings.Builder
	hooks.DescribeSource(&desc, src)
	title := fmt.Sprintf("Run repository hooks from %s?", path)
	warning := "These commands run on this machine with your permissions."
	trustName := "Trust these hooks"
	if rules {
		title = fmt.Sprintf("Apply the stream rules in %s?", path)
		warning = "These rules can stop replies, re-run turns and add instructions the model follows."
		trustName = "Trust these rules"
	}
	params := RequestPermissionParams{
		SessionID: s.id,
		ToolCall: ToolCallUpdate{
			ToolCallID: fmt.Sprintf("hooks_%d", s.permSeq.Add(1)),
			Title:      title,
			Kind:       ToolKindExecute,
			Status:     ToolStatusPending,
			Content:    []ToolCallContent{TextToolContent(desc.String() + "\n" + warning)},
			Locations:  []Location{{Path: strings.TrimSuffix(src.Path, "#stream-rules")}},
		},
		Options: []PermissionOption{
			{OptionID: OptionAllowAlways, Name: trustName, Kind: OptionAllowAlways},
			{OptionID: OptionRejectOnce, Name: "Skip", Kind: OptionRejectOnce},
		},
	}
	var out RequestPermissionResult
	if err := a.call(ctx, "session/request_permission", params, &out); err != nil {
		if ctx.Err() == nil {
			a.logf("acp: session %s: asking about hooks in %s: %v", s.id, src.Path, err)
		}
		return false, false
	}
	if ctx.Err() != nil || out.Outcome.Outcome != OutcomeSelected {
		return false, false
	}
	return out.Outcome.OptionID == OptionAllowAlways, true
}

// rebuildEnv runs the session's Setup again (trusted hooks now load; a
// new cwd or new editor MCP servers apply) and closes the old Env. A
// failure keeps the old Env. The small-window notice is not shown twice.
// Only the goroutine holding the session (s.running) calls it.
func (s *session) rebuildEnv(ctx context.Context, a *Agent) *RPCError {
	s.mu.Lock()
	old, shown := s.env, s.notice == ""
	s.mu.Unlock()
	if rerr := a.setupEnv(ctx, s); rerr != nil {
		return rerr
	}
	if shown {
		s.mu.Lock()
		s.notice = ""
		s.mu.Unlock()
	}
	if old != nil {
		old.Close()
	}
	return nil
}
