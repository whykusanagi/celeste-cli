// ElevenLabs text-to-speech tool.
//
// Ported from celeste-tts-bot/internal/tts/client.go. Generates speech
// audio via the ElevenLabs API and saves/plays it locally.
package builtin

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/config"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// TTSTool generates speech audio via ElevenLabs.
type TTSTool struct {
	BaseTool
}

func NewTTSTool() *TTSTool {
	return &TTSTool{
		BaseTool: BaseTool{
			ToolName: "generate_speech",
			ToolDescription: "Generate speech audio from text using ElevenLabs TTS. " +
				"Saves an MP3 file to the workspace. API key and voice ID are read from config " +
				"(set via /voice set-key and /voice set-voice). " +
				"IMPORTANT: voice_id must be the alphanumeric ID (e.g., 'aOOmAwwiZAflD25S7sXQ'), " +
				"NOT the display name. Use action 'voices' first to get IDs. " +
				"If a default voice is configured, you do not need to pass voice_id. " +
				"For batch mode, the tool auto-detects SSML from the clips file.",
			ToolParameters: json.RawMessage(`{
				"type": "object",
				"properties": {
					"action": {
						"type": "string",
						"enum": ["speak", "generate", "play", "batch", "voices", "setup", "history", "download", "sound", "mix"],
						"description": "Actions: 'speak' generates TTS+plays, 'generate' saves TTS to file, 'play' plays an EXISTING audio file (NEVER use bash afplay), 'batch' processes clips JSON, 'voices' lists voices, 'setup' saves config, 'history' lists recent generations, 'download' downloads by history_item_id, 'sound' generates sound effects, 'mix' layers tracks via ffmpeg. IMPORTANT: Do NOT use SSML tags — ElevenLabs v3 speaks them as literal words. Write natural text only. For effects like laughing, breathing, moans — generate those as separate sound effects using action='sound', then layer them in the mix."
					},
					"history_item_id": {
						"type": "string",
						"description": "ElevenLabs history item ID for download action."
					},
					"text": {
						"type": "string",
						"description": "For speak/generate: text to convert to speech. For sound: description of the sound effect (e.g., 'soft fingertip tapping on glass, close proximity ASMR, binaural')."
					},
					"duration_seconds": {
						"type": "number",
						"description": "Duration for sound effect generation in seconds (0.5-22). Default: 5."
					},
					"tracks": {
						"type": "array",
						"items": {
							"type": "object",
							"properties": {
								"file": {"type": "string", "description": "Path to audio file"},
								"volume": {"type": "number", "description": "Volume 0.0-1.0 (default 1.0). Ambient beds should be 0.15-0.3, effects 0.4-0.7."},
								"delay": {"type": "number", "description": "Start time in SECONDS on the timeline — when this track begins. Example: 45.0 means it starts at the 45-second mark of the output."},
								"loop": {"type": "boolean", "description": "Loop this track continuously for the full output duration. Use for ambient beds (breathing, rain, etc)."},
								"end_at": {"type": "number", "description": "Fade out and stop this track at this second mark. 0 = play to natural end."}
							},
							"required": ["file"]
						},
						"description": "IMPORTANT: Plan your mix on a timeline BEFORE calling. Each track needs a start time (delay). The SAME file can appear MULTIPLE times at different positions — use this for repeating SFX like breathing, tapping, or heartbeats at specific moments. First track = voice/base (delay:0). Ambient beds = loop:true, low volume. Timed SFX = specific delay matching the voice content. Example timeline for a 60s track: [{voice.mp3, vol:1.0, delay:0}, {breathing.mp3, vol:0.2, delay:0, loop:true}, {whisper.mp3, vol:0.5, delay:10}, {tap.mp3, vol:0.4, delay:20}, {tap.mp3, vol:0.4, delay:35}, {tap.mp3, vol:0.3, delay:50}, {moan.mp3, vol:0.6, delay:45}]"
					},
					"filename": {
						"type": "string",
						"description": "Output filename (default: speech_<timestamp>.mp3). For batch: output directory."
					},
					"voice_id": {
						"type": "string",
						"description": "ElevenLabs voice ID. For setup: saves as default. For speak/generate: overrides default."
					},
					"api_key": {
						"type": "string",
						"description": "ElevenLabs API key. For setup action only — saves to config."
					},
					"file": {
						"type": "string",
						"description": "Path to clips JSON file for batch action. Supports both v1 (plain text) and v2 (SSML) formats."
					},
					"ssml": {
						"type": "boolean",
						"description": "DEPRECATED — ElevenLabs v3 does not support SSML. Any XML tags are automatically stripped. Write natural text instead."
					}
				},
				"required": ["action"]
			}`),
			ReadOnly: false,
		},
	}
}

