package commands

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// R2: the export summary's rows start at the same indent; the styled title's
// newline used to be padded into the next row.
func TestExportSuccessRowsAligned(t *testing.T) {
	out := ansi.Strip(renderExportSuccess("/tmp/x.md", "markdown", "done"))
	for _, row := range strings.Split(out, "\n") {
		if strings.Contains(row, "▓ Format:") || strings.Contains(row, "▓ Path:") {
			if !strings.HasPrefix(row, "  ▓") {
				t.Errorf("row not at the 2-space indent: %q", row)
			}
		}
	}
}
