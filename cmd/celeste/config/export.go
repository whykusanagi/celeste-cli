package config

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Exporter handles session export to various formats
type Exporter struct {
	session *Session
}

// NewExporter creates a new exporter for a session
func NewExporter(session *Session) *Exporter {
	return &Exporter{session: session}
}

// ToMarkdown exports the session as Markdown with frontmatter
func (e *Exporter) ToMarkdown() (string, error) {
	if e.session == nil {
		return "", fmt.Errorf("session is nil")
	}

	var sb strings.Builder

	// Frontmatter
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("session_id: %s\n", e.session.ID))
	sb.WriteString(fmt.Sprintf("created: %s\n", e.session.CreatedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("updated: %s\n", e.session.UpdatedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("model: %s\n", e.session.Model))

	if e.session.Provider != "" {
		sb.WriteString(fmt.Sprintf("provider: %s\n", e.session.Provider))
	}

	sb.WriteString(fmt.Sprintf("messages: %d\n", len(e.session.Messages)))
	sb.WriteString(fmt.Sprintf("tokens: %d\n", e.session.TokenCount))

	if e.session.UsageMetrics != nil {
		sb.WriteString(fmt.Sprintf("cost: $%.4f\n", e.session.UsageMetrics.EstimatedCost))
		if e.session.UsageMetrics.CompactionCount > 0 {
			sb.WriteString(fmt.Sprintf("compactions: %d\n", e.session.UsageMetrics.CompactionCount))
		}
	}

	sb.WriteString("---\n\n")

	// Title
	title := e.session.Metadata["title"]
	if title == "" {
		title = "Conversation Session"
	}
	sb.WriteString(fmt.Sprintf("# %s\n\n", title))

	// Session info
	sb.WriteString(fmt.Sprintf("**Session ID:** %s  \n", e.session.ID))
	sb.WriteString(fmt.Sprintf("**Created:** %s  \n", e.session.CreatedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString(fmt.Sprintf("**Model:** %s  \n", e.session.Model))
	if e.session.UsageMetrics != nil {
		sb.WriteString(fmt.Sprintf("**Tokens:** %s  \n", FormatTokenCount(e.session.UsageMetrics.TotalTokens)))
		sb.WriteString(fmt.Sprintf("**Cost:** %s  \n", FormatCost(e.session.UsageMetrics.EstimatedCost)))
	}
	sb.WriteString("\n---\n\n")

	// Messages
	for _, msg := range e.session.Messages {
		// Format role (capitalize first letter)
		role := msg.Role
		if len(role) > 0 {
			role = strings.ToUpper(role[:1]) + role[1:]
		}

		// Timestamp
		timestamp := msg.Timestamp.Format("2006-01-02 15:04:05")

		// Write message header
		sb.WriteString(fmt.Sprintf("## %s (%s)\n\n", role, timestamp))

		// Write content
		// If content is very long, truncate for readability in markdown
		content := msg.Content
		if len(content) > 10000 {
			content = content[:10000] + "\n\n...[truncated]..."
		}

		sb.WriteString(content)
		sb.WriteString("\n\n---\n\n")
	}

	return sb.String(), nil
}

// ToJSON exports the session as pretty-printed JSON
func (e *Exporter) ToJSON() (string, error) {
	if e.session == nil {
		return "", fmt.Errorf("session is nil")
	}

	data, err := json.MarshalIndent(e.session, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal session: %w", err)
	}

	return string(data), nil
}

// ToCSV exports the session as CSV (one row per message)
// Format: timestamp,role,content,tokens,model,cost
func (e *Exporter) ToCSV() (string, error) {
	if e.session == nil {
		return "", fmt.Errorf("session is nil")
	}

	var sb strings.Builder
	writer := csv.NewWriter(&sb)

	// Write header
	headers := []string{"timestamp", "role", "content", "tokens", "model", "cost"}
	if err := writer.Write(headers); err != nil {
		return "", fmt.Errorf("failed to write CSV header: %w", err)
	}

	// Get model pricing for cost per message
	costPerToken := 0.0
	if e.session.UsageMetrics != nil && e.session.UsageMetrics.TotalTokens > 0 {
		costPerToken = e.session.UsageMetrics.EstimatedCost / float64(e.session.UsageMetrics.TotalTokens)
	}

	// Write messages
	for _, msg := range e.session.Messages {
		// Clean content for CSV (remove newlines, escape quotes)
		content := strings.ReplaceAll(msg.Content, "\n", " ")
		content = strings.ReplaceAll(content, "\r", "")

		// Truncate very long content
		if len(content) > 1000 {
			content = content[:1000] + "..."
		}

		// Estimate tokens for this message
		msgTokens := EstimateTokens(msg.Content)

		// Estimate cost for this message
		msgCost := float64(msgTokens) * costPerToken

		row := []string{
			msg.Timestamp.Format(time.RFC3339),
			msg.Role,
			content,
			fmt.Sprintf("%d", msgTokens),
			e.session.Model,
			fmt.Sprintf("%.4f", msgCost),
		}

		if err := writer.Write(row); err != nil {
			return "", fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", fmt.Errorf("CSV writer error: %w", err)
	}

	return sb.String(), nil
}

// SaveToFile saves the exported content to a file
func (e *Exporter) SaveToFile(content string, format string) (string, error) {
	if e.session == nil {
		return "", fmt.Errorf("session is nil")
	}

	// Get export directory
	exportDir := GetExportDir()

	// Ensure directory exists
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create export directory: %w", err)
	}

	// Generate filename
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("session_%s_%s.%s", e.session.ID, timestamp, format)
	filepath := filepath.Join(exportDir, filename)

	// Write file
	if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write export file: %w", err)
	}

	return filepath, nil
}

// ExportToFile is a convenience method that exports and saves in one call
func (e *Exporter) ExportToFile(format string) (string, error) {
	var content string
	var err error

	switch format {
	case "md", "markdown":
		content, err = e.ToMarkdown()
	case "json":
		content, err = e.ToJSON()
	case "csv":
		content, err = e.ToCSV()
	default:
		return "", fmt.Errorf("unsupported export format: %s", format)
	}

	if err != nil {
		return "", err
	}

	// Normalize format for file extension
	if format == "markdown" {
		format = "md"
	}

	return e.SaveToFile(content, format)
}

// GetExportDir returns the path to the exports directory
func GetExportDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".celeste", "exports")
	}
	return filepath.Join(homeDir, ".celeste", "exports")
}

// ExportSession is a helper function to export a session by ID
func ExportSession(sessionID int64, format string) (string, error) {
	// Load session
	session, err := LoadSession(sessionID)
	if err != nil {
		return "", fmt.Errorf("failed to load session: %w", err)
	}

	// Create exporter and export
	exporter := NewExporter(session)
	return exporter.ExportToFile(format)
}