func (t *TTSTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	action, _ := input["action"].(string)

	// Load from config first, env as fallback
	cfg, _ := config.Load()
	apiKey := cfg.ElevenLabsAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("ELEVEN_LABS_API_KEY")
	}
	if apiKey == "" {
		apiKey = os.Getenv("ELEVENLABS_API_KEY")
	}
	defaultVoice := cfg.ElevenLabsVoiceID
	if defaultVoice == "" {
		defaultVoice = os.Getenv("CELESTE_VOICE_ID")
	}

	switch action {
	case "setup":
		newKey, _ := input["api_key"].(string)
		newVoice, _ := input["voice_id"].(string)
		if newKey == "" && newVoice == "" {
			// Show current config
			maskedKey := "not set"
			if apiKey != "" {
				if len(apiKey) > 8 {
					maskedKey = apiKey[:4] + "..." + apiKey[len(apiKey)-4:]
				} else {
					maskedKey = "***"
				}
			}
			voice := defaultVoice
			if voice == "" {
				voice = "not set"
			}
			return tools.ToolResult{
				Content: fmt.Sprintf("ElevenLabs TTS config:\n  API Key: %s\n  Voice ID: %s\n\nUse setup with api_key and/or voice_id to configure.", maskedKey, voice),
			}, nil
		}
		if newKey != "" {
			cfg.ElevenLabsAPIKey = newKey
		}
		if newVoice != "" {
			cfg.ElevenLabsVoiceID = newVoice
		}
		if err := config.Save(cfg); err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("Failed to save config: %v", err), Error: true}, nil
		}
		msg := "TTS config saved."
		if newKey != "" {
			msg += " API key updated."
		}
		if newVoice != "" {
			msg += fmt.Sprintf(" Voice ID: %s", newVoice)
		}
		return tools.ToolResult{Content: msg}, nil

	case "voices":
		if apiKey == "" {
			return tools.ToolResult{Content: "ElevenLabs API key not configured. Use generate_speech with action 'setup' to set it.", Error: true}, nil
		}
		return listVoices(ctx, apiKey)

	case "play":
		filename, _ := input["filename"].(string)
		if filename == "" {
			return tools.ToolResult{Content: "'filename' required — path to audio file to play", Error: true}, nil
		}
		if _, err := os.Stat(filename); err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("File not found: %s", filename), Error: true}, nil
		}
		if err := playAudio(filename); err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("Playback failed: %v", err), Error: true}, nil
		}
		return tools.ToolResult{
			Content:  fmt.Sprintf("Playing: %s", filename),
			Metadata: map[string]any{"filename": filename, "playing": true},
		}, nil

	case "speak", "generate":
		if apiKey == "" {
			return tools.ToolResult{Content: "ELEVEN_LABS_API_KEY not set", Error: true}, nil
		}
		text, _ := input["text"].(string)
		if strings.TrimSpace(text) == "" {
			return tools.ToolResult{Content: "'text' is required for speak/generate", Error: true}, nil
		}

		voiceID, _ := input["voice_id"].(string)
		if voiceID == "" {
			voiceID = defaultVoice
		}
		if voiceID == "" {
			return tools.ToolResult{Content: "No voice ID — set CELESTE_VOICE_ID or pass voice_id", Error: true}, nil
		}

		useSSML, _ := input["ssml"].(bool)

		filename, _ := input["filename"].(string)
		if filename == "" {
			filename = fmt.Sprintf("speech_%d.mp3", time.Now().Unix())
		}

		if progress != nil {
			mode := "text"
			if useSSML {
				mode = "SSML"
			}
			progress <- tools.ProgressEvent{
				ToolName: "generate_speech",
				Message:  fmt.Sprintf("Generating speech [%s] (%d chars)...", mode, len(text)),
			}
		}

		audioData, err := generateSpeech(ctx, apiKey, voiceID, text, useSSML)
		if err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("TTS generation failed: %v", err), Error: true}, nil
		}

		// Save to file
		dir := filepath.Dir(filename)
		if dir != "." && dir != "" {
			os.MkdirAll(dir, 0755)
		}
		if err := os.WriteFile(filename, audioData, 0644); err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("Failed to save audio: %v", err), Error: true}, nil
		}

		// Probe duration with ffmpeg if available
		duration := probeDuration(filename)
		result := fmt.Sprintf("Audio saved: %s (%d bytes, %d chars, duration: %.1fs)", filename, len(audioData), len(text), duration)
		if duration > 0 && duration < 10 {
			result += fmt.Sprintf("\nWARNING: Track is only %.1fs — if you need a longer track, write a longer script (~150 chars per 10 seconds of speech).", duration)
		}

		// Play if action is "speak"
		if action == "speak" {
			if err := playAudio(filename); err != nil {
				result += fmt.Sprintf("\nPlayback failed: %v", err)
			} else {
				result += "\nPlayback started."
			}
		}

		return tools.ToolResult{
			Content: result,
			Metadata: map[string]any{
				"filename": filename,
				"bytes":    len(audioData),
				"text_len": len(text),
				"voice_id": voiceID,
				"ssml":     useSSML,
				"played":   action == "speak",
			},
		}, nil

	case "batch":
		if apiKey == "" {
			return tools.ToolResult{Content: "ElevenLabs API key not configured. Use /voice set-key or action 'setup'.", Error: true}, nil
		}
		filePath, _ := input["file"].(string)
		if filePath == "" {
			return tools.ToolResult{Content: "'file' is required for batch action", Error: true}, nil
		}

		voiceID, _ := input["voice_id"].(string)
		if voiceID == "" {
			voiceID = defaultVoice
		}
		if voiceID == "" {
			return tools.ToolResult{Content: "No voice ID configured. Use /voice set-voice or pass voice_id.", Error: true}, nil
		}

		outDir, _ := input["filename"].(string)
		if outDir == "" {
			outDir = filepath.Dir(filePath)
		}

		return executeBatch(ctx, apiKey, voiceID, filePath, outDir, progress)

	case "history":
		if apiKey == "" {
			return tools.ToolResult{Content: "ElevenLabs API key not configured.", Error: true}, nil
		}
		return fetchHistory(ctx, apiKey)

	case "download":
		if apiKey == "" {
			return tools.ToolResult{Content: "ElevenLabs API key not configured.", Error: true}, nil
		}
		itemID, _ := input["history_item_id"].(string)
		if itemID == "" {
			return tools.ToolResult{Content: "'history_item_id' required. Use action 'history' to find IDs.", Error: true}, nil
		}
		filename, _ := input["filename"].(string)
		if filename == "" {
			filename = fmt.Sprintf("%s.mp3", itemID)
		}
		return downloadHistoryItem(ctx, apiKey, itemID, filename)

	case "sound":
		if apiKey == "" {
			return tools.ToolResult{Content: "ElevenLabs API key not configured.", Error: true}, nil
		}
		text, _ := input["text"].(string)
		if strings.TrimSpace(text) == "" {
			return tools.ToolResult{Content: "'text' required — describe the sound effect (e.g., 'soft tapping on glass, ASMR binaural')", Error: true}, nil
		}
		duration := 5.0
		if d, ok := input["duration_seconds"].(float64); ok && d > 0 {
			duration = d
			if duration > 22 {
				duration = 22
			}
		}
		filename, _ := input["filename"].(string)
		if filename == "" {
			filename = fmt.Sprintf("sfx_%d.mp3", time.Now().Unix())
		}

		if progress != nil {
			progress <- tools.ProgressEvent{
				ToolName: "generate_speech",
				Message:  fmt.Sprintf("Generating sound effect (%.0fs): %s", duration, truncateTTSText(text, 50)),
			}
		}

		audioData, err := generateSoundEffect(ctx, apiKey, text, duration)
		if err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("Sound generation failed: %v", err), Error: true}, nil
		}

		dir := filepath.Dir(filename)
		if dir != "." && dir != "" {
			os.MkdirAll(dir, 0755)
		}
		if err := os.WriteFile(filename, audioData, 0644); err != nil {
			return tools.ToolResult{Content: fmt.Sprintf("Write failed: %v", err), Error: true}, nil
		}

		return tools.ToolResult{
			Content: fmt.Sprintf("Sound effect saved: %s (%d bytes, %.0fs)\nPrompt: %s", filename, len(audioData), duration, text),
			Metadata: map[string]any{
				"filename": filename,
				"bytes":    len(audioData),
				"duration": duration,
			},
		}, nil

	case "mix":
		tracksRaw, ok := input["tracks"].([]any)
		if !ok || len(tracksRaw) < 2 {
			return tools.ToolResult{Content: "'tracks' required — array of at least 2 tracks [{file, volume, delay}]. REMINDER: always pass 'filename' for the output.", Error: true}, nil
		}
		filename, _ := input["filename"].(string)
		if filename == "" {
			return tools.ToolResult{
				Content: "ERROR: 'filename' is required for mix action. Pass the desired output filename (e.g., 'test_asmr_validation.mp3'). Do NOT omit it — auto-generated names won't match what the user asked for.",
				Error:   true,
			}, nil
		}
		return mixTracks(tracksRaw, filename, progress)

	default:
		return tools.ToolResult{Content: "Invalid action. Use speak, generate, batch, sound, mix, voices, setup, history, or download.", Error: true}, nil
	}
}

