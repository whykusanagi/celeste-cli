package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// Reminder represents a reminder entry.
type Reminder struct {
	ID      string    `json:"id"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
	Created time.Time `json:"created"`
}

// getRemindersPath returns the path to reminders.json.
func getRemindersPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "reminders.json")
}

// ReminderSetTool sets a reminder.
type ReminderSetTool struct {
	BaseTool
}

// NewReminderSetTool creates a ReminderSetTool.
func NewReminderSetTool() *ReminderSetTool {
	return &ReminderSetTool{
		BaseTool: BaseTool{
			ToolName:        "set_reminder",
			ToolDescription: "Set a reminder with a specific time and message",
			ToolParameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"message": map[string]interface{}{
						"type":        "string",
						"description": "Reminder message",
					},
					"time": map[string]interface{}{
						"type":        "string",
						"description": "Time for reminder (format: 'YYYY-MM-DD HH:MM' or 'HH:MM' for today, or relative like 'in 1 hour', 'tomorrow at 3pm')",
					},
				},
				"required": []string{"message", "time"},
			}),
			ReadOnly:        false,
			ConcurrencySafe: false,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"message", "time"},
		},
	}
}

func (t *ReminderSetTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	message, ok := input["message"].(string)
	if !ok || message == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'message' parameter is required",
			"Please provide a reminder message.",
			map[string]interface{}{
				"skill": "set_reminder",
				"field": "message",
			},
		))
	}

	timeStr, ok := input["time"].(string)
	if !ok || timeStr == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'time' parameter is required",
			"Please provide a time for the reminder (format: 'YYYY-MM-DD HH:MM' or 'HH:MM' for today).",
			map[string]interface{}{
				"skill": "set_reminder",
				"field": "time",
			},
		))
	}

	// Parse time string
	var reminderTime time.Time
	var err error

	now := time.Now()
	if strings.HasPrefix(timeStr, "in ") {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"Relative time parsing not yet implemented",
			"Please use format 'YYYY-MM-DD HH:MM' or 'HH:MM' for today.",
			map[string]interface{}{
				"skill":    "set_reminder",
				"field":    "time",
				"provided": timeStr,
			},
		))
	}

	if len(timeStr) > 10 {
		reminderTime, err = time.Parse("2006-01-02 15:04", timeStr)
		if err != nil {
			reminderTime, err = time.Parse("2006-01-02 15:04:05", timeStr)
		}
	} else {
		timeLayout := "15:04"
		if len(strings.Split(timeStr, ":")) == 3 {
			timeLayout = "15:04:05"
		}
		parsedTime, parseErr := time.Parse(timeLayout, timeStr)
		if parseErr != nil {
			return tools.ToolResult{}, fmt.Errorf("invalid time format: %w", parseErr)
		}
		reminderTime = time.Date(now.Year(), now.Month(), now.Day(), parsedTime.Hour(), parsedTime.Minute(), parsedTime.Second(), 0, now.Location())
		if reminderTime.Before(now) {
			reminderTime = reminderTime.Add(24 * time.Hour)
		}
		err = nil
	}

	if err != nil {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"Failed to parse time",
			"Please use format 'YYYY-MM-DD HH:MM' or 'HH:MM' for today.",
			map[string]interface{}{
				"skill":    "set_reminder",
				"field":    "time",
				"provided": timeStr,
				"error":    err.Error(),
			},
		))
	}

	// Load existing reminders
	remindersPath := getRemindersPath()
	var reminders []Reminder
	if data, readErr := os.ReadFile(remindersPath); readErr == nil {
		_ = json.Unmarshal(data, &reminders)
	}

	// Add new reminder
	reminder := Reminder{
		ID:      uuid.New().String(),
		Message: message,
		Time:    reminderTime,
		Created: now,
	}
	reminders = append(reminders, reminder)

	// Save reminders
	os.MkdirAll(filepath.Dir(remindersPath), 0755)
	data, err := json.MarshalIndent(reminders, "", "  ")
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to save reminder",
			"An internal error occurred while saving the reminder. Please try again.",
			map[string]interface{}{
				"skill": "set_reminder",
				"error": err.Error(),
			},
		))
	}
	if err := os.WriteFile(remindersPath, data, 0644); err != nil {
		return resultFromMap(formatErrorResponse(
			"internal_error",
			"Failed to save reminder file",
			"An internal error occurred while saving the reminder. Please try again.",
			map[string]interface{}{
				"skill": "set_reminder",
				"error": err.Error(),
			},
		))
	}

	return resultFromMap(map[string]interface{}{
		"id":      reminder.ID,
		"message": message,
		"time":    reminderTime.Format(time.RFC3339),
		"success": true,
	})
}

// ReminderListTool lists all active reminders.
type ReminderListTool struct {
	BaseTool
}

// NewReminderListTool creates a ReminderListTool.
func NewReminderListTool() *ReminderListTool {
	return &ReminderListTool{
		BaseTool: BaseTool{
			ToolName:        "list_reminders",
			ToolDescription: "List all active reminders",
			ToolParameters: mustJSON(map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
				"required":   []string{},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
		},
	}
}

func (t *ReminderListTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	remindersPath := getRemindersPath()
	var reminders []Reminder
	if data, err := os.ReadFile(remindersPath); err == nil {
		_ = json.Unmarshal(data, &reminders)
	}

	now := time.Now()
	activeReminders := make([]map[string]interface{}, 0)
	for _, r := range reminders {
		if r.Time.After(now) {
			activeReminders = append(activeReminders, map[string]interface{}{
				"id":      r.ID,
				"message": r.Message,
				"time":    r.Time.Format(time.RFC3339),
				"created": r.Created.Format(time.RFC3339),
			})
		}
	}

	return resultFromMap(map[string]interface{}{
		"count":     len(activeReminders),
		"reminders": activeReminders,
	})
}
