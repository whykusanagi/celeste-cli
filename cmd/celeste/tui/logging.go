// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains logging functionality for debugging skill calls.
package tui

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/privfs"
)

// logMu guards logFile and logFilePath: runs and /agent and /orch
// goroutines log while the chat starts and stops logging.
var (
	logMu       sync.Mutex
	logFile     *os.File
	logEnabled  = true
	logFilePath string
)

// logf writes one formatted entry, or nothing when logging is off.
func logf(format string, args ...any) {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile == nil {
		return
	}
	fmt.Fprintf(logFile, format, args...)
}

// InitLogging initializes the skill call log file.
func InitLogging() error {
	if !logEnabled {
		return nil
	}

	// Create log directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}

	// Owner-only: the log holds tool arguments and results. An existing
	// directory or file an older version left open is tightened.
	logDir := filepath.Join(homeDir, ".celeste", "logs")
	if err := privfs.MkdirAll(logDir); err != nil {
		return err
	}

	// Create log file with timestamp (celeste_YYYY-MM-DD.log)
	path := filepath.Join(logDir, fmt.Sprintf("celeste_%s.log", time.Now().Format("2006-01-02")))
	logMu.Lock()
	logFilePath = path
	logMu.Unlock()
	f, err := privfs.OpenAppend(path)
	if err != nil {
		return err
	}
	logMu.Lock()
	if logFile != nil {
		// A second init must not leak the first handle: Windows cannot
		// remove a file that is still open.
		logFile.Close()
	}
	logFile = f
	logMu.Unlock()

	LogInfo("=== Session started ===")
	return nil
}

// CloseLogging closes the log file.
func CloseLogging() {
	LogInfo("=== Session ended ===")
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		logFile.Close()
		logFile = nil
	}
}

// LogInfo logs an informational message.
func LogInfo(msg string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logf("[%s] INFO: %s\n", timestamp, msg)
}

// LogSkillCall logs when a skill/function is called by the LLM.
func LogSkillCall(name string, args map[string]any) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logf("[%s] SKILL_CALL: %s\n", timestamp, name)
	logf("  Arguments: %v\n", args)
}

// LogSkillResult logs the result of a skill execution.
func LogSkillResult(name string, result string, err error) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if err != nil {
		logf("[%s] SKILL_ERROR: %s - %v\n", timestamp, name, err)
	} else {
		// Truncate result for log
		resultTrunc := result
		if len(resultTrunc) > 200 {
			resultTrunc = resultTrunc[:200] + "..."
		}
		logf("[%s] SKILL_RESULT: %s\n", timestamp, name)
		logf("  Result: %s\n", resultTrunc)
	}
}

// LogLLMRequest logs an LLM request.
func LogLLMRequest(messageCount int, toolCount int) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logf("[%s] LLM_REQUEST: %d messages, %d tools available\n", timestamp, messageCount, toolCount)
}

// LogLLMResponse logs an LLM response.
func LogLLMResponse(contentLen int, hasToolCalls bool) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if hasToolCalls {
		logf("[%s] LLM_RESPONSE: %d chars, HAS TOOL CALLS\n", timestamp, contentLen)
	} else {
		logf("[%s] LLM_RESPONSE: %d chars, no tool calls\n", timestamp, contentLen)
	}
}

// RedirectStdLog points the standard library's log package at the TUI log
// file (dropping the output when none is open) and returns a func that puts
// the previous writer back. While Bubble Tea owns the terminal, a plain
// log.Printf (tools/mcp logs every connect) would draw over the frame.
func RedirectStdLog() (restore func()) {
	prev := log.Writer()
	log.SetOutput(stdLogWriter{})
	return func() { log.SetOutput(prev) }
}

// stdLogWriter appends the log package's output to the TUI log file.
type stdLogWriter struct{}

func (stdLogWriter) Write(p []byte) (int, error) {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		_, _ = logFile.Write(p)
	}
	return len(p), nil
}

// GetLogPath returns the current log file path.
func GetLogPath() string {
	logMu.Lock()
	defer logMu.Unlock()
	return logFilePath
}
