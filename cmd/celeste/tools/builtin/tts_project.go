// Audio project pipeline — declarative timeline-based audio rendering.
//
// The model constructs a JSON project file describing the mix timeline,
// then calls render. The tool handles all ffmpeg complexity, progress
// reporting, and validation. Similar to how a video renderer takes a
// project file and produces output.
package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// AudioProject is the declarative project file schema.
type AudioProject struct {
	Name    string       `json:"name"`    // project name
	Output  string       `json:"output"`  // output filename (required)
	Tracks  []AudioTrack `json:"tracks"`  // timeline tracks
	Version string       `json:"version"` // schema version
}

// AudioTrack is a single audio element on the timeline.
type AudioTrack struct {
	File   string  `json:"file"`            // path to audio file
	Role   string  `json:"role"`            // "voice", "bed", "sfx" — determines behavior
	Volume float64 `json:"volume"`          // 0.0-1.0
	Start  float64 `json:"start"`           // start time in seconds on timeline
	End    float64 `json:"end,omitempty"`   // optional: cut at this second (0 = play full)
	Loop   bool    `json:"loop,omitempty"`  // loop for full output duration (beds only)
	Label  string  `json:"label,omitempty"` // human-readable label for progress display
}

// AudioProjectTool handles the project-based render pipeline.
type AudioProjectTool struct {
	BaseTool
}

