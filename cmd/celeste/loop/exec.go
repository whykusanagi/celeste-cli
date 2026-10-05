package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	ctxmgr "github.com/whykusanagi/celeste-cli/v2/cmd/celeste/context"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/llm"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/tools"
)

// pending is one tool call on its way to a result message.
type pending struct {
	call    ToolCall
	content string
	isError bool
	meta    map[string]any // the tool's ToolResult.Metadata
	settled bool           // has its result without running (bad args, unknown tool)
	ran     bool           // went through the registry
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
			} else if l.Refuse != nil {
				if why := l.Refuse(c.Name); why != "" {
					p.settle(errorEnvelope(c.Name, why))
				}
			}
		}
		l.emit(Event{Kind: EventToolStart, Call: p.call, At: time.Now()})
	}

	l.execute(ctx, ps, lim)

	sigs := make([]string, 0, len(ps))
	for i, p := range ps {
		sigs = append(sigs, p.call.Name+"|"+p.content)
		content := l.spill(p.content, p.call.ID, i, lim)
		out.messages = append(out.messages, toolMessage(p.call, content, p.meta, lim.KeepToolMetadata))
		l.emit(Event{Kind: EventToolResult, Call: p.call, Text: content, IsError: p.isError, Metadata: p.meta})
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
	ex.SetExecFunc(func(ectx context.Context, id string, t tools.Tool, input map[string]any) (tools.ToolResult, error) {
		// The tool sees the model's call ID (2.0 F4: its checkpoint's
		// message_id), not the executor's group index.
		i, _ := strconv.Atoi(id)
		return l.invoke(tools.WithCallID(ectx, group[i].call.ID), t, input, lim)
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
		p.meta = r.Result.Metadata
	}
}

// invoke runs one call through the registry, so tool hooks (F0), the
// permission checker and its deny rules always apply, exactly once.
//
// The tool's timeout always goes through tools.WithExecTimeout: the registry
// applies it to the tool alone, after the hooks and the permission prompt,
// so a slow PreToolUse hook never eats the tool's time. A tool that ignores
// its context is abandoned by a watchdog at tool timeout + HookBudget. With
// a Gate the watchdog is paused while the Gate is asked and restarts the
// full budget after the answer (F2d: before, gated runs had no watchdog).
// Rarely a call is abandoned just as approval starts: the timer fires while
// pause runs, so the Gate is asked for a call already given up on.
// The Gate is asked with the call's own context, which ends when invoke
// returns, so for a call abandoned before it reached the Gate, PromptGate
// won't open the modal afterwards (a raw GateFunc may ignore ctx).
func (l *Loop) invoke(ctx context.Context, t tools.Tool, input map[string]any, lim Limits) (tools.ToolResult, error) {
	timeout := tools.TimeoutFor(t, lim.ToolTimeout)
	cctx, cancel := context.WithCancel(tools.WithExecTimeout(ctx, timeout))
	defer cancel()
	if l.Advisor != nil {
		cctx = tools.WithAskAdvisor(cctx, l.Advisor)
	}
	askCtx := cctx // ends with this call, abandoned or not
	wd := newWatchdog(timeout + lim.HookBudget)
	defer wd.stop()
	if l.Gate != nil {
		// This run's Gate answers the registry's Ask, including one forced
		// by a PreToolUse hook's "ask". Serialized: parallel-safe calls in
		// one batch must not reach the Gate at once, whatever Gate the
		// adopter supplies (PromptGate already serializes itself; this
		// covers a raw GateFunc too).
		cctx = tools.WithPrompt(cctx, func(req tools.PermissionRequest) tools.PermissionResponse {
			wd.pause()
			defer wd.resume()
			l.gateMu.Lock()
			defer l.gateMu.Unlock()
			return l.Gate.Ask(askCtx, req)
		})
	} else {
		// No Gate: deny an Ask outright, as headless ("no prompt is
		// configured"). Without this, the registry falls back to its own
		// promptFn (an adopter's TUI modal, wired at SetPromptFunc call
		// sites), which would silently defeat "no Gate means headless deny".
		cctx = tools.WithoutPrompt(cctx)
	}
	name := t.Name()
	return abandonAfter(cctx, wd.expired, func() (tools.ToolResult, error) {
		// No progress channel: an abandoned call may outlive the executor,
		// which closes its channel when this function returns.
		return l.Tools.ExecuteWithProgress(cctx, name, input, nil)
	})
}

// watchdog closes expired once its budget has run while not paused. pause
// stops the clock; resume restarts the full budget.
type watchdog struct {
	mu      sync.Mutex
	d       time.Duration
	t       *time.Timer
	expired chan struct{}
	once    sync.Once
	stopped bool
}

func newWatchdog(d time.Duration) *watchdog {
	w := &watchdog{d: d, expired: make(chan struct{})}
	if d > 0 {
		w.t = time.AfterFunc(d, w.fire)
	}
	return w
}

func (w *watchdog) fire() { w.once.Do(func() { close(w.expired) }) }

func (w *watchdog) pause() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.t != nil {
		w.t.Stop()
	}
}

