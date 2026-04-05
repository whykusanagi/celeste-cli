package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/ansi"
	"github.com/charmbracelet/glamour/styles"
)

// corruptedStyleConfig returns a glamour style config matching the corrupted theme.
func corruptedStyleConfig() ansi.StyleConfig {
	s := styles.DarkStyleConfig

	purple := "#8b5cf6"
	pink := "#d94f90"
	cyan := "#00d4ff"
	muted := "#7a7085"
	text := "#f5f1f8"

	// Headings
	s.H1.Color = stringPtr(pink)
	s.H1.Bold = boolPtr(true)
	s.H2.Color = stringPtr(purple)
	s.H2.Bold = boolPtr(true)
	s.H3.Color = stringPtr(purple)
	s.H3.Bold = boolPtr(true)

	// Inline code
	s.Code.Color = stringPtr(cyan)

	// Code blocks — use dracula theme for syntax highlighting
	s.CodeBlock.Theme = "dracula"
	s.CodeBlock.Margin = uintPtr(1)

	// Bold/italic
	s.Emph.Color = stringPtr(purple)
	s.Emph.Italic = boolPtr(true)
	s.Strong.Color = stringPtr(pink)
	s.Strong.Bold = boolPtr(true)

	// Links
	s.Link.Color = stringPtr(cyan)
	s.LinkText.Color = stringPtr(purple)

	// Lists
	s.List.LevelIndent = 2
	s.Item.Color = stringPtr(text)

	// Tables
	s.Table.CenterSeparator = stringPtr("┼")
	s.Table.ColumnSeparator = stringPtr("│")
	s.Table.RowSeparator = stringPtr("─")

	// Horizontal rule
	s.HorizontalRule.Color = stringPtr(muted)

	// Block quotes
	s.BlockQuote.Color = stringPtr(muted)
	s.BlockQuote.Indent = uintPtr(2)
	s.BlockQuote.IndentToken = stringPtr("│ ")

	return s
}

// cachedRenderer is a package-level renderer to avoid re-creating per message.
var cachedRenderer *glamour.TermRenderer

func getRenderer(width int) *glamour.TermRenderer {
	if cachedRenderer != nil {
		return cachedRenderer
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(corruptedStyleConfig()),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	cachedRenderer = r
	return r
}

// renderMarkdown renders markdown content with the corrupted theme.
// Falls back to plain text if rendering fails.
func renderMarkdown(content string, width int) string {
	if width < 20 {
		width = 80
	}

	// Don't render very short or non-markdown content
	if len(content) < 10 || !looksLikeMarkdown(content) {
		return content
	}

	r := getRenderer(width - 4)
	if r == nil {
		return content
	}

	rendered, err := r.Render(content)
	if err != nil {
		return content
	}

	return strings.TrimRight(rendered, "\n ")
}

// looksLikeMarkdown checks if content contains markdown formatting.
func looksLikeMarkdown(s string) bool {
	indicators := []string{
		"```", "**", "##", "| ", "> ",
	}
	for _, ind := range indicators {
		if strings.Contains(s, ind) {
			return true
		}
	}
	return false
}

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }
func uintPtr(u uint) *uint       { return &u }