// clipsFile represents both v1 (plain text) and v2 (SSML) clip formats.
type clipsFile struct {
	Clips []struct {
		Name             string   `json:"name"`
		Script           string   `json:"script"`
		DurationEstimate string   `json:"duration_estimate"`
		Tags             []string `json:"tags"`
	} `json:"clips"`
	Version       string `json:"version"`
	Persona       string `json:"persona"`
	SSMLOptimized bool   `json:"ssml_optimized"`
}

// executeBatch processes a clips JSON file, generating audio for each clip.
func executeBatch(ctx context.Context, apiKey, voiceID, filePath, outDir string, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Failed to read %s: %v", filePath, err), Error: true}, nil
	}

	var clips clipsFile
	if err := json.Unmarshal(data, &clips); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Failed to parse clips JSON: %v", err), Error: true}, nil
	}

	if len(clips.Clips) == 0 {
		return tools.ToolResult{Content: "No clips found in file", Error: true}, nil
	}

	os.MkdirAll(outDir, 0755)
	useSSML := clips.SSMLOptimized

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Batch TTS: %d clips from %s (SSML: %v)\n\n", len(clips.Clips), filepath.Base(filePath), useSSML))

	var generated, skipped, failed int
	for i, clip := range clips.Clips {
		outFile := filepath.Join(outDir, clip.Name+".mp3")

		// Skip if already generated (idempotent — don't re-generate on retry)
		if info, err := os.Stat(outFile); err == nil && info.Size() > 0 {
			sb.WriteString(fmt.Sprintf("  SKIP  %s — already exists (%d bytes)\n", clip.Name, info.Size()))
			skipped++
			continue
		}

		if progress != nil {
			progress <- tools.ProgressEvent{
				ToolName: "generate_speech",
				Message:  fmt.Sprintf("Generating clip %d/%d: %s", i+1, len(clips.Clips), clip.Name),
				Percent:  float64(i) / float64(len(clips.Clips)),
			}
		}

		audioData, err := generateSpeech(ctx, apiKey, voiceID, clip.Script, useSSML)
		if err != nil {
			// On timeout, the clip may have been generated server-side
			if strings.Contains(err.Error(), "deadline exceeded") || strings.Contains(err.Error(), "context canceled") {
				sb.WriteString(fmt.Sprintf("  TIMEOUT  %s — may have completed on ElevenLabs. Check your dashboard and download manually, or retry (existing files are skipped).\n", clip.Name))
			} else {
				sb.WriteString(fmt.Sprintf("  FAIL  %s: %v\n", clip.Name, err))
			}
			failed++
			continue
		}

		if err := os.WriteFile(outFile, audioData, 0644); err != nil {
			sb.WriteString(fmt.Sprintf("  FAIL  %s: write error: %v\n", clip.Name, err))
			failed++
			continue
		}

		sb.WriteString(fmt.Sprintf("  OK    %s → %s (%d bytes)\n", clip.Name, outFile, len(audioData)))
		generated++
	}

	if skipped > 0 || failed > 0 {
		sb.WriteString(fmt.Sprintf("\nSummary: %d generated, %d skipped (existing), %d failed\n", generated, skipped, failed))
		if failed > 0 {
			sb.WriteString("Re-run batch to retry failed clips (completed clips are skipped automatically).\n")
		}
	}

	return tools.ToolResult{
		Content: sb.String(),
		Metadata: map[string]any{
			"clips_count": len(clips.Clips),
			"output_dir":  outDir,
			"ssml":        useSSML,
		},
	}, nil
}

