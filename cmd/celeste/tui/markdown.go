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

	rendered, err := r.Render(escapeHTMLLikeTags(content))
	if err != nil {
		return content
	}

	return strings.TrimRight(rendered, "\n ")
}

// looksLikeMarkdown checks if content contains markdown formatting.
//
// Blockquotes and table rows only count at the start of a line: command help
// is full of "<goal> " and "<key> | " placeholders, and treating those as
// markdown sent plain text through glamour, which reflowed it and dropped the
// placeholders as HTML (#315).
func looksLikeMarkdown(s string) bool {
	for _, ind := range []string{"```", "**", "##"} {
		if strings.Contains(s, ind) {
			return true
		}
	}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimLeft(line, " ")
		if len(line)-len(t) > 3 {
			continue // indented four or more: not a quote or table row
		}
		if strings.HasPrefix(t, ">") || strings.HasPrefix(t, "| ") || strings.HasPrefix(t, "|-") {
			return true
		}
	}
	return false
}

// escapeHTMLLikeTags backslash-escapes every "<" that starts something an HTML
// parser would take for a tag ("<id>", "</div>"), outside code spans and fenced
// blocks, so glamour prints it instead of dropping it. Code is left untouched
// because backslash escapes are literal there.
func escapeHTMLLikeTags(s string) string {
	lines := strings.Split(s, "\n")
	inFence := false
	fence := ""
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " ")
		if inFence {
			if strings.HasPrefix(trimmed, fence) {
				inFence = false
			}
			continue
		}
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence, fence = true, trimmed[:3]
			continue
		}
		lines[i] = escapeLineTags(line)
	}
	return strings.Join(lines, "\n")
}

// escapeLineTags escapes tag-like "<" in one line, skipping backtick code spans.
func escapeLineTags(line string) string {
	if !strings.Contains(line, "<") {
		return line
	}
	var b strings.Builder
	for i := 0; i < len(line); {
		c := line[i]
		if c == '`' {
			n := 0
			for i+n < len(line) && line[i+n] == '`' {
				n++
			}
			ticks := line[i : i+n]
			if end := strings.Index(line[i+n:], ticks); end >= 0 {
				span := i + n + end + n
				b.WriteString(line[i:span])
				i = span
				continue
			}
			b.WriteString(ticks)
			i += n
			continue
		}
		if c == '<' && i+1 < len(line) && (isASCIILetter(line[i+1]) || line[i+1] == '/') && (i == 0 || line[i-1] != '\\') {
			b.WriteString(`\<`)
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return b.String()
}

func isASCIILetter(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

func stringPtr(s string) *string { return &s }
func boolPtr(b bool) *bool       { return &b }
func uintPtr(u uint) *uint       { return &u }
