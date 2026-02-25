# Design: /clear Reset, NSFW Upscale Removal, Upscale Tool Call

**Date**: 2026-02-24
**Status**: Approved

---

## Overview

Three related changes to improve session management and make image upscaling a first-class utility:

1. **`/clear` full session reset** — extend the existing command to also start a new session
2. **NSFW mode cleanup** — remove upscaling from NSFW-specific media handling
3. **`upscale_image` skill** — register image upscaling as a tool call available to all providers

---

## Feature 1: `/clear` Full Session Reset

### Current Behavior
`/clear` (commands.go:617–625) sets `ClearHistory: true` in `StateChange`, which tells the TUI to wipe the visual chat (`m.chat.Clear()`). The underlying session is preserved.

### Target Behavior
`/clear` wipes the visual chat **and** starts a fresh session — equivalent to `/clear` + `/session new` in one command.

### Architecture

**`commands/commands.go`** — extend `StateChange`:
```go
type StateChange struct {
    // existing fields...
    ClearHistory bool
    NewSession   bool  // NEW: also start a fresh session
}
```

Update the `/clear` handler to set both:
```go
StateChange: &StateChange{
    ClearHistory: true,
    NewSession:   true,
}
```

**`tui/app.go`** — handle `NewSession` alongside `ClearHistory`:
```go
if result.StateChange.ClearHistory {
    m.chat = m.chat.Clear()
}
if result.StateChange.NewSession {
    // Same session creation logic as /session new
    m.currentSession = config.NewSession()
    m.persistSession()
    m.chat = m.chat.AddSystemMessage("Session cleared, new session started.")
}
```

The system message replaces the separate messages that `/clear` and `/session new` would produce independently.

### Files Changed
- `cmd/celeste/commands/commands.go`
- `cmd/celeste/tui/app.go`

---

## Feature 2: NSFW Mode — Remove Upscaling

### Current Behavior
When in NSFW mode, the TUI (or venice/media.go) parses `upscale: <path>` in user input and routes it to `venice.UpscaleImage()`. This ties upscaling exclusively to NSFW mode.

### Target Behavior
Remove the `upscale:` media command parsing from NSFW mode. Everything else about NSFW mode stays:
- Auto-switch to Venice.ai endpoint
- Set image model to `lustify-sdxl`
- Disable skills for uncensored models

### Architecture
Locate and remove the `upscale:` case from the NSFW media command dispatch (in `venice/media.go` or `tui/app.go`). No new code — surgical deletion only.

### Files Changed
- `cmd/celeste/venice/media.go` (remove upscale dispatch case)
- Possibly `cmd/celeste/tui/app.go` if upscale parsing lives there

---

## Feature 3: `upscale_image` Skill/Tool Call

### Current State
`venice.UpscaleImage()` exists in `cmd/celeste/venice/media.go:200–297`. It takes a config, image path, and params map. It's fully functional — just not exposed as a skill.

### Target State
Register `upscale_image` as a built-in skill with Venice config injected at registration time. Always available in the tool list (like `generate_qr_code` or `generate_uuid`).

### Skill Definition
```go
SkillDefinition{
    Name:        "upscale_image",
    Description: "Upscale and enhance an image using Venice.ai. Returns the path to the upscaled file.",
    Parameters: map[string]any{
        "type": "object",
        "properties": map[string]any{
            "image_path": map[string]any{
                "type":        "string",
                "description": "Path to the image file to upscale",
            },
            "scale": map[string]any{
                "type":        "integer",
                "description": "Upscale factor (default 2)",
                "default":     2,
            },
            "creativity": map[string]any{
                "type":        "number",
                "description": "Enhancement creativity level 0.0–1.0 (default 0.5)",
                "default":     0.5,
            },
        },
        "required": []string{"image_path"},
    },
}
```

### Config Injection
At skill registration (in `skills/builtin.go` or where skills are initialized in `main.go`), pass the Venice config:
```go
RegisterUpscaleSkill(registry, venice.Config{
    BaseURL: cfg.VeniceBaseURL,
    APIKey:  cfg.VeniceAPIKey,
})
```

The skill executor calls `venice.UpscaleImage(veniceConfig, args["image_path"], params)` and returns the output path.

### Return Value
The skill returns a JSON result with:
- `output_path`: where the upscaled image was saved
- `message`: human-readable confirmation (e.g., "Image upscaled 2x and saved to ~/Pictures/upscaled_abc.png")

If Venice credentials are missing, return a clear error message rather than panicking.

### Files Changed
- `cmd/celeste/skills/builtin.go` — add `RegisterUpscaleSkill`
- `cmd/celeste/main.go` or skill init path — inject Venice config at registration
- `cmd/celeste/venice/media.go` — may need minor refactor to expose `UpscaleImage` cleanly

---

## Data Flow

```
User: "upscale this image at ~/foo.png to 4x"
  → LLM decides to call upscale_image tool
  → SkillCallMsg{name: "upscale_image", args: {image_path: "~/foo.png", scale: 4}}
  → Executor.Execute("upscale_image", args)
  → venice.UpscaleImage(config, "~/foo.png", {scale: 4, creativity: 0.5})
  → saves file, returns {output_path: "~/foo_upscaled.png", message: "..."}
  → LLM receives tool result, responds to user
```

---

## Testing
- `/clear` creates a new session ID distinct from the previous one
- Old session messages are not included in new session's history
- `upscale_image` skill appears in `/skills` listing
- Calling `upscale_image` with a valid path returns a saved file
- Calling `upscale_image` with missing Venice config returns a readable error
- NSFW mode still works for text and image generation; `upscale:` no longer works as a media command
