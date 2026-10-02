package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/internal/proctree"
)

const (
	maxStdout       = 1 << 20   // hook stdout kept; more is a failure
	maxStderr       = 64 << 10  // hook stderr kept for messages
	maxContext      = 8 << 10   // additionalContext per event, all hooks together
	maxReason       = 2 << 10   // reason / failure text
	maxToolResponse = 256 << 10 // tool_response.content sent on stdin
	waitDelay       = time.Second
)

// hookResult is what one hook process decided.
type hookResult struct {
	decision Decision
	reason   string
	context  string
	updated  map[string]any
	failed   string // why the hook failed; empty on success
}

// runHook runs one hook in dir with payload as JSON on stdin and interprets
// its output by protocol. Stdout is untrusted input: capped, and for v2 it
// must be a single JSON object, or the hook failed.
func runHook(ctx context.Context, def Definition, dir string, payload map[string]any) hookResult {
	stdin, err := json.Marshal(payload)
	if err != nil {
		return hookResult{failed: "encode payload: " + err.Error()}
	}
	env, omitted := hookEnv(payload, dir)
	if def.Protocol == ProtocolV1 && omitted {
		// v1 guards read CELESTE_TOOL_* (or {{command}}); they can't judge an
		// input that didn't fit, so the hook fails (and PreToolUse blocks).
		return hookResult{failed: "the tool input is too large (or contains NUL) for protocol v1 environment variables; move this hook to hooks.json protocol v2, which reads the full input on stdin"}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(def.Timeout)*time.Second)
	defer cancel()

	var proc *exec.Cmd
	if def.Protocol == ProtocolV1 {
		toolName, _ := payload["tool_name"].(string)
		input, _ := payload["tool_input"].(map[string]any)
		workspace, _ := payload["workspace"].(string)
		proc = v1Command(ctx, expandTemplateVars(def.Command, workspace, toolName, input))
	} else {
		proc = shellCommand(ctx, def.Command)
	}
	proc.Dir = dir
	proc.Env = append(filterInheritedHookEnv(os.Environ()), env...)
	proc.Stdin = bytes.NewReader(stdin)
	stdout := &capBuffer{max: maxStdout, onOverflow: cancel}
	stderr := &capBuffer{max: maxStderr}
	proc.Stdout, proc.Stderr = stdout, stderr
	proc.WaitDelay = waitDelay // a child holding stdout can't hang us

	runErr := proctree.Start(proc)
	if runErr == nil {
		runErr = proc.Wait()
	}
	// Unconditional, regardless of whether ctx ever fired: a hook's shell can
	// exit on its own while a background child still holds the group open
	// (surfacing below as exec.ErrWaitDelay). On unix this signals -pgid after
	// the group leader has already been reaped; the kernel can't recycle a
	// pgid while any process in that group still exists, so the only residual
	// risk is a brand-new, unrelated group leader that later reused this exact
	// pid after the original group fully emptied — accepted. On Windows it
	// terminates the hook's Job Object, which holds only the hook's tree.
	_ = proctree.Kill(proc)
	if stdout.over {
		// Applies to both protocols, including v1: 1.x had no stdout cap, so a
		// v1 guard that floods stdout now fails closed even at exit 0 instead
		// of being allowed through on a silently truncated read — an
		// intentional behavior change from 1.x, tracked for MIGRATING-2.0.md.
		return hookResult{failed: fmt.Sprintf("stdout exceeded %d bytes", maxStdout)}
	}
	if ctx.Err() != nil {
		return hookResult{failed: "did not finish: " + ctx.Err().Error()}
	}
	exitCode := 0
	if runErr != nil {
		if errors.Is(runErr, exec.ErrWaitDelay) {
			return hookResult{failed: "a background process kept the hook's output open"}
		}
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) {
			return hookResult{failed: truncate(runErr.Error(), maxReason)}
		}
		exitCode = exitErr.ExitCode()
	}
	if def.Protocol == ProtocolV1 {
		return v1Result(def.Event, exitCode, stdout, stderr)
	}
	return v2Result(exitCode, stdout, stderr)
}

// v1Result keeps 1.x semantics: exit 0 allows; anything else blocks a
// PreToolUse with the output as the reason, and is a logged failure for
// PostToolUse.
func v1Result(ev Event, exitCode int, stdout, stderr *capBuffer) hookResult {
	if exitCode == 0 {
		return hookResult{decision: Allow}
	}
	msg := strings.TrimSpace(stdout.String())
	if msg == "" {
		msg = strings.TrimSpace(stderr.String())
	}
	if ev != EventPreToolUse {
		return hookResult{failed: truncate(fmt.Sprintf("exit status %d: %s", exitCode, msg), maxReason)}
	}
	return hookResult{decision: Deny, reason: truncate(msg, maxReason)}
}

