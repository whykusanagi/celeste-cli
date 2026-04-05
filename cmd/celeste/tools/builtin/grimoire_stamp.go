package builtin

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// stampGrimoireMetadata reads a .grimoire file, updates or inserts the metadata
// comment block (<!-- ... -->) with current timestamp, git hash, branch,
// commit count, and code graph stats. Called automatically by write_file and
// patch_file when the target is a .grimoire file.
func stampGrimoireMetadata(grimoirePath string) {
	data, err := os.ReadFile(grimoirePath)
	if err != nil {
		return
	}

	content := string(data)
	dir := filepath.Dir(grimoirePath)

	// Build metadata block
	meta := buildGrimoireMeta(dir)

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

	_ = os.WriteFile(grimoirePath, []byte(content), 0644)
}

func buildGrimoireMeta(dir string) string {
	var sb strings.Builder
	sb.WriteString("<!--\n")
	sb.WriteString(fmt.Sprintf("last_updated: %s\n", time.Now().Format("2006-01-02 15:04:05")))

	if hash := stampGitCmd(dir, "rev-parse", "--short", "HEAD"); hash != "" {
		sb.WriteString(fmt.Sprintf("git_hash: %s\n", hash))
	}
	if branch := stampGitCmd(dir, "rev-parse", "--abbrev-ref", "HEAD"); branch != "" {
		sb.WriteString(fmt.Sprintf("git_branch: %s\n", branch))
	}
	if count := stampGitCmd(dir, "rev-list", "--count", "HEAD"); count != "" {
		sb.WriteString(fmt.Sprintf("git_commit_count: %s\n", count))
	}

	// Include code graph index info if available
	if indexInfo := getIndexInfo(dir); indexInfo != "" {
		sb.WriteString(fmt.Sprintf("index: %s\n", indexInfo))
	}

	sb.WriteString("-->\n")
	return sb.String()
}

func stampGitCmd(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// getIndexInfo returns code graph database info for the given project directory.
func getIndexInfo(projectDir string) string {
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		return ""
	}

	// Compute project hash (mirrors codegraph.DefaultIndexPath)
	hash := sha256.Sum256([]byte(projectDir))
	hexHash := hex.EncodeToString(hash[:8])
	dbPath := filepath.Join(homeDir, ".celeste", "projects", hexHash, "codegraph.db")

	info, err := os.Stat(dbPath)
	if err != nil {
		return ""
	}

	modTime := info.ModTime().Format("2006-01-02 15:04")
	sizeMB := float64(info.Size()) / (1024 * 1024)
	return fmt.Sprintf("indexed %s (%.1fMB)", modTime, sizeMB)
}
