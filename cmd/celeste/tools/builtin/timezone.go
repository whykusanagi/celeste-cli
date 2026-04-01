package builtin

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// TimezoneConverterTool converts time between timezones.
type TimezoneConverterTool struct {
	BaseTool
}

// NewTimezoneConverterTool creates a TimezoneConverterTool.
func NewTimezoneConverterTool() *TimezoneConverterTool {
	return &TimezoneConverterTool{
		BaseTool: BaseTool{
			ToolName:        "convert_timezone",
			ToolDescription: "Convert time between different timezones",
			ToolParameters: mustJSON(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"time": map[string]any{
						"type":        "string",
						"description": "Time to convert (format: 'HH:MM' or 'HH:MM:SS', defaults to current time if not provided)",
					},
					"from_timezone": map[string]any{
						"type":        "string",
						"description": "Source timezone (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo')",
					},
					"to_timezone": map[string]any{
						"type":        "string",
						"description": "Target timezone (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo')",
					},
					"date": map[string]any{
						"type":        "string",
						"description": "Optional date (format: 'YYYY-MM-DD', defaults to today if not provided)",
					},
				},
				"required": []string{"from_timezone", "to_timezone"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"from_timezone", "to_timezone"},
		},
	}
}

func (tool *TimezoneConverterTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	fromTZ, ok := input["from_timezone"].(string)
	if !ok || fromTZ == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'from_timezone' parameter is required",
			"Please specify the source timezone (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo').",
			map[string]any{
				"skill": "convert_timezone",
				"field": "from_timezone",
			},
		))
	}

	toTZ, ok := input["to_timezone"].(string)
	if !ok || toTZ == "" {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			"The 'to_timezone' parameter is required",
			"Please specify the target timezone (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo').",
			map[string]any{
				"skill": "convert_timezone",
				"field": "to_timezone",
			},
		))
	}

	fromLoc, err := time.LoadLocation(fromTZ)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Invalid timezone '%s'", fromTZ),
			"Please use a valid IANA timezone identifier (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo').",
			map[string]any{
				"skill": "convert_timezone", "field": "from_timezone", "provided": fromTZ, "error": err.Error(),
			},
		))
	}

	toLoc, err := time.LoadLocation(toTZ)
	if err != nil {
		return resultFromMap(formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Invalid timezone '%s'", toTZ),
			"Please use a valid IANA timezone identifier (e.g., 'America/New_York', 'UTC', 'Asia/Tokyo').",
			map[string]any{
				"skill": "convert_timezone", "field": "to_timezone", "provided": toTZ, "error": err.Error(),
			},
		))
	}

	var t time.Time
	if timeStr, ok := input["time"].(string); ok && timeStr != "" {
		var dateStr string
		if date, ok := input["date"].(string); ok && date != "" {
			dateStr = date
		} else {
			dateStr = time.Now().In(fromLoc).Format("2006-01-02")
		}

		timeLayout := "2006-01-02 15:04:05"
		if !strings.Contains(timeStr, ":") {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				"Invalid time format",
				"Please use format 'HH:MM' or 'HH:MM:SS' (e.g., '14:30' or '14:30:00').",
				map[string]any{
					"skill": "convert_timezone", "field": "time", "provided": timeStr,
				},
			))
		}
		if len(strings.Split(timeStr, ":")[0]) == 1 {
			timeStr = "0" + timeStr
		}
		if len(strings.Split(timeStr, ":")) == 2 {
			timeStr = timeStr + ":00"
		}

		fullTimeStr := dateStr + " " + timeStr
		t, err = time.ParseInLocation(timeLayout, fullTimeStr, fromLoc)
		if err != nil {
			return resultFromMap(formatErrorResponse(
				"validation_error",
				"Invalid time format",
				"Please use format 'YYYY-MM-DD HH:MM' or 'HH:MM' for today.",
				map[string]any{
					"skill": "convert_timezone", "field": "time", "provided": timeStr, "error": err.Error(),
				},
			))
		}
	} else {
		if date, ok := input["date"].(string); ok && date != "" {
			dateStr := date + " 00:00:00"
			timeLayout := "2006-01-02 15:04:05"
			t, err = time.ParseInLocation(timeLayout, dateStr, fromLoc)
			if err != nil {
				return resultFromMap(formatErrorResponse(
					"validation_error",
					"Invalid date format",
					"Please use format 'YYYY-MM-DD' (e.g., '2024-12-03').",
					map[string]any{
						"skill": "convert_timezone", "field": "date", "provided": date, "error": err.Error(),
					},
				))
			}
		} else {
			t = time.Now().In(fromLoc)
		}
	}

	converted := t.In(toLoc)

	return resultFromMap(map[string]any{
		"original_time":   t.Format("2006-01-02 15:04:05 MST"),
		"converted_time":  converted.Format("2006-01-02 15:04:05 MST"),
		"from_timezone":   fromTZ,
		"to_timezone":     toTZ,
		"original_utc":    t.UTC().Format("2006-01-02 15:04:05 UTC"),
		"converted_utc":   converted.UTC().Format("2006-01-02 15:04:05 UTC"),
		"timezone_offset": converted.Format("-07:00"),
	})
}