// generateSpeech calls the ElevenLabs TTS API.
// ElevenLabs eleven_v3 does NOT support SSML — it uses natural language.
// Any XML/SSML tags are stripped before sending to prevent them being spoken.
// Action cues like [laughs], [whispers] are also stripped — generate those
// as separate sound effects instead.
func generateSpeech(ctx context.Context, apiKey, voiceID, text string, ssml bool) ([]byte, error) {
	// Strip SSML/XML tags — ElevenLabs v3 speaks them as literal text
	cleanText := stripTagsAndCues(text)

	body, _ := json.Marshal(map[string]any{
		"text":     cleanText,
		"model_id": "eleven_v3",
		"voice_settings": map[string]any{
			"stability":        0.5,
			"similarity_boost": 0.75,
		},
	})

	url := fmt.Sprintf("https://api.elevenlabs.io/v1/text-to-speech/%s", voiceID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", apiKey)

	client := &http.Client{Timeout: 120 * time.Second} // long scripts take time
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		// Truncate error body to avoid dumping SSML/script content into chat
		errStr := string(errBody)
		if len(errStr) > 200 {
			errStr = errStr[:200] + "..."
		}
		return nil, fmt.Errorf("ElevenLabs API error %d: %s", resp.StatusCode, errStr)
	}

	return io.ReadAll(resp.Body)
}

