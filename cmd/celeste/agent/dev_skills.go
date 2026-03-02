package agent

import (
	"bufio"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
)

const (
	maxReadBytes     = 200_000
	maxCommandOutput = 12_000
)

func RegisterDevSkills(registry *skills.Registry, workspace string) error {
	workspace, err := normalizeWorkspace(workspace)
	if err != nil {
		return err
	}

	definitions := []skills.Skill{
		devListFilesSkill(),
		devReadFileSkill(),
		devWriteFileSkill(),
		devSearchFilesSkill(),
		devRunCommandSkill(),
	}
	for _, skillDef := range definitions {
		registry.RegisterSkill(skillDef)
	}

	registry.RegisterHandler("dev_list_files", func(args map[string]interface{}) (interface{}, error) {
		return devListFilesHandler(workspace, args)
	})
	registry.RegisterHandler("dev_read_file", func(args map[string]interface{}) (interface{}, error) {
		return devReadFileHandler(workspace, args)
	})
	registry.RegisterHandler("dev_write_file", func(args map[string]interface{}) (interface{}, error) {
		return devWriteFileHandler(workspace, args)
	})
	registry.RegisterHandler("dev_search_files", func(args map[string]interface{}) (interface{}, error) {
		return devSearchFilesHandler(workspace, args)
	})
	registry.RegisterHandler("dev_run_command", func(args map[string]interface{}) (interface{}, error) {
		return devRunCommandHandler(workspace, args)
	})

	return nil
}

func devListFilesSkill() skills.Skill {
	return skills.Skill{
		Name:        "dev_list_files",
		Description: "List files/directories inside the configured workspace. Use this before reading or editing files.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Relative directory path inside workspace. Defaults to '.'",
				},
				"recursive": map[string]interface{}{
					"type":        "boolean",
					"description": "Recursively walk subdirectories when true.",
				},
				"max_entries": map[string]interface{}{
					"type":        "number",
					"description": "Maximum entries to return. Default 200.",
				},
			},
		},
	}
}

func devReadFileSkill() skills.Skill {
	return skills.Skill{
		Name:        "dev_read_file",
		Description: "Read a text file from workspace. Supports optional line ranges.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Relative file path inside workspace.",
				},
				"start_line": map[string]interface{}{
					"type":        "number",
					"description": "1-based inclusive start line. Defaults to 1.",
				},
				"end_line": map[string]interface{}{
					"type":        "number",
					"description": "1-based inclusive end line. Defaults to end-of-file.",
				},
			},
			"required": []string{"path"},
		},
	}
}

func devWriteFileSkill() skills.Skill {
	return skills.Skill{
		Name:        "dev_write_file",
		Description: "Write text to a workspace file. Creates parent directories automatically.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Relative file path inside workspace.",
				},
				"content": map[string]interface{}{
					"type":        "string",
					"description": "Content to write.",
				},
				"append": map[string]interface{}{
					"type":        "boolean",
					"description": "Append instead of overwrite when true.",
				},
			},
			"required": []string{"path", "content"},
		},
	}
}

func devSearchFilesSkill() skills.Skill {
	return skills.Skill{
		Name:        "dev_search_files",
		Description: "Search for text in workspace files and return matching lines.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]interface{}{
					"type":        "string",
					"description": "Text pattern to search for.",
				},
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Relative directory path to search. Defaults to '.'",
				},
				"max_results": map[string]interface{}{
					"type":        "number",
					"description": "Maximum matches to return. Defaults to 100.",
				},
				"case_sensitive": map[string]interface{}{
					"type":        "boolean",
					"description": "Use case-sensitive matching when true.",
				},
			},
			"required": []string{"pattern"},
		},
	}
}

func devRunCommandSkill() skills.Skill {
	return skills.Skill{
		Name:        "dev_run_command",
		Description: "Execute a shell command from workspace root and return combined output.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]interface{}{
					"type":        "string",
					"description": "Shell command to execute.",
				},
				"timeout_seconds": map[string]interface{}{
					"type":        "number",
					"description": "Execution timeout in seconds. Defaults to 20.",
				},
			},
			"required": []string{"command"},
		},
	}
}