// resume is a no-op after stop: an abandoned call's prompt may return
// after invoke did, and must not re-arm the timer.
func (w *watchdog) resume() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.t != nil && !w.stopped {
		w.t.Reset(w.d)
	}
}

func (w *watchdog) stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.stopped = true
	if w.t != nil {
		w.t.Stop()
	}
}

// abandonAfter returns when ctx ends or expired closes, even if fn ignores
// its context (the v1.10 codegraph spin, task 349f1f14). The goroutine is
// abandoned, not killed; the buffered channel lets it finish its send and
// exit.
func abandonAfter(ctx context.Context, expired <-chan struct{}, fn func() (tools.ToolResult, error)) (tools.ToolResult, error) {
	type outcome struct {
		res tools.ToolResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, err := fn()
		done <- outcome{res, err}
	}()
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

// spillFailedNote goes in the cut marker when the spill file could not be
// written: the middle of the output is gone, so recall cannot bring it back.
const spillFailedNote = "The full output could not be saved, so it cannot be recalled. " +
	"If you need the missing part, re-run with narrower output (a line range, grep, head or tail)."

func (l *Loop) spill(content, id string, idx int, lim Limits) string {
	if lim.SpillBytes <= 0 || len(content) <= lim.SpillBytes {
		return content
	}
	name := fmt.Sprintf("%s-%d", safeName(id, "call-"+strconv.Itoa(idx)), l.nextSpill())
	capped, _, err := ctxmgr.CapToolResult(content, lim.SpillBytes, l.sessionID(), name, l.SpillDir)
	if err != nil {
		// The cap still applies (2.0 F3: nothing trims a result after it
		// is recorded). Without the spill file there is no recall path, so
		// the model gets the head and tail with a cut marker.
		l.emit(Event{Kind: EventNotice, Text: "could not spill a large tool result, so only its start and end were kept: " + err.Error()})
		return ctxmgr.SnipToolResult(content, lim.SpillBytes, spillFailedNote)
	}
	return capped
}

func (l *Loop) sessionID() string {
	if l.SessionID != "" {
		return safeName(l.SessionID, "loop")
	}
	return "loop-" + strconv.Itoa(os.Getpid())
}

// nextSpill numbers the next spill file: from SpillCounter when the adopter
// shares one across runs, else per Loop.
func (l *Loop) nextSpill() int64 {
	if l.SpillCounter != nil {
		return l.SpillCounter.Add(1)
	}
	l.spillSeq++
	return int64(l.spillSeq)
}

// toolMessage pairs a result with its call. Text-format calls ("text-tc-N")
// have no tool_call entry to pair with, so they come back as a labelled user
// message, as the agent always did. With keep, an image result's metadata
// rides on the message with a marker the model can read (as the chat did
// before the loop); other metadata stays off the provider's messages.
func toolMessage(c ToolCall, content string, meta map[string]any, keep bool) Message {
	if strings.HasPrefix(c.ID, "text-tc-") {
		return Message{Role: "user", Content: fmt.Sprintf("[Tool Result: %s]\n%s", c.Name, content), Timestamp: time.Now()}
	}
	msg := Message{Role: "tool", ToolCallID: c.ID, Name: c.Name, Content: content, Timestamp: time.Now()}
	if kind, _ := meta["type"].(string); keep && kind == "image" {
		// A copy: EventToolResult carries meta, and a consumer changing
		// one must not change the other.
		msg.Metadata = maps.Clone(meta)
		if format, _ := meta["format"].(string); format != "" {
			msg.Content += fmt.Sprintf("\n\n[Image data available: format=%s. The image content has been captured and will be provided to vision-capable models.]", format)
		}
	}
	return msg
}