// listVoices fetches available voices from ElevenLabs.
func listVoices(ctx context.Context, apiKey string) (tools.ToolResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.elevenlabs.io/v1/voices", nil)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Request failed: %v", err), Error: true}, nil
	}
	req.Header.Set("xi-api-key", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("API call failed: %v", err), Error: true}, nil
	}
	defer resp.Body.Close()

	var result struct {
		Voices []struct {
			VoiceID string            `json:"voice_id"`
			Name    string            `json:"name"`
			Labels  map[string]string `json:"labels"`
		} `json:"voices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Parse failed: %v", err), Error: true}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Available voices (%d):\n\n", len(result.Voices)))
	for _, v := range result.Voices {
		labels := ""
		if len(v.Labels) > 0 {
			parts := make([]string, 0, len(v.Labels))
			for k, val := range v.Labels {
				parts = append(parts, k+":"+val)
			}
			labels = " [" + strings.Join(parts, ", ") + "]"
		}
		sb.WriteString(fmt.Sprintf("  %s — %s%s\n", v.VoiceID, v.Name, labels))
	}

	return tools.ToolResult{Content: sb.String()}, nil
}

// fetchHistory lists recent TTS generations from the ElevenLabs history API.
func fetchHistory(ctx context.Context, apiKey string) (tools.ToolResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.elevenlabs.io/v1/history?page_size=20", nil)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Request failed: %v", err), Error: true}, nil
	}
	req.Header.Set("xi-api-key", apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("API call failed: %v", err), Error: true}, nil
	}
	defer resp.Body.Close()

	var result struct {
		History []struct {
			HistoryItemID string `json:"history_item_id"`
			VoiceID       string `json:"voice_id"`
			VoiceName     string `json:"voice_name"`
			Text          string `json:"text"`
			DateUnix      int64  `json:"date_unix"`
			State         string `json:"state"`
			ContentType   string `json:"content_type"`
		} `json:"history"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Parse failed: %v", err), Error: true}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("ElevenLabs History (last %d):\n\n", len(result.History)))
	for _, h := range result.History {
		ts := time.Unix(h.DateUnix, 0).Format("Jan 2 15:04")
		textPreview := h.Text
		// Strip SSML tags for preview
		for strings.Contains(textPreview, "<") {
			start := strings.Index(textPreview, "<")
			end := strings.Index(textPreview, ">")
			if end == -1 {
				break
			}
			textPreview = textPreview[:start] + textPreview[end+1:]
		}
		if len(textPreview) > 60 {
			textPreview = textPreview[:57] + "..."
		}
		textPreview = strings.TrimSpace(textPreview)

		sb.WriteString(fmt.Sprintf("  %s  %s  [%s]  %s\n    text: %s\n",
			h.HistoryItemID, ts, h.State, h.VoiceName, textPreview))
	}
	sb.WriteString("\nDownload: use action 'download' with history_item_id and filename")

	return tools.ToolResult{Content: sb.String()}, nil
}

