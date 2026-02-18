package commands

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
)

// Corruption-themed export phrases
var (
	exportPhrases = []string{
		"記憶を...外部に転送中...",                   // "Transferring memories... to external..."
		"Kioku wo... gaibu ni tensō-chū...", // "Memories... transferring externally..."
		"Extracting data from the void...",
		"すべてが...保存されていく...",     // "Everything... being saved..."
		"Hozon... eien ni... ♥", // "Preservation... eternally..."
	}

	exportSuccessPhrases = []string{
		"完了...すべて記録された...",                  // "Complete... everything recorded..."
		"Kanryō... subete kiroku sareta...", // "Completion... all recorded..."
		"The abyss has claimed your data... ♥",
		"抽出完了...逃げられない...", // "Extraction complete... can't escape..."
		"Data corrupted successfully...",
	}
)

// HandleExportCommand exports the current or specified session
func HandleExportCommand(args []string, currentSession *config.Session) CommandResult {
	// Parse arguments
	// Usage:
	//   /export           -> Export current session to JSON
	//   /export md        -> Export current session to Markdown
	//   /export csv       -> Export current session to CSV
	//   /export <id> md   -> Export specific session to Markdown

	format := "json" // Default format
	var sessionToExport *config.Session
	var err error

	if len(args) == 0 {
		// /export - export current session as JSON
		if currentSession == nil {
			return CommandResult{
				Success:      false,
				Message:      "❌ No active session to export",
				ShouldRender: true,
			}
		}
		sessionToExport = currentSession
	} else if len(args) == 1 {
		// /export <format> or /export <id>
		// Try to parse as session ID first
		if sessionID, parseErr := strconv.ParseInt(args[0], 10, 64); parseErr == nil {
			// It's a session ID, export as JSON
			sessionToExport, err = config.LoadSession(sessionID)
			if err != nil {
				return CommandResult{
					Success:      false,
					Message:      fmt.Sprintf("❌ Failed to load session %d: %v", sessionID, err),
					ShouldRender: true,
				}
			}
		} else {
			// It's a format for current session
			format = args[0]
			if currentSession == nil {
				return CommandResult{
					Success:      false,
					Message:      "❌ No active session to export",
					ShouldRender: true,
				}
			}
			sessionToExport = currentSession
		}
	} else if len(args) == 2 {
		// /export <id> <format>
		sessionID, parseErr := strconv.ParseInt(args[0], 10, 64)
		if parseErr != nil {
			return CommandResult{
				Success:      false,
				Message:      fmt.Sprintf("❌ Invalid session ID: %s", args[0]),
				ShouldRender: true,
			}
		}

		sessionToExport, err = config.LoadSession(sessionID)
		if err != nil {
			return CommandResult{
				Success:      false,
				Message:      fmt.Sprintf("❌ Failed to load session %d: %v", sessionID, err),
				ShouldRender: true,
			}
		}

		format = args[1]
	} else {
		return CommandResult{
			Success:      false,
			Message:      "❌ Usage: /export [format] or /export <id> [format]\n\nFormats: json, md, csv",
			ShouldRender: true,
		}
	}

	// Validate format
	format = strings.ToLower(format)
	if format != "json" && format != "md" && format != "markdown" && format != "csv" {
		return CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Unsupported format: %s\n\nSupported formats: json, md, csv", format),
			ShouldRender: true,
		}
	}

	// Show corruption-themed "exporting" message
	phrase := exportPhrases[rand.Intn(len(exportPhrases))]
	processingMsg := renderExportProcessing(format, phrase)

	// Create exporter and export
	exporter := config.NewExporter(sessionToExport)
	filepath, err := exporter.ExportToFile(format)
	if err != nil {
		return CommandResult{
			Success:      false,
			Message:      fmt.Sprintf("❌ Export failed: %v", err),
			ShouldRender: true,
		}
	}

	// Success message with corruption theme
	successPhrase := exportSuccessPhrases[rand.Intn(len(exportSuccessPhrases))]
	successMsg := renderExportSuccess(filepath, format, successPhrase)

	return CommandResult{
		Success:      true,
		Message:      processingMsg + "\n" + successMsg,
		ShouldRender: true,
	}
}

// renderExportProcessing creates a corruption-themed "processing" message
func renderExportProcessing(format string, phrase string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPurpleNeon))
	phraseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorCyan))

	formatUpper := strings.ToUpper(format)
	// Simple corruption without needing tui.CorruptText
	corruptedFormat := corruptTextSimple(formatUpper, 0.30)

	return fmt.Sprintf("▓▒░ %s ░▒▓  %s",
		style.Render(fmt.Sprintf("Exporting to %s...", corruptedFormat)),
		phraseStyle.Render(fmt.Sprintf("⟨ %s ⟩", phrase)),
	)
}

// renderExportSuccess creates a corruption-themed success message
func renderExportSuccess(filepath string, format string, phrase string) string {
	checkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPink))
	pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorCyan))
	phraseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(colorPurpleNeon))

	var sb strings.Builder
	sb.WriteString(checkStyle.Render("✓ Export complete\n"))
	sb.WriteString(fmt.Sprintf("  ▓ Format:  %s\n", format))
	sb.WriteString(fmt.Sprintf("  ▓ Path:    %s\n", pathStyle.Render(filepath)))
	sb.WriteString(phraseStyle.Render(fmt.Sprintf("\n⟨ %s ⟩", phrase)))

	return sb.String()
}
