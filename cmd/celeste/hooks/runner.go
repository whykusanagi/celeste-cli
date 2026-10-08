package hooks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Answer is a person's reply to a trust question.
type Answer int

const (
	// AnswerLater is no answer (EOF, a cancelled prompt, a question queued
	// for later): the source does not run and nothing is recorded.
	AnswerLater Answer = iota
	// AnswerYes approves the source; the approval is recorded.
	AnswerYes
	// AnswerNo declines it; the decline is recorded, so it is not asked
	// about again until it changes (#411).
	AnswerNo
)

// ApproveFunc asks a person whether an untrusted source may run. Only
// interactive callers supply one. It is never asked about a Trusted or
// Declined source.
type ApproveFunc func(src Source, status TrustStatus) Answer

// Options configures Load.
type Options struct {
	Workspace string
	Home      string // "" = os.UserHomeDir()
	SessionID string
	// Approve is nil in non-interactive modes: untrusted sources are then
	// skipped with a warning and never approved.
	Approve ApproveFunc
	Warn    func(string) // nil = stderr
}

// Outcome is the combined verdict of every hook that ran for one event.
type Outcome struct {
	Decision          Decision // Allow when no hook matched
	Reason            string
	AdditionalContext string         // returned to the model
	UpdatedInput      map[string]any // PreToolUse only; nil = unchanged
}

// ToolResponse is a tool's result as PostToolUse hooks see it.
type ToolResponse struct {
	Content string
	Error   bool
}

type boundHook struct {
	def    Definition
	source string // Source.Path, for messages
	dir    string // where the process runs
}

// Runner runs a session's hooks. It is immutable after Load and safe for
// concurrent use. A nil *Runner allows everything.
type Runner struct {
	hooks     []boundHook
	workspace string
	sessionID string
	warn      func(string)
}

// Load discovers the workspace's hooks and keeps the ones allowed to run:
// global sources always, repo sources when trusted or approved now. On error,
// callers must warn the user and treat hooks as disabled; global guards are
// not running in that state.
func Load(opts Options) (*Runner, error) {
	warn := opts.Warn
	if warn == nil {
		warn = func(s string) { fmt.Fprintln(os.Stderr, s) }
	}
	home := opts.Home
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("hooks: no home directory: %w", err)
		}
		home = h
	}
	ws, err := filepath.Abs(opts.Workspace)
	if err != nil {
		return nil, err
	}
	sources, warnings, err := Discover(ws, home)
	if err != nil {
		return nil, err
	}
	for _, w := range warnings {
		warn(w)
	}
	r := &Runner{workspace: ws, sessionID: opts.SessionID, warn: warn}
	var store *TrustStore
	for _, src := range sources {
		if src.Kind == KindRepoStreamRules {
			continue // runs nothing; the rules package checks its trust
		}
		if !src.Global() {
			if store == nil {
				store = LoadTrust(home)
				if err := store.Err(); err != nil {
					warn(fmt.Sprintf("hooks: %v; repo hooks stay untrusted until it is fixed or removed", err))
				}
			}
			if !r.admit(src, store, opts.Approve) {
				continue
			}
		}
		dir := src.Root
		if dir == "" {
			dir = ws
		}
		for _, d := range src.Hooks {
			r.hooks = append(r.hooks, boundHook{def: d, source: src.Path, dir: dir})
		}
	}
	return r, nil
}

// admit decides whether a repo source runs. Without an approver it never
// does: only a person approves hooks.
func (r *Runner) admit(src Source, store *TrustStore, approve ApproveFunc) bool {
	run, why, err := Decide(store, src, approve)
	if err != nil {
		r.warn(fmt.Sprintf("hooks: %v", err))
	}
	if !run {
		r.warn(fmt.Sprintf("hooks: skipping %d hook(s) in %s: %s; run `celeste hooks trust` to approve them", len(src.Hooks), strconv.Quote(src.Path), why))
	}
	return run
}

// Has reports whether any loaded hook listens for ev.
func (r *Runner) Has(ev Event) bool {
	if r == nil {
		return false
	}
	for _, h := range r.hooks {
		if h.def.Event == ev {
			return true
		}
	}
	return false
}

func (r *Runner) PreToolUse(ctx context.Context, tool string, input map[string]any) Outcome {
	return r.run(ctx, EventPreToolUse, tool, map[string]any{"tool_name": tool, "tool_input": input})
}