// v2Result reads {decision, reason, additionalContext, updatedInput} from
// stdout. Empty stdout allows. A non-zero exit, oversized or malformed
// output, or a decision other than allow|deny|ask is a failure.
func v2Result(exitCode int, stdout, stderr *capBuffer) hookResult {
	if exitCode != 0 {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return hookResult{failed: truncate(fmt.Sprintf("exit status %d: %s", exitCode, msg), maxReason)}
	}
	if stdout.over {
		return hookResult{failed: fmt.Sprintf("stdout exceeded %d bytes", maxStdout)}
	}
	out := bytes.TrimSpace(stdout.Bytes())
	if len(out) == 0 {
		return hookResult{decision: Allow}
	}
	fields, err := decodeV2OutputObject(out)
	if err != nil {
		return hookResult{failed: truncate("stdout is not a JSON object: "+err.Error(), maxReason)}
	}
	res := hookResult{decision: Allow}
	if raw, ok := fields["reason"]; ok {
		var reason string
		if err := json.Unmarshal(raw, &reason); err != nil {
			return hookResult{failed: "reason is not a string"}
		}
		res.reason = truncate(reason, maxReason)
	}
	if raw, ok := fields["additionalContext"]; ok {
		if err := json.Unmarshal(raw, &res.context); err != nil {
			return hookResult{failed: "additionalContext is not a string"}
		}
	}
	if raw, ok := fields["decision"]; ok {
		var decision string
		if err := json.Unmarshal(raw, &decision); err != nil {
			return hookResult{failed: "decision is not a string"}
		}
		switch Decision(decision) {
		case Allow, Deny, Ask:
			res.decision = Decision(decision)
		default:
			return hookResult{failed: truncate(fmt.Sprintf("decision must be allow, deny or ask, not %q", decision), maxReason)}
		}
	}
	if raw, ok := fields["updatedInput"]; ok && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil || m == nil {
			return hookResult{failed: "updatedInput is not a JSON object"}
		}
		res.updated = m
	}
	return res
}

func decodeV2OutputObject(out []byte) (map[string]json.RawMessage, error) {
	if len(out) == 0 || out[0] != '{' {
		return nil, errors.New("stdout is not a JSON object")
	}
	allowed := map[string]bool{
		"decision":          true,
		"reason":            true,
		"additionalContext": true,
		"updatedInput":      true,
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("stdout is not a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("object key is not a string")
		}
		if !allowed[key] {
			return nil, fmt.Errorf("unknown key %q in hook output", key)
		}
		if _, ok := fields[key]; ok {
			return nil, fmt.Errorf("duplicate key %q in hook output", key)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		fields[key] = raw
	}
	tok, err = dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '}' {
		return nil, errors.New("stdout is not a JSON object")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return nil, errors.New("stdout contains more than one JSON value")
	}
	return fields, nil
}

// hookEnv is the CELESTE_* environment. A value over envValueCap or
// containing NUL is omitted (empty), never cut to a prefix, and
// CELESTE_TOOL_INPUT_TRUNCATED=1 says so; omitted reports whether that
// happened.
func hookEnv(payload map[string]any, dir string) (env []string, omitted bool) {
	put := func(name, value string) {
		if len(value) > envValueCap || strings.ContainsRune(value, 0) {
			value, omitted = "", true
		}
		env = append(env, name+"="+value)
	}
	str := func(k string) string { s, _ := payload[k].(string); return s }
	put("CELESTE_HOOK_EVENT", str("event"))
	put("CELESTE_WORKSPACE", str("workspace"))
	put("CELESTE_PROJECT_DIR", dir)
	put("CELESTE_SESSION_ID", str("session_id"))
	if name := str("tool_name"); name != "" {
		input, _ := payload["tool_input"].(map[string]any)
		inputJSON, _ := json.Marshal(input)
		put("CELESTE_TOOL_NAME", name)
		put("CELESTE_TOOL_INPUT", string(inputJSON))
		put("CELESTE_TOOL_PATH", stringArg(input, "path"))
		put("CELESTE_TOOL_COMMAND", stringArg(input, "command"))
	}
	if omitted {
		env = append(env, "CELESTE_TOOL_INPUT_TRUNCATED=1")
	}
	return env, omitted
}

func filterInheritedHookEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "CELESTE_") {
			continue
		}
		filtered = append(filtered, kv)
	}
	return filtered
}

// truncate cuts s to at most n bytes without leaving a broken UTF-8 rune.
// It is used only for text shown to people or the model, never for values
// a hook decides on.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.ToValidUTF8(s[:n], "")
}

// capBuffer keeps the first max bytes and discards the rest. It never
// errors, so a flooding hook isn't killed by a broken pipe mid-write.
type capBuffer struct {
	buf        bytes.Buffer
	max        int
	over       bool
	onOverflow func()
}

func (c *capBuffer) Write(p []byte) (int, error) {
	overflow := false
	room := c.max - c.buf.Len()
	switch {
	case room <= 0:
		if len(p) > 0 {
			overflow = true
		}
	case len(p) > room:
		c.buf.Write(p[:room])
		overflow = true
	default:
		c.buf.Write(p)
	}
	if overflow && !c.over {
		c.over = true
		if c.onOverflow != nil {
			c.onOverflow()
		}
	}
	return len(p), nil
}

func (c *capBuffer) Bytes() []byte  { return c.buf.Bytes() }
func (c *capBuffer) String() string { return c.buf.String() }
