// cmd/celeste/tools/mcp/stdio.go
package mcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// StdioTransport communicates with an MCP server via a child process's
// stdin and stdout. Each JSON-RPC message is a single line of JSON.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
	closed bool
}

// NewStdioTransport spawns a child process and connects to its stdin/stdout.
// command is the executable to run (e.g., "npx", "python3").
// args are the command-line arguments.
// env is an optional map of environment variables (supports ${VAR} expansion).
func NewStdioTransport(command string, args []string, env map[string]string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)

	// Build environment: inherit current env + add custom vars
	if len(env) > 0 {
		expanded := expandEnvVars(env)
		cmdEnv := os.Environ()
		for k, v := range expanded {
			cmdEnv = append(cmdEnv, k+"="+v)
		}
		cmd.Env = cmdEnv
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}

	// Discard stderr to avoid blocking
	cmd.Stderr = io.Discard

	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start process %q: %w", command, err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
	}, nil
}

// Send sends a JSON-RPC request as a single JSON line to the child process stdin.
func (t *StdioTransport) Send(req *Request) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("transport is closed")
	}

	data, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	data = append(data, '\n')
	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("write to stdin: %w", err)
	}
	return nil
}

// SendNotification sends a JSON-RPC notification as a single JSON line.
func (t *StdioTransport) SendNotification(notif *Notification) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return fmt.Errorf("transport is closed")
	}

	data, err := json.Marshal(notif)
	if err != nil {
		return fmt.Errorf("marshal notification: %w", err)
	}

	data = append(data, '\n')
	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("write notification to stdin: %w", err)
	}
	return nil
}

// Receive reads the next JSON line from stdout and parses it as a Response.
func (t *StdioTransport) Receive() (*Response, error) {
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("read from stdout: %w", err)
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w (raw: %s)", err, string(line))
	}
	return &resp, nil
}

// Close shuts down the child process and closes pipes.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.closed {
		return nil
	}
	t.closed = true

	t.stdin.Close()
	// Wait for process to exit (ignore error -- process may have already exited)
	_ = t.cmd.Wait()
	return nil
}

// expandEnvVars expands ${VAR} references in environment variable values
// using the current process's environment.
func expandEnvVars(env map[string]string) map[string]string {
	result := make(map[string]string, len(env))
	for k, v := range env {
		result[k] = os.Expand(v, func(key string) string {
			return os.Getenv(key)
		})
	}
	return result
}

// isEnvVarRef checks if a string contains ${...} patterns.
func isEnvVarRef(s string) bool {
	return strings.Contains(s, "${")
}
