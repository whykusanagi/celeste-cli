package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/codegraph"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/grimoire"
)

// stampGrimoireMetadata updates or inserts the metadata comment block
// (<!-- ... -->) of the .grimoire file the call just wrote: grimoire.GrimoireMeta
// (timestamp, git hash, branch, commit count) plus the code graph index line.
// Called by write_file and patch_file when the target is a .grimoire file.
// targetPath is the path as the workspace names it (the project the index
// belongs to); realPath is the checked path the edit wrote, and the stamp
// reads and writes that one through writeFileFunc, as the edit did.
func stampGrimoireMetadata(workspace, targetPath, realPath string) {
	data, err := readFileNoFollow(realPath, maxEditBytes)
	if err != nil {
		return
	}

	content := string(data)
	dir := filepath.Dir(targetPath)

	meta := grimoire.GrimoireMeta(dir)
	if indexInfo := getIndexInfo(dir); indexInfo != "" {
		meta = strings.TrimSuffix(meta, "-->\n") + fmt.Sprintf("index: %s\n", indexInfo) + "-->\n"
	}

	// Replace existing metadata block or prepend
	if startIdx := strings.Index(content, "<!--"); startIdx >= 0 {
		if endIdx := strings.Index(content, "-->"); endIdx > startIdx {
			// Replace existing block, preserve content after -->
			after := content[endIdx+3:]
			// Trim leading newline after -->
			after = strings.TrimPrefix(after, "\n")
			content = meta + after
		}
	} else {
		// Prepend metadata
		content = meta + "\n" + content
	}

	_ = writeFileFunc(workspace, realPath, []byte(content), 0644)
}

// getIndexInfo returns code graph database info for the given project directory.
func getIndexInfo(projectDir string) string {
	if home, _ := os.UserHomeDir(); home == "" {
		return ""
	}
	info, err := os.Stat(codegraph.IndexPath(projectDir))
	if err != nil {
		return ""
	}

	modTime := info.ModTime().Format("2006-01-02 15:04")
	sizeMB := float64(info.Size()) / (1024 * 1024)
	return fmt.Sprintf("indexed %s (%.1fMB)", modTime, sizeMB)
}