func NewAudioProjectTool() *AudioProjectTool {
	return &AudioProjectTool{
		BaseTool: BaseTool{
			ToolName: "audio_render",
			ToolDescription: "Render an audio project from a timeline manifest. " +
				"Step 1: Create a project JSON file with tracks on a timeline. " +
				"Step 2: Call this tool with action 'render' to produce the final mix. " +
				"The tool validates the project, checks all source files exist, and renders via ffmpeg. " +
				"Actions: 'render' processes a project file, 'validate' checks without rendering, 'create' writes a project file from params.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"action": {
						"type": "string",
						"enum": ["render", "validate", "create"],
						"description": "Action: 'render' produces final audio from project file, 'validate' checks project without rendering, 'create' writes a project JSON from the provided tracks."
					},
					"file": {
						"type": "string",
						"description": "Path to project JSON file (for render/validate actions)."
					},
					"name": {
						"type": "string",
						"description": "Project name (for create action)."
					},
					"output": {
						"type": "string",
						"description": "Output filename (for create action)."
					},
					"tracks": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"file": {"type": "string", "description": "Audio source file path"},
								"role": {"type": "string", "enum": ["voice", "bed", "sfx"], "description": "Track role: 'voice' is the primary track (always at start), 'bed' loops as ambient background, 'sfx' plays once at its start time"},
								"volume": {"type": "number", "description": "Volume 0.0-1.0"},
								"start": {"type": "number", "description": "Start time in seconds on the timeline"},
								"end": {"type": "number", "description": "Cut track at this second (0 = full length)"},
								"loop": {"type": "boolean", "description": "Loop for full duration (auto-set for bed role)"},
								"label": {"type": "string", "description": "Human-readable label"}
							},
							"required": ["file", "role", "start"]
						},
						"description": "Track list for create action. Each track needs file, role, and start time. Roles: 'voice' (primary, start:0), 'bed' (ambient loop, low volume), 'sfx' (one-shot at specific time)."
					}
				},
				"required": ["action"]
			}`),
			ReadOnly: false,
		},
	}
}

func (t *AudioProjectTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	action, _ := input["action"].(string)

	switch action {
	case "create":
		return t.handleCreate(input)
	case "validate":
		filePath, _ := input["file"].(string)
		if filePath == "" {
			return tools.ToolResult{Content: "'file' required for validate action", Error: true}, nil
		}
		return t.handleValidate(filePath)
	case "render":
		filePath, _ := input["file"].(string)
		if filePath == "" {
			return tools.ToolResult{Content: "'file' required for render action", Error: true}, nil
		}
		return t.handleRender(ctx, filePath, progress)
	default:
		return tools.ToolResult{Content: "Invalid action. Use 'create', 'validate', or 'render'.", Error: true}, nil
	}
}

func (t *AudioProjectTool) handleCreate(input map[string]any) (tools.ToolResult, error) {
	name, _ := input["name"].(string)
	output, _ := input["output"].(string)
	tracksRaw, _ := input["tracks"].([]any)

	if output == "" {
		return tools.ToolResult{Content: "'output' required — the final audio filename", Error: true}, nil
	}
	if len(tracksRaw) == 0 {
		return tools.ToolResult{Content: "'tracks' required — at least one track", Error: true}, nil
	}

	project := AudioProject{
		Name:    name,
		Output:  output,
		Version: "1.0",
	}

	hasVoice := false
	for _, raw := range tracksRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		track := AudioTrack{Volume: 1.0}
		if f, ok := m["file"].(string); ok {
			track.File = f
		}
		if r, ok := m["role"].(string); ok {
			track.Role = r
		}
		if v, ok := m["volume"].(float64); ok {
			track.Volume = v
		}
		if s, ok := m["start"].(float64); ok {
			track.Start = s
		}
		if e, ok := m["end"].(float64); ok {
			track.End = e
		}
		if l, ok := m["loop"].(bool); ok {
			track.Loop = l
		}
		if lb, ok := m["label"].(string); ok {
			track.Label = lb
		}

		// Auto-set loop for bed role
		if track.Role == "bed" {
			track.Loop = true
		}
		if track.Role == "voice" {
			hasVoice = true
		}

		project.Tracks = append(project.Tracks, track)
	}

	if !hasVoice {
		return tools.ToolResult{Content: "Project must have at least one track with role 'voice' (the primary audio).", Error: true}, nil
	}

	// Write project file
	// Ensure output is an audio file, not a JSON
	if strings.HasSuffix(output, ".json") {
		output = strings.TrimSuffix(output, ".json") + ".mp3"
	}
	if !strings.HasSuffix(output, ".mp3") && !strings.HasSuffix(output, ".wav") {
		output = output + ".mp3"
	}
	project.Output = output
	projectPath := strings.TrimSuffix(output, filepath.Ext(output)) + ".project.json"
	data, _ := json.MarshalIndent(project, "", "  ")
	if err := os.WriteFile(projectPath, data, 0644); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Failed to write project: %v", err), Error: true}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Project created: %s\n", projectPath))
	sb.WriteString(fmt.Sprintf("Output target: %s\n", output))
	sb.WriteString(fmt.Sprintf("Tracks: %d\n\n", len(project.Tracks)))
	sb.WriteString("Timeline:\n")
	for _, tr := range project.Tracks {
		label := tr.Label
		if label == "" {
			label = filepath.Base(tr.File)
		}
		sb.WriteString(fmt.Sprintf("  %5.1fs  [%s]  %3.0f%%  %s", tr.Start, tr.Role, tr.Volume*100, label))
		if tr.Loop {
			sb.WriteString("  [loop]")
		}
		sb.WriteString("\n")
	}
	// Visual timeline
	sb.WriteString("\n")
	sb.WriteString(renderGantt(&project, 0))
	sb.WriteString(fmt.Sprintf("\nNext: call audio_render with action='render' file='%s'", projectPath))

	return tools.ToolResult{
		Content: sb.String(),
		Metadata: map[string]any{
			"project_file": projectPath,
			"track_count":  len(project.Tracks),
			"output":       output,
		},
	}, nil
}

func (t *AudioProjectTool) handleValidate(filePath string) (tools.ToolResult, error) {
	project, err := loadProject(filePath)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Invalid project: %v", err), Error: true}, nil
	}

	errors := validateProject(project)
	if len(errors) > 0 {
		return tools.ToolResult{
			Content: fmt.Sprintf("Project validation failed:\n- %s", strings.Join(errors, "\n- ")),
			Error:   true,
		}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Project valid: %d tracks, output: %s\n\n", len(project.Tracks), project.Output))
	sb.WriteString(renderGantt(project, 0))
	return tools.ToolResult{Content: sb.String()}, nil
}

func (t *AudioProjectTool) handleRender(ctx context.Context, filePath string, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return tools.ToolResult{Content: "ffmpeg not found — required for audio rendering", Error: true}, nil
	}

	project, err := loadProject(filePath)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Failed to load project: %v", err), Error: true}, nil
	}

	errors := validateProject(project)
	if len(errors) > 0 {
		return tools.ToolResult{
			Content: fmt.Sprintf("Project validation failed:\n- %s\n\nFix the project file and re-render.", strings.Join(errors, "\n- ")),
			Error:   true,
		}, nil
	}

	if progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: "audio_render",
			Message:  fmt.Sprintf("Rendering %d tracks → %s", len(project.Tracks), project.Output),
		}
	}

	// Build ffmpeg command from project
	var args []string
	for _, tr := range project.Tracks {
		if tr.Loop {
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-i", tr.File)
	}

	// Build filter graph
	var filterParts []string
	var mixInputs []string
	for i, tr := range project.Tracks {
		label := fmt.Sprintf("t%d", i)
		var filters []string

		filters = append(filters, fmt.Sprintf("volume=%.2f", tr.Volume))
		if tr.End > 0 {
			filters = append(filters, fmt.Sprintf("atrim=end=%.1f", tr.End))
		}
		if tr.Start > 0 {
			delayMs := int(tr.Start * 1000)
			filters = append(filters, fmt.Sprintf("adelay=%d|%d", delayMs, delayMs))
		}
		filterLine := fmt.Sprintf("[%d]%s[%s]", i, strings.Join(filters, ","), label)
		filterParts = append(filterParts, filterLine)
		mixInputs = append(mixInputs, fmt.Sprintf("[%s]", label))
	}

	if len(project.Tracks) == 1 {
		// Single track — just apply volume filter, no amix needed
		filterGraph := filterParts[0]
		args = append(args, "-filter_complex", filterGraph, "-map", "[t0]", "-y", project.Output)
	} else {
		filterGraph := strings.Join(filterParts, ";") + ";" +
			strings.Join(mixInputs, "") +
			fmt.Sprintf("amix=inputs=%d:duration=first:normalize=0", len(project.Tracks))
		args = append(args, "-filter_complex", filterGraph, "-y", project.Output)
	}

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := string(out)
		if len(errMsg) > 300 {
			errMsg = errMsg[:300] + "..."
		}
		return tools.ToolResult{
			Content: fmt.Sprintf("Render failed: %v\n%s", err, errMsg),
			Error:   true,
		}, nil
	}

	// Verify output
	absOutput, _ := filepath.Abs(project.Output)
	info, err := os.Stat(absOutput)
	if err != nil {
		return tools.ToolResult{
			Content: fmt.Sprintf("Render completed but output NOT found: %s", absOutput),
			Error:   true,
		}, nil
	}

	duration := probeAudioDuration(absOutput)

	var sb strings.Builder
	sb.WriteString("RENDER COMPLETE\n")
	sb.WriteString(fmt.Sprintf("Output: %s\n", absOutput))
	sb.WriteString(fmt.Sprintf("Size: %d bytes | Duration: %.1fs\n\n", info.Size(), duration))
	sb.WriteString(renderGantt(project, duration))

	return tools.ToolResult{
		Content: sb.String(),
		Metadata: map[string]any{
			"output":   absOutput,
			"bytes":    info.Size(),
			"duration": duration,
			"tracks":   len(project.Tracks),
		},
	}, nil
}

// renderGantt produces an ASCII timeline visualization of the project.
// Scales cleanly from 10s clips to 10-minute productions.
func renderGantt(project *AudioProject, totalDuration float64) string {
	if totalDuration <= 0 {
		// Estimate from voice track or default to 30s
		for _, tr := range project.Tracks {
			if tr.Role == "voice" {
				d := probeAudioDuration(tr.File)
				if d > 0 {
					totalDuration = d
				}
			}
		}
		if totalDuration <= 0 {
			totalDuration = 30.0
		}
	}

	width := 60 // chart width in characters
	var sb strings.Builder

	// Adaptive time format based on duration
	formatTime := func(seconds float64) string {
		if totalDuration >= 120 {
			m := int(seconds) / 60
			s := int(seconds) % 60
			if s == 0 {
				return fmt.Sprintf("%dm", m)
			}
			return fmt.Sprintf("%d:%02d", m, s)
		}
		return fmt.Sprintf("%.0fs", seconds)
	}

	// Adaptive marker count based on duration
	markerCount := 6
	if totalDuration > 180 {
		markerCount = 5
	}
	if totalDuration > 600 {
		markerCount = 4
	}

	sb.WriteString(fmt.Sprintf("Timeline (%s):\n", formatTime(totalDuration)))

	// Time axis header
	interval := totalDuration / float64(markerCount)
	sb.WriteString("  ")
	segWidth := width / markerCount
	for i := 0; i <= markerCount; i++ {
		t := float64(i) * interval
		label := formatTime(t)
		if i < markerCount {
			pad := segWidth - len(label)
			if pad < 1 {
				pad = 1
			}
			sb.WriteString(label + strings.Repeat(" ", pad))
		} else {
			sb.WriteString(label)
		}
	}
	sb.WriteString("\n")

	// Axis line
	sb.WriteString("  ├")
	for i := 0; i < width-2; i++ {
		if segWidth > 0 && i%segWidth == 0 && i > 0 {
			sb.WriteString("┼")
		} else {
			sb.WriteString("─")
		}
	}
	sb.WriteString("┤\n")

	// Role-based characters for visual distinction
	roleChar := map[string]rune{
		"voice": '█',
		"bed":   '░',
		"sfx":   '▓',
	}

	for _, tr := range project.Tracks {
		char, ok := roleChar[tr.Role]
		if !ok {
			char = '▒'
		}

		// Calculate start position
		startPos := int((tr.Start / totalDuration) * float64(width))
		if startPos >= width {
			startPos = width - 1
		}

		// Calculate end position
		var endPos int
		if tr.Loop || tr.Role == "bed" {
			endPos = width // loops fill to end
		} else if tr.End > 0 {
			endPos = int((tr.End / totalDuration) * float64(width))
		} else {
			// Probe or estimate duration
			sfxDur := probeAudioDuration(tr.File)
			if sfxDur <= 0 {
				sfxDur = 5.0
			}
			endPos = int(((tr.Start + sfxDur) / totalDuration) * float64(width))
		}
		if endPos > width {
			endPos = width
		}
		// Minimum 2 chars wide so short SFX are always visible
		if endPos-startPos < 2 {
			endPos = startPos + 2
			if endPos > width {
				endPos = width
				startPos = width - 2
			}
		}

		// Build bar
		bar := strings.Repeat(" ", startPos) + strings.Repeat(string(char), endPos-startPos)
		if len(bar) < width {
			bar += strings.Repeat(" ", width-len(bar))
		}

		// Label with role tag
		label := tr.Label
		if label == "" {
			label = filepath.Base(tr.File)
		}
		meta := fmt.Sprintf("[%s] %.0f%%", tr.Role, tr.Volume*100)
		if tr.Loop || tr.Role == "bed" {
			meta += " loop"
		}
		if tr.Start > 0 {
			meta += fmt.Sprintf(" @%s", formatTime(tr.Start))
		}

		sb.WriteString(fmt.Sprintf("  %s  %s %s\n", bar, label, meta))
	}

	// Legend
	sb.WriteString("\n  █=voice  ░=bed(loop)  ▓=sfx\n")

	return sb.String()
}

func loadProject(filePath string) (*AudioProject, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var project AudioProject
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	return &project, nil
}

func validateProject(p *AudioProject) []string {
	var errs []string

	if p.Output == "" {
		errs = append(errs, "missing 'output' field")
	}
	if len(p.Tracks) == 0 {
		errs = append(errs, "no tracks defined")
	}

	hasVoice := false
	for i, tr := range p.Tracks {
		if tr.File == "" {
			errs = append(errs, fmt.Sprintf("track %d: missing 'file'", i))
			continue
		}
		if _, err := os.Stat(tr.File); err != nil {
			errs = append(errs, fmt.Sprintf("track %d: file not found: %s", i, tr.File))
		}
		if tr.Role == "" {
			errs = append(errs, fmt.Sprintf("track %d: missing 'role' (voice/bed/sfx)", i))
		}
		if tr.Role == "voice" {
			hasVoice = true
		}
		if tr.Volume <= 0 || tr.Volume > 1.0 {
			errs = append(errs, fmt.Sprintf("track %d: volume must be 0.0-1.0, got %.2f", i, tr.Volume))
		}
	}

	if !hasVoice {
		errs = append(errs, "no track with role 'voice' — need at least one primary audio track")
	}

	return errs
}

// probeAudioDuration uses ffprobe to get duration. Returns 0 on failure.
func probeAudioDuration(filename string) float64 {
	out, err := exec.Command("ffprobe", "-v", "quiet", "-show_entries", "format=duration", "-of", "csv=p=0", filename).Output()
	if err != nil {
		return 0
	}
	s := strings.TrimSpace(string(out))
	if d, err := strconv.ParseFloat(s, 64); err == nil {
		return d
	}
	return 0
}
