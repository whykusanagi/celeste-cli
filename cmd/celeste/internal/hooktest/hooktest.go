// Package hooktest runs the test binary itself as a hook command, so hook
// tests behave the same under sh on Unix and cmd.exe on Windows (2.0 F0).
package hooktest

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

const envVar = "HOOKTEST_HELPER"

// RunIfHelper must be the first call in TestMain. When Command started this
// process, it acts as the hook and exits.
func RunIfHelper() {
	if os.Getenv(envVar) != "1" {
		return
	}
	os.Exit(helper(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// Command returns a hook command line that runs this test binary as the
// helper with args. Args must avoid ", $, % and backquotes: the line is
// parsed by sh on Unix and cmd.exe on Windows.
func Command(t testing.TB, args ...string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(envVar, "1") // inherited by the hook through os.Environ
	parts := []string{`"` + exe + `"`}
	for _, a := range args {
		if strings.ContainsAny(a, "\"$%`") {
			t.Fatalf("hooktest: unsafe argument %q", a)
		}
		parts = append(parts, `"`+a+`"`)
	}
	return strings.Join(parts, " ")
}

var canned = map[string]string{
	"garbage":      "not json",
	"array":        "[1,2]",
	"null":         "null",
	"bad-key":      `{"decison":"deny"}`,
	"wrong-case":   `{"Decision":"deny"}`,
	"bad-decision": `{"decision":"maybe"}`,
	"alias":        `{"decision":"approve"}`,
	"bad-update":   `{"updatedInput":"rm -rf /"}`,
	"flood":        strings.Repeat("x", 2<<20),
}

var envKeys = []string{
	"CELESTE_HOOK_EVENT", "CELESTE_TOOL_NAME", "CELESTE_TOOL_INPUT", "CELESTE_TOOL_PATH",
	"CELESTE_TOOL_COMMAND", "CELESTE_TOOL_INPUT_TRUNCATED", "CELESTE_WORKSPACE",
	"CELESTE_PROJECT_DIR", "CELESTE_SESSION_ID",
}

func helper(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	arg := func(i int) string {
		if i < len(args) {
			return args[i]
		}
		return ""
	}
	emit := func(v any) int {
		_ = json.NewEncoder(stdout).Encode(v)
		return 0
	}
	writeTo := func(path string, data []byte) int {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	}
	switch arg(0) {
	case "allow":
		return emit(map[string]any{"decision": "allow"})
	case "deny", "ask":
		return emit(map[string]any{"decision": arg(0), "reason": arg(1)})
	case "context":
		return emit(map[string]any{"additionalContext": arg(1)})
	case "rewrite":
		var p struct {
			ToolInput map[string]any `json:"tool_input"`
		}
		_ = json.NewDecoder(stdin).Decode(&p)
		if p.ToolInput == nil {
			p.ToolInput = map[string]any{}
		}
		p.ToolInput[arg(1)] = arg(2)
		return emit(map[string]any{"updatedInput": p.ToolInput})
	case "record":
		data, _ := io.ReadAll(stdin)
		return writeTo(arg(1), data)
	case "env":
		env := map[string]string{}
		for _, k := range envKeys {
			env[k] = os.Getenv(k)
		}
		data, _ := json.Marshal(env)
		return writeTo(arg(1), data)
	case "canned":
		out, ok := canned[arg(1)]
		if !ok {
			fmt.Fprintf(stderr, "hooktest: unknown canned output %q\n", arg(1))
			return 2
		}
		fmt.Fprint(stdout, out)
		return 0
	case "floodforever":
		for {
			fmt.Fprint(stdout, strings.Repeat("x", 64<<10))
		}
	case "exit":
		code, _ := strconv.Atoi(arg(1))
		fmt.Fprint(stderr, arg(2))
		return code
	case "sleep":
		// Bounded: a helper that escaped a kill must not hold a TempDir
		// open past the test's cleanup retries.
		time.Sleep(10 * time.Second)
		return 0
	}
	fmt.Fprintf(stderr, "hooktest: unknown mode %q\n", arg(0))
	return 2
}
