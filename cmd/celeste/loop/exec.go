package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	ctxmgr "github.com/whykusanagi/celeste-cli/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// pending is one tool call on its way to a result message.
type pending struct {
	call    ToolCall
	content string
	isError bool
	settled bool // has its result without running (bad args, unknown tool)
	ran     bool // went through the registry
}

func (p *pending) settle(content string) {
	p.content, p.isError, p.settled = content, true, true
}

type callsOutcome struct {
	messages   []Message
	anyInvalid bool   // some call had corrupt or invalid argument JSON
	resultSig  string // name|result of every call, for the progress guard
}

// runCalls runs one turn's calls and returns their result messages in call
// order. Every call gets exactly one result, so the history stays paired
// even when the run is interrupted.
func (l *Loop) runCalls(ctx context.Context, calls []llm.ToolCallResult, lim Limits) callsOutcome {
	var out callsOutcome
	ps := make([]*pending, len(calls))
	for i, c := range calls {
		p := &pending{call: ToolCall{ID: c.ID, Name: c.Name}}
		ps[i] = p
		switch {
		case c.ArgsError != "":
			out.anyInvalid = true
			p.settle(fmt.Sprintf(`{"error": true, "message": "stream-corrupted tool arguments", "detail": %q, "tool": %q}`, c.ArgsError, c.Name))
		case !json.Valid([]byte(argsOrEmpty(c.Arguments))):
			out.anyInvalid = true
			p.settle(fmt.Sprintf(`{"error": true, "message": "invalid tool arguments JSON", "tool": %q}`, c.Name))
		default:
			input, err := parseInput(c.Arguments)
			if err != nil {
				p.settle(errorEnvelope(c.Name, "failed to parse arguments: "+err.Error()))
				break
			}
			p.call.Input = input
			if _, ok := l.Tools.Get(c.Name); !ok {
				p.settle(errorEnvelope(c.Name, fmt.Sprintf("tool '%s' not found", c.Name)))
			}
		}
		l.emit(Event{Kind: EventToolStart, Call: p.call})
	}

	l.execute(ctx, ps, lim)

	sigs := make([]string, 0, len(ps))
	for i, p := range ps {
		sigs = append(sigs, p.call.Name+"|"+p.content)
		content := l.spill(p.content, p.call.ID, i, lim)
		out.messages = append(out.messages, toolMessage(p.call, content))
		l.emit(Event{Kind: EventToolResult, Call: p.call, Text: content, IsError: p.isError})
	}
	out.resultSig = strings.Join(sigs, ",")
	return out
}

// execute runs unsettled calls: consecutive concurrency-safe calls run
// together through one StreamingToolExecutor; any other call runs alone, so
// a write always finishes before a later read starts.
func (l *Loop) execute(ctx context.Context, ps []*pending, lim Limits) {
	var group []*pending
	flush := func() {
		if len(group) > 0 {
			l.runGroup(ctx, group, lim)
			group = nil
		}
	}
	for _, p := range ps {
		if p.settled {
			continue
		}
		if t, ok := l.Tools.Get(p.call.Name); ok && t.IsConcurrencySafe(p.call.Input) {
			group = append(group, p)
			continue
		}
		flush()
		l.runGroup(ctx, []*pending{p}, lim)
	}
	flush()
}

func (l *Loop) runGroup(ctx context.Context, group []*pending, lim Limits) {
	ex := tools.NewStreamingToolExecutorWithContext(ctx, l.Tools)
	defer ex.Cancel()
	// Executor call IDs are group indexes: model IDs may repeat or be empty.
	ex.SetExecFunc(func(ectx context.Context, _ string, t tools.Tool, input map[string]any, _ chan<- tools.ProgressEvent) (tools.ToolResult, error) {
		return l.invoke(ectx, t, input, lim)
	})
	for i, p := range group {
		b, _ := json.Marshal(p.call.Input)
		ex.AddTool(strconv.Itoa(i), p.call.Name, string(b))
	}
	ex.Done()
	for _, r := range ex.Wait() {
		i, _ := strconv.Atoi(r.CallID)
		p := group[i]
		p.ran = true
		p.content, p.isError = formatResult(p.call.Name, r)
	}
}