func (r *Runner) PostToolUse(ctx context.Context, tool string, input map[string]any, res ToolResponse) Outcome {
	content, truncated := res.Content, false
	if len(content) > maxToolResponse {
		content, truncated = truncate(content, maxToolResponse), true
	}
	return r.run(ctx, EventPostToolUse, tool, map[string]any{
		"tool_name": tool, "tool_input": input,
		"tool_response": map[string]any{"content": content, "error": res.Error, "truncated": truncated},
	})
}

// SessionStart: source is "startup" or "resume".
func (r *Runner) SessionStart(ctx context.Context, source string) Outcome {
	return r.run(ctx, EventSessionStart, "", map[string]any{"source": source})
}

// UserPromptSubmit: any Decision other than Allow blocks the prompt.
func (r *Runner) UserPromptSubmit(ctx context.Context, prompt string) Outcome {
	return r.run(ctx, EventUserPromptSubmit, "", map[string]any{"prompt": prompt})
}

// PreCompact: trigger is "manual" or "auto"; anything but Allow blocks the
// compaction; AdditionalContext is extra summary instructions.
func (r *Runner) PreCompact(ctx context.Context, trigger, instructions string) Outcome {
	return r.run(ctx, EventPreCompact, "", map[string]any{"trigger": trigger, "custom_instructions": instructions})
}

func (r *Runner) PostCompact(ctx context.Context, trigger, summary string) Outcome {
	return r.run(ctx, EventPostCompact, "", map[string]any{"trigger": trigger, "summary": summary})
}

// Stop: Deny means "don't stop; continue with Reason" (acted on by F2).
func (r *Runner) Stop(ctx context.Context, lastMessage string) Outcome {
	return r.run(ctx, EventStop, "", map[string]any{"last_message": lastMessage})
}

// SubagentStop: same rule as Stop, for a subagent's final message.
func (r *Runner) SubagentStop(ctx context.Context, agentID, lastMessage string) Outcome {
	return r.run(ctx, EventSubagentStop, "", map[string]any{"agent_id": agentID, "last_message": lastMessage})
}

// run executes matching hooks in order. The first deny wins and stops the
// chain; ask is kept; updatedInput feeds later hooks; contexts are joined.
// A failed hook denies gating events (fail closed) and only warns otherwise.
func (r *Runner) run(ctx context.Context, ev Event, tool string, payload map[string]any) Outcome {
	out := Outcome{Decision: Allow}
	if r == nil {
		return out
	}
	payload["event"] = string(ev)
	payload["session_id"] = r.sessionID
	payload["workspace"] = r.workspace
	var contexts []string
	for _, h := range r.hooks {
		if !h.def.matches(ev, tool) {
			continue
		}
		payload["project_dir"] = h.dir
		res := runHook(ctx, h.def, h.dir, payload)
		if res.failed != "" {
			// res.failed can carry the hook's own stderr: quote it if it
			// holds control or bidi characters.
			failed := SafeText(res.failed)
			r.warnFor(ctx)(fmt.Sprintf("hooks: %s hook from %s failed: %s", ev, strconv.Quote(h.source), failed))
			if ev.gating() {
				out.Decision, out.Reason = Deny, "hook failed: "+failed
				break
			}
			continue
		}
		if res.context != "" {
			contexts = append(contexts, res.context)
		}
		if res.updated != nil && ev == EventPreToolUse {
			out.UpdatedInput = res.updated
			payload["tool_input"] = res.updated
		}
		if res.decision == Deny && ev.decides() {
			out.Decision, out.Reason = Deny, SafeText(res.reason)
			break
		}
		if res.decision == Ask && ev.decides() && out.Decision == Allow {
			out.Decision, out.Reason = Ask, SafeText(res.reason)
		}
	}
	out.AdditionalContext = truncate(strings.Join(contexts, "\n"), maxContext)
	return out
}

type warnKey struct{}

// WithWarn returns ctx carrying warn as the sink for warnings raised by hooks
// run under it. A Runner shared by concurrent callers (MCP chat's cached Env)
// sends each caller's hook failures to that caller, not to the sink it was
// loaded with. A nil warn leaves ctx unchanged.
func WithWarn(ctx context.Context, warn func(string)) context.Context {
	if warn == nil {
		return ctx
	}
	return context.WithValue(ctx, warnKey{}, warn)
}

// warnFor returns ctx's warn sink, or the Runner's own when ctx has none.
func (r *Runner) warnFor(ctx context.Context) func(string) {
	if w, ok := ctx.Value(warnKey{}).(func(string)); ok {
		return w
	}
	return r.warn
}

// DisabledWarning is the one warning every adopter shows when Load fails.
func DisabledWarning(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("hooks disabled: %v (no hooks run this session, including global guards)", err)
}