// downloadHistoryItem downloads a generated audio file from ElevenLabs history.
func downloadHistoryItem(ctx context.Context, apiKey, itemID, filename string) (tools.ToolResult, error) {
	url := fmt.Sprintf("https://api.elevenlabs.io/v1/history/%s/audio", itemID)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Request failed: %v", err), Error: true}, nil
	}
	req.Header.Set("xi-api-key", apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Download failed: %v", err), Error: true}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return tools.ToolResult{Content: fmt.Sprintf("ElevenLabs returned %d for history item %s", resp.StatusCode, itemID), Error: true}, nil
	}

	audioData, err := io.ReadAll(resp.Body)
	if err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Read failed: %v", err), Error: true}, nil
	}

	dir := filepath.Dir(filename)
	if dir != "." && dir != "" {
		os.MkdirAll(dir, 0755)
	}
	if err := os.WriteFile(filename, audioData, 0644); err != nil {
		return tools.ToolResult{Content: fmt.Sprintf("Write failed: %v", err), Error: true}, nil
	}

	return tools.ToolResult{
		Content: fmt.Sprintf("Downloaded: %s (%d bytes) from history item %s", filename, len(audioData), itemID),
		Metadata: map[string]any{
			"filename":        filename,
			"bytes":           len(audioData),
			"history_item_id": itemID,
		},
	}, nil
}

// generateSoundEffect calls the ElevenLabs Sound Generation API.
func generateSoundEffect(ctx context.Context, apiKey, text string, durationSeconds float64) ([]byte, error) {
	payload := map[string]any{
		"text":             text,
		"duration_seconds": durationSeconds,
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.elevenlabs.io/v1/sound-generation", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("xi-api-key", apiKey)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		errStr := string(errBody)
		if len(errStr) > 200 {
			errStr = errStr[:200] + "..."
		}
		return nil, fmt.Errorf("Sound API error %d: %s", resp.StatusCode, errStr)
	}

	return io.ReadAll(resp.Body)
}