// invoke runs one call through the registry, so tool hooks (F0), the
// permission checker and its deny rules always apply, exactly once.
//
// The tool's timeout always goes through tools.WithExecTimeout: the registry
// applies it to the tool alone, after the hooks and the permission prompt,
// so a slow PreToolUse hook never eats the tool's time. A tool that ignores
// its context is abandoned by a watchdog at tool timeout + HookBudget. With
// a Gate the approval wait is unbounded, so gated runs get no watchdog;
// only cancelling the run ends the wait (known limitation, fixed in F2d).
func (l *Loop) invoke(ctx context.Context, t tools.Tool, input map[string]any, lim Limits) (tools.ToolResult, error) {
	timeout := tools.TimeoutFor(t, lim.ToolTimeout)
	cctx, cancel := context.WithCancel(tools.WithExecTimeout(ctx, timeout))
	defer cancel()
	watchdog := timeout + lim.HookBudget
	if l.Gate != nil {
		watchdog = 0
		// This run's Gate answers the registry's Ask, including one forced
		// by a PreToolUse hook's "ask". Serialized: parallel-safe calls in
		// one batch must not reach the Gate at once, whatever Gate the
		// adopter supplies (PromptGate already serializes itself; this
		// covers a raw GateFunc too).
		cctx = tools.WithPrompt(cctx, func(req tools.PermissionRequest) tools.PermissionResponse {
			l.gateMu.Lock()
			defer l.gateMu.Unlock()
			return l.Gate.Ask(ctx, req)
		})
	} else {
		// No Gate: deny an Ask outright, as headless ("no prompt is
		// configured"). Without this, the registry falls back to its own
		// promptFn (an adopter's TUI modal, wired at SetPromptFunc call
		// sites), which would silently defeat "no Gate means headless deny".
		cctx = tools.WithoutPrompt(cctx)
	}
	name := t.Name()
	return abandonAfter(cctx, watchdog, func() (tools.ToolResult, error) {
		// No progress channel: an abandoned call may outlive the executor,
		// which closes its channel when this function returns.
		return l.Tools.ExecuteWithProgress(cctx, name, input, nil)
	})
}

// abandonAfter returns when ctx ends, or after d (d<=0: never), even if fn
// ignores its context (the v1.10 codegraph spin, task 349f1f14). The
// goroutine is abandoned, not killed; the buffered channel lets it finish
// its send and exit.
func abandonAfter(ctx context.Context, d time.Duration, fn func() (tools.ToolResult, error)) (tools.ToolResult, error) {
	type outcome struct {
		res tools.ToolResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := fn()
		done <- outcome{res, err}
	}()
	var expired <-chan time.Time
	if d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		expired = timer.C
	}
	select {
	case <-ctx.Done():
		return tools.ToolResult{}, fmt.Errorf("tool execution exceeded timeout: %w", ctx.Err())
	case <-expired:
		return tools.ToolResult{}, fmt.Errorf("tool execution exceeded timeout: %w", context.DeadlineExceeded)
	case o := <-done:
		return o.res, o.err
	}
}

// errorEnvelope is the JSON error the model receives for a failed call. It
// matches the agent's historical format.
func errorEnvelope(tool, msg string) string {
	b, _ := json.Marshal(map[string]any{"error": true, "tool": tool, "message": msg})
	return string(b)
}

func formatResult(name string, r tools.ExecutorResult) (string, bool) {
	if r.Result.Error || r.Err != nil {
		msg := r.Result.Content
		if msg == "" && r.Err != nil {
			msg = r.Err.Error()
		}
		return errorEnvelope(name, msg), true
	}
	return r.Result.Content, false
}

func argsOrEmpty(args string) string {
	if strings.TrimSpace(args) == "" {
		return "{}"
	}
	return args
}

func parseInput(args string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(argsOrEmpty(args)), &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// safeName turns a model-supplied ID into a file name that cannot leave its
// directory.
func safeName(id string, fallback string) string {
	name := unsafeName.ReplaceAllString(id, "_")
	if strings.Trim(name, "_") == "" {
		return fallback
	}
	return name
}

func (l *Loop) spill(content, id string, idx int, lim Limits) string {
	if lim.SpillBytes <= 0 || len(content) <= lim.SpillBytes {
		return content
	}
	l.spillSeq++
	name := fmt.Sprintf("%s-%d", safeName(id, "call-"+strconv.Itoa(idx)), l.spillSeq)
	capped, _, err := ctxmgr.CapToolResult(content, lim.SpillBytes, l.sessionID(), name, l.SpillDir)
	if err != nil {
		l.emit(Event{Kind: EventNotice, Text: "could not spill a large tool result: " + err.Error()})
		return content
	}
	return capped
}

func (l *Loop) sessionID() string {
	if l.SessionID != "" {
		return safeName(l.SessionID, "loop")
	}
	return "loop-" + strconv.Itoa(os.Getpid())
}

// toolMessage pairs a result with its call. Text-format calls ("text-tc-N")
// have no tool_call entry to pair with, so they come back as a labelled user
// message, as the agent always did.
func toolMessage(c ToolCall, content string) Message {
	if strings.HasPrefix(c.ID, "text-tc-") {
		return Message{Role: "user", Content: fmt.Sprintf("[Tool Result: %s]\n%s", c.Name, content), Timestamp: time.Now()}
	}
	return Message{Role: "tool", ToolCallID: c.ID, Name: c.Name, Content: content, Timestamp: time.Now()}
}