func devListFilesHandler(workspace string, args map[string]interface{}) (interface{}, error) {
	path := getStringArg(args, "path", ".")
	recursive := getBoolArg(args, "recursive", false)
	maxEntries := getIntArg(args, "max_entries", 200)
	if maxEntries <= 0 {
		maxEntries = 200
	}
	if maxEntries > 1000 {
		maxEntries = 1000
	}

	targetPath, err := resolveWorkspacePath(workspace, path)
	if err != nil {
		return nil, err
	}

	entries := make([]map[string]interface{}, 0, maxEntries)
	truncated := false

	if !recursive {
		dirs, err := os.ReadDir(targetPath)
		if err != nil {
			return nil, err
		}
		for _, entry := range dirs {
			if len(entries) >= maxEntries {
				truncated = true
				break
			}
			info, _ := entry.Info()
			rel, _ := filepath.Rel(workspace, filepath.Join(targetPath, entry.Name()))
			entries = append(entries, map[string]interface{}{
				"path":   rel,
				"name":   entry.Name(),
				"is_dir": entry.IsDir(),
				"size":   fileSize(info),
			})
		}
	} else {
		err = filepath.WalkDir(targetPath, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if path == targetPath {
				return nil
			}
			if len(entries) >= maxEntries {
				truncated = true
				return fs.SkipAll
			}
			info, _ := d.Info()
			rel, _ := filepath.Rel(workspace, path)
			entries = append(entries, map[string]interface{}{
				"path":   rel,
				"name":   d.Name(),
				"is_dir": d.IsDir(),
				"size":   fileSize(info),
			})
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	return map[string]interface{}{
		"workspace": workspace,
		"path":      path,
		"entries":   entries,
		"count":     len(entries),
		"truncated": truncated,
	}, nil
}

func devReadFileHandler(workspace string, args map[string]interface{}) (interface{}, error) {
	path := getStringArg(args, "path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	startLine := getIntArg(args, "start_line", 1)
	if startLine < 1 {
		startLine = 1
	}
	endLine := getIntArg(args, "end_line", 0)

	targetPath, err := resolveWorkspacePath(workspace, path)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, err
	}
	truncated := false
	if len(data) > maxReadBytes {
		data = data[:maxReadBytes]
		truncated = true
	}

	text := string(data)
	lines := strings.Split(text, "\n")
	totalLines := len(lines)

	if endLine <= 0 || endLine > totalLines {
		endLine = totalLines
	}
	if startLine > endLine {
		startLine = endLine
	}

	selected := ""
	if totalLines > 0 {
		selected = strings.Join(lines[startLine-1:endLine], "\n")
	}

	return map[string]interface{}{
		"path":        path,
		"workspace":   workspace,
		"start_line":  startLine,
		"end_line":    endLine,
		"total_lines": totalLines,
		"truncated":   truncated,
		"content":     selected,
	}, nil
}

func devWriteFileHandler(workspace string, args map[string]interface{}) (interface{}, error) {
	path := getStringArg(args, "path", "")
	if path == "" {
		return nil, fmt.Errorf("path is required")
	}
	content := getStringArg(args, "content", "")
	appendMode := getBoolArg(args, "append", false)

	targetPath, err := resolveWorkspacePath(workspace, path)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
		return nil, err
	}

	var bytesWritten int
	if appendMode {
		f, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		n, err := f.WriteString(content)
		if err != nil {
			return nil, err
		}
		bytesWritten = n
	} else {
		if err := os.WriteFile(targetPath, []byte(content), 0644); err != nil {
			return nil, err
		}
		bytesWritten = len(content)
	}

	return map[string]interface{}{
		"path":          path,
		"workspace":     workspace,
		"bytes_written": bytesWritten,
		"append":        appendMode,
	}, nil
}

func devSearchFilesHandler(workspace string, args map[string]interface{}) (interface{}, error) {
	pattern := getStringArg(args, "pattern", "")
	if pattern == "" {
		return nil, fmt.Errorf("pattern is required")
	}
	path := getStringArg(args, "path", ".")
	maxResults := getIntArg(args, "max_results", 100)
	if maxResults <= 0 {
		maxResults = 100
	}
	if maxResults > 1000 {
		maxResults = 1000
	}
	caseSensitive := getBoolArg(args, "case_sensitive", false)

	targetPath, err := resolveWorkspacePath(workspace, path)
	if err != nil {
		return nil, err
	}

	needle := pattern
	if !caseSensitive {
		needle = strings.ToLower(pattern)
	}

	matches := make([]map[string]interface{}, 0, maxResults)
	truncated := false

	err = filepath.WalkDir(targetPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNumber := 0
		for scanner.Scan() {
			lineNumber++
			line := scanner.Text()
			haystack := line
			if !caseSensitive {
				haystack = strings.ToLower(line)
			}
			if strings.Contains(haystack, needle) {
				rel, _ := filepath.Rel(workspace, path)
				matches = append(matches, map[string]interface{}{
					"path":        rel,
					"line_number": lineNumber,
					"line":        line,
				})
				if len(matches) >= maxResults {
					truncated = true
					return fs.SkipAll
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"pattern":        pattern,
		"case_sensitive": caseSensitive,
		"matches":        matches,
		"count":          len(matches),
		"truncated":      truncated,
	}, nil
}

func devRunCommandHandler(workspace string, args map[string]interface{}) (interface{}, error) {
	command := getStringArg(args, "command", "")
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("command is required")
	}
	timeoutSeconds := getIntArg(args, "timeout_seconds", 20)
	if timeoutSeconds <= 0 {
		timeoutSeconds = 20
	}
	if timeoutSeconds > 300 {
		timeoutSeconds = 300
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-lc", command)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()

	outputStr := string(output)
	truncated := false
	if len(outputStr) > maxCommandOutput {
		outputStr = outputStr[:maxCommandOutput]
		truncated = true
	}

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	result := map[string]interface{}{
		"command":   command,
		"workspace": workspace,
		"exit_code": exitCode,
		"output":    outputStr,
		"truncated": truncated,
		"timed_out": ctx.Err() == context.DeadlineExceeded,
	}

	if err != nil {
		result["error"] = err.Error()
	}
	return result, nil
}

func normalizeWorkspace(workspace string) (string, error) {
	if workspace == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve current directory: %w", err)
		}
		workspace = cwd
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("resolve workspace path: %w", err)
	}
	clean := filepath.Clean(abs)
	info, err := os.Stat(clean)
	if err != nil {
		return "", fmt.Errorf("stat workspace: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace is not a directory: %s", clean)
	}
	return clean, nil
}

func resolveWorkspacePath(workspace, input string) (string, error) {
	workspace = filepath.Clean(workspace)
	if input == "" {
		input = "."
	}

	var candidate string
	if filepath.IsAbs(input) {
		candidate = filepath.Clean(input)
	} else {
		candidate = filepath.Clean(filepath.Join(workspace, input))
	}

	rel, err := filepath.Rel(workspace, candidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("path escapes workspace: %s", input)
	}
	return candidate, nil
}

func getStringArg(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key]; ok {
		switch s := v.(type) {
		case string:
			return s
		case fmt.Stringer:
			return s.String()
		}
	}
	return fallback
}

func getBoolArg(args map[string]interface{}, key string, fallback bool) bool {
	if v, ok := args[key]; ok {
		switch b := v.(type) {
		case bool:
			return b
		case string:
			parsed, err := strconv.ParseBool(strings.TrimSpace(b))
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}

func getIntArg(args map[string]interface{}, key string, fallback int) int {
	if v, ok := args[key]; ok {
		switch n := v.(type) {
		case int:
			return n
		case int32:
			return int(n)
		case int64:
			return int(n)
		case float32:
			return int(n)
		case float64:
			return int(n)
		case string:
			parsed, err := strconv.Atoi(strings.TrimSpace(n))
			if err == nil {
				return parsed
			}
		}
	}
	return fallback
}

func fileSize(info os.FileInfo) int64 {
	if info == nil {
		return 0
	}
	return info.Size()
}