// mixTrack represents a single audio track for mixing.
type mixTrack struct {
	File   string  `json:"file"`
	Volume float64 `json:"volume"`
	Delay  float64 `json:"delay"`  // start time in seconds
	Loop   bool    `json:"loop"`   // loop for full duration
	EndAt  float64 `json:"end_at"` // fade out at this second (0 = natural end)
}

// mixTracks combines multiple audio files using ffmpeg.
func mixTracks(tracksRaw []any, output string, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	// Check ffmpeg is available
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return tools.ToolResult{Content: "ffmpeg not found — install it to mix audio tracks", Error: true}, nil
	}

	// Parse tracks
	var tracks []mixTrack
	for _, raw := range tracksRaw {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		t := mixTrack{Volume: 1.0}
		if f, ok := m["file"].(string); ok {
			t.File = f
		}
		if v, ok := m["volume"].(float64); ok {
			t.Volume = v
		}
		if d, ok := m["delay"].(float64); ok {
			t.Delay = d
		}
		if l, ok := m["loop"].(bool); ok {
			t.Loop = l
		}
		if e, ok := m["end_at"].(float64); ok {
			t.EndAt = e
		}
		if t.File == "" {
			continue
		}
		// Verify file exists
		if _, err := os.Stat(t.File); err != nil {
			return tools.ToolResult{
				Content: fmt.Sprintf("Track file not found: %s", t.File),
				Error:   true,
			}, nil
		}
		tracks = append(tracks, t)
	}

	if len(tracks) < 2 {
		return tools.ToolResult{Content: "Need at least 2 tracks to mix", Error: true}, nil
	}

	// Validation: reject mixes where non-primary tracks have no timeline placement.
	// If everything is at delay:0 without loop, the audio stacks instead of layering.
	allAtZero := true
	for i, t := range tracks {
		if i == 0 {
			continue // primary track is always at 0
		}
		if t.Delay > 0 || t.Loop {
			allAtZero = false
			break
		}
	}
	if allAtZero && len(tracks) > 3 {
		return tools.ToolResult{
			Content: "REJECTED: All tracks have delay:0 and no loop — this will stack everything at the start. " +
				"You MUST plan a timeline: set 'delay' (start time in seconds) for each SFX track, or 'loop':true for ambient beds. " +
				"Example: voice at 0s, breathing (loop, 0.2 vol), kiss at 10s, moan at 25s, growl at 40s. " +
				"Re-call with proper delay values based on the voice track duration.",
			Error: true,
		}, nil
	}

	if progress != nil {
		progress <- tools.ProgressEvent{
			ToolName: "generate_speech",
			Message:  fmt.Sprintf("Mixing %d tracks → %s", len(tracks), output),
		}
	}

	// Build ffmpeg command with proper timeline-based mixing.
	// Uses overlay approach: each track is volume-adjusted, delayed to its
	// start time, optionally looped, then all are mixed without normalization.
	var args []string
	for _, t := range tracks {
		if t.Loop {
			// Loop this input infinitely (ffmpeg trims to longest input)
			args = append(args, "-stream_loop", "-1")
		}
		args = append(args, "-i", t.File)
	}

	// Build filter graph
	var filterParts []string
	var mixInputs []string
	for i, t := range tracks {
		label := fmt.Sprintf("t%d", i)
		var filters []string

		// Volume
		filters = append(filters, fmt.Sprintf("volume=%.2f", t.Volume))

		// Trim end if specified
		if t.EndAt > 0 {
			filters = append(filters, fmt.Sprintf("atrim=end=%.1f", t.EndAt))
		}

		// Delay (start time on timeline) — adelay in ms, applied to both channels
		if t.Delay > 0 {
			delayMs := int(t.Delay * 1000)
			filters = append(filters, fmt.Sprintf("adelay=%d|%d", delayMs, delayMs))
		}

		// Ensure consistent sample format for mixing
		filterLine := fmt.Sprintf("[%d]%s[%s]", i, strings.Join(filters, ","), label)
		filterParts = append(filterParts, filterLine)
		mixInputs = append(mixInputs, fmt.Sprintf("[%s]", label))
	}

	// Use amix with normalize=0 to prevent volume reduction when layering.
	// duration=first means output length matches the first (primary) track.
	filterGraph := strings.Join(filterParts, ";") + ";" +
		strings.Join(mixInputs, "") +
		fmt.Sprintf("amix=inputs=%d:duration=first:normalize=0", len(tracks))

	args = append(args, "-filter_complex", filterGraph, "-y", output)

	cmd := exec.Command("ffmpeg", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		errMsg := string(out)
		if len(errMsg) > 300 {
			errMsg = errMsg[:300] + "..."
		}
		return tools.ToolResult{
			Content: fmt.Sprintf("ffmpeg failed: %v\n%s", err, errMsg),
			Error:   true,
		}, nil
	}

	// Verify output exists and get details
	absOutput, _ := filepath.Abs(output)
	info, err := os.Stat(absOutput)
	if err != nil {
		return tools.ToolResult{
			Content: fmt.Sprintf("ffmpeg ran but output file NOT found at: %s\nThis means the mix silently failed.", absOutput),
			Error:   true,
		}, nil
	}
	size := info.Size()
	duration := probeDuration(absOutput)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("OUTPUT FILE: %s\n", absOutput))
	sb.WriteString(fmt.Sprintf("Size: %d bytes | Duration: %.1fs\n\n", size, duration))
	sb.WriteString(fmt.Sprintf("Mixed %d tracks:\n", len(tracks)))
	for _, t := range tracks {
		sb.WriteString(fmt.Sprintf("  %5.1fs  %3.0f%%  %s", t.Delay, t.Volume*100, filepath.Base(t.File)))
		if t.Loop {
			sb.WriteString("  [loop]")
		}
		if t.EndAt > 0 {
			sb.WriteString(fmt.Sprintf("  [until %.1fs]", t.EndAt))
		}
		sb.WriteString("\n")
	}

	return tools.ToolResult{
		Content: sb.String(),
		Metadata: map[string]any{
			"filename": output,
			"bytes":    size,
			"tracks":   len(tracks),
		},
	}, nil
}

