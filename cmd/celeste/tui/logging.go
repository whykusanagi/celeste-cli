// Package tui provides the Bubble Tea-based terminal UI for Celeste CLI.
// This file contains logging functionality for debugging skill calls.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var (
	logFile     *os.File
	logEnabled  = true
	logFilePath string
)

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

	logDir := filepath.Join(homeDir, ".celeste", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}

	// Create log file with timestamp (celeste_YYYY-MM-DD.log)
	logFilePath = filepath.Join(logDir, fmt.Sprintf("celeste_%s.log", time.Now().Format("2006-01-02")))
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	logFile = f

	LogInfo("=== Session started ===")
	return nil
}

// CloseLogging closes the log file.
func CloseLogging() {
	if logFile != nil {
		LogInfo("=== Session ended ===")
		logFile.Close()
	}
}

// LogInfo logs an informational message.
func LogInfo(msg string) {
	if logFile == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(logFile, "[%s] INFO: %s\n", timestamp, msg)
}

// LogSkillCall logs when a skill/function is called by the LLM.
func LogSkillCall(name string, args map[string]any) {
	if logFile == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(logFile, "[%s] SKILL_CALL: %s\n", timestamp, name)
	fmt.Fprintf(logFile, "  Arguments: %v\n", args)
}

// LogSkillResult logs the result of a skill execution.
func LogSkillResult(name string, result string, err error) {
	if logFile == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if err != nil {
		fmt.Fprintf(logFile, "[%s] SKILL_ERROR: %s - %v\n", timestamp, name, err)
	} else {
		// Truncate result for log
		resultTrunc := result
		if len(resultTrunc) > 200 {
			resultTrunc = resultTrunc[:200] + "..."
		}
		fmt.Fprintf(logFile, "[%s] SKILL_RESULT: %s\n", timestamp, name)
		fmt.Fprintf(logFile, "  Result: %s\n", resultTrunc)
	}
}

// LogLLMRequest logs an LLM request.
func LogLLMRequest(messageCount int, toolCount int) {
	if logFile == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(logFile, "[%s] LLM_REQUEST: %d messages, %d tools available\n", timestamp, messageCount, toolCount)
}

// LogLLMResponse logs an LLM response.
func LogLLMResponse(contentLen int, hasToolCalls bool) {
	if logFile == nil {
		return
	}
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	if hasToolCalls {
		fmt.Fprintf(logFile, "[%s] LLM_RESPONSE: %d chars, HAS TOOL CALLS\n", timestamp, contentLen)
	} else {
		fmt.Fprintf(logFile, "[%s] LLM_RESPONSE: %d chars, no tool calls\n", timestamp, contentLen)
	}
}

// GetLogPath returns the current log file path.
func GetLogPath() string {
	return logFilePath
}