// probeDuration uses ffprobe to get the duration of an audio file in seconds.
// Returns 0 if ffprobe isn't available or fails.
func probeDuration(filename string) float64 {
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

// stripTagsAndCues removes SSML/XML tags and bracketed action cues from text.
// ElevenLabs v3 speaks these as literal words — they need to be removed.
// Examples: <prosody rate="slow">text</prosody> → text
//
//	[laughs] → (removed)
//	[whispers] text [/whispers] → text
func stripTagsAndCues(text string) string {
	// Strip XML/SSML tags
	result := text
	for strings.Contains(result, "<") {
		start := strings.Index(result, "<")
		end := strings.Index(result[start:], ">")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+1:]
	}
	// Strip bracketed action cues like [laughs], [whispers], [purr], [long pause]
	for {
		start := strings.Index(result, "[")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "]")
		if end == -1 {
			break
		}
		result = result[:start] + result[start+end+1:]
	}
	// Clean up multiple spaces and trim
	for strings.Contains(result, "  ") {
		result = strings.ReplaceAll(result, "  ", " ")
	}
	return strings.TrimSpace(result)
}

func truncateTTSText(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

// playAudio plays an audio file through the system's default audio device.
func playAudio(filename string) error {
	var cmd *exec.Cmd

	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("afplay", filename)
	case "linux":
		if _, err := exec.LookPath("mpg123"); err == nil {
			cmd = exec.Command("mpg123", filename)
		} else if _, err := exec.LookPath("ffplay"); err == nil {
			cmd = exec.Command("ffplay", "-nodisp", "-autoexit", filename)
		} else {
			return fmt.Errorf("no audio player found (install mpg123 or ffplay)")
		}
	default:
		return fmt.Errorf("unsupported OS for audio playback: %s", runtime.GOOS)
	}

	// Start async — don't block the tool call
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
