# /clear Reset, NSFW Cleanup, Upscale Tool Call — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Extend `/clear` to start a fresh session, remove `upscale:` from NSFW media commands, and add `upscale_image` as an always-available tool call skill.

**Architecture:** Three independent changes touching commands, TUI, venice media parsing, and the skills registry. The upscale handler in `skills/builtin.go` will import `venice` package directly since venice has no dependency on skills (no circular import). The `upscale:` media command is removed from `venice/media.go`'s `ParseMediaCommand` function; the TUI switch case is removed in parallel.

**Tech Stack:** Go 1.24, Bubble Tea TUI, `github.com/stretchr/testify`

---

## Task 1: Extend StateChange + update /clear handler

**Files:**
- Modify: `cmd/celeste/commands/commands.go:57-67` (StateChange struct)
- Modify: `cmd/celeste/commands/commands.go:617-627` (handleClear)
- Modify: `cmd/celeste/commands/commands_test.go:195-204` (TestExecuteClear)

---

**Step 1: Update the failing test first**

In `cmd/celeste/commands/commands_test.go`, find `TestExecuteClear` at line 195 and add the new assertion:

```go
func TestExecuteClear(t *testing.T) {
	cmd := &Command{Name: "clear"}
	ctx := &CommandContext{}
	result := Execute(cmd, ctx)

	assert.True(t, result.Success)
	assert.False(t, result.ShouldRender)
	require.NotNil(t, result.StateChange)
	assert.True(t, result.StateChange.ClearHistory)
	assert.True(t, result.StateChange.NewSession) // NEW assertion
}
```

**Step 2: Run test to confirm it fails**

```bash
go test ./cmd/celeste/commands/... -run TestExecuteClear -v
```

Expected: `FAIL — result.StateChange.NewSession` is undefined (compile error or false)

**Step 3: Add `NewSession bool` to StateChange struct**

In `cmd/celeste/commands/commands.go`, find the `StateChange` struct at line 58 and add the field:

```go
type StateChange struct {
	EndpointChange *string
	NSFWMode       *bool
	Model          *string
	ImageModel     *string
	ClearHistory   bool
	NewSession     bool           // NEW: also start a fresh session
	MenuState      *string        // "status", "commands", "skills"
	SessionAction  *SessionAction // Session management operations
	ShowSelector   *SelectorData  // Show interactive selector
}
```

**Step 4: Update handleClear to set NewSession**

In `cmd/celeste/commands/commands.go`, replace `handleClear` (around line 617):

```go
// handleClear handles the /clear command.
func handleClear(cmd *Command) *CommandResult {
	return &CommandResult{
		Success:      true,
		Message:      "Session cleared, new session started.",
		ShouldRender: false,
		StateChange: &StateChange{
			ClearHistory: true,
			NewSession:   true,
		},
	}
}
```

**Step 5: Run test to confirm it passes**

```bash
go test ./cmd/celeste/commands/... -run TestExecuteClear -v
```

Expected: `PASS`

**Step 6: Run all command tests**

```bash
go test ./cmd/celeste/commands/... -v
```

Expected: all PASS

**Step 7: Commit**

```bash
git add cmd/celeste/commands/commands.go cmd/celeste/commands/commands_test.go
git commit -m "feat: /clear now sets NewSession flag for full session reset"
```

---

## Task 2: Handle NewSession in TUI

**Files:**
- Modify: `cmd/celeste/tui/app.go:530-532` (ClearHistory handler block)

Note: The TUI has no unit tests for this path; we'll verify it compiles and rely on manual smoke testing.

---

**Step 1: Add NewSession handling after ClearHistory block**

In `cmd/celeste/tui/app.go`, find the `ClearHistory` block at line 530:

```go
if result.StateChange.ClearHistory {
    m.chat = m.chat.Clear()
}
```

Replace it with:

```go
if result.StateChange.ClearHistory {
    m.chat = m.chat.Clear()
}
if result.StateChange.NewSession {
    m = m.handleSessionAction(&commands.SessionAction{Action: "new"})
}
```

This delegates to the same session-creation logic used by `/session new`, which creates a new `Session`, clears the chat, and shows the "📝 New session created" message.

**Step 2: Verify it compiles**

```bash
go build ./cmd/celeste/...
```

Expected: builds with no errors

**Step 3: Commit**

```bash
git add cmd/celeste/tui/app.go
git commit -m "feat: /clear starts a fresh session after clearing chat"
```

---

## Task 3: Remove `upscale:` from venice media parsing

**Files:**
- Modify: `cmd/celeste/venice/media.go:558-585` (prefixes map + upscale block)
- Modify: `cmd/celeste/venice/media_test.go:58-72` (remove upscale test cases)

---

**Step 1: Remove the upscale test cases first (they'll fail once we remove the code)**

In `cmd/celeste/venice/media_test.go`, remove the two upscale test cases from the `tests` slice (lines 58–72):

```go
// DELETE these two entries from the tests slice:
{
    name:          "Upscale prefix with path",
    input:         "upscale: /path/to/image.png some params",
    expectType:    "upscale",
    expectPrompt:  "some params",
    expectParams:  map[string]interface{}{"path": "/path/to/image.png"},
    expectIsMedia: true,
},
{
    name:          "Upscale with just path",
    input:         "upscale: /path/to/image.png",
    expectType:    "upscale",
    expectPrompt:  "",
    expectParams:  map[string]interface{}{"path": "/path/to/image.png"},
    expectIsMedia: true,
},
```

After deletion, there should be no upscale entries in the table.

**Step 2: Add a regression test confirming upscale: is no longer a media command**

Add this test case to the `tests` slice in `TestParseMediaCommand`:

```go
{
    name:          "upscale prefix no longer a media command",
    input:         "upscale: /path/to/image.png",
    expectType:    "",
    expectPrompt:  "",
    expectParams:  nil,
    expectIsMedia: false,
},
```

**Step 3: Run the test to confirm it fails (upscale still parses)**

```bash
go test ./cmd/celeste/venice/... -run TestParseMediaCommand -v
```

Expected: FAIL — the new regression test fails because `upscale:` still parses as a media command

**Step 4: Remove `upscale:` from the prefixes map in ParseMediaCommand**

In `cmd/celeste/venice/media.go`, find the `prefixes` map around line 559:

```go
prefixes := map[string]string{
    "image:":   "image",
    "upscale:": "upscale",
}
```

Change it to:

```go
prefixes := map[string]string{
    "image:": "image",
}
```

Then remove the upscale-specific block immediately below (lines 570–581):

```go
// DELETE this entire block:
// For upscale, first word is file path
if mediaType == "upscale" {
    parts := strings.SplitN(content, " ", 2)
    if len(parts) > 0 {
        params["path"] = parts[0]
        if len(parts) > 1 {
            content = parts[1] // Rest is additional params
        } else {
            content = ""
        }
    }
}
```

**Step 5: Run tests to confirm they pass**

```bash
go test ./cmd/celeste/venice/... -run TestParseMediaCommand -v
```

Expected: PASS

**Step 6: Run all venice tests**

```bash
go test ./cmd/celeste/venice/... -v
```

Expected: all PASS

**Step 7: Commit**

```bash
git add cmd/celeste/venice/media.go cmd/celeste/venice/media_test.go
git commit -m "feat: remove upscale: media command from NSFW parsing"
```

---

## Task 4: Remove upscale from TUI media switch + help text

**Files:**
- Modify: `cmd/celeste/tui/app.go:763-768` (case "upscale" in switch)
- Modify: `cmd/celeste/commands/commands.go:660-661` (help text)

---

**Step 1: Remove the `case "upscale":` block from the TUI media switch**

In `cmd/celeste/tui/app.go`, find the switch at line 758 and delete the upscale case:

```go
// DELETE this case:
case "upscale":
    if path, ok := msg.Params["path"].(string); ok {
        response, genErr = venice.UpscaleImage(config, path, msg.Params)
    } else {
        genErr = fmt.Errorf("no image path provided for upscale")
    }
```

The switch should now only have `case "image":`, `case "video":`, `case "image-to-video":`, and `default:`.

**Step 2: Remove the `upscale:` line from the NSFW help text**

In `cmd/celeste/commands/commands.go`, find around line 660:

```
  upscale: <path>              Upscale and enhance existing image
                               Example: upscale: ~/photo.jpg
```

Delete those two lines from the help string.

**Step 3: Verify it compiles**

```bash
go build ./cmd/celeste/...
```

Expected: builds with no errors

**Step 4: Commit**

```bash
git add cmd/celeste/tui/app.go cmd/celeste/commands/commands.go
git commit -m "feat: remove upscale case from TUI media handler and help text"
```

---

## Task 5: Add `upscale_image` skill

**Files:**
- Modify: `cmd/celeste/skills/builtin.go` (add skill definition, handler, registration)
- Create: `cmd/celeste/skills/builtin_upscale_test.go` (tests)

---

**Step 1: Write the failing tests**

Create `cmd/celeste/skills/builtin_upscale_test.go`:

```go
package skills

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpscaleImageSkill(t *testing.T) {
	skill := UpscaleImageSkill()

	assert.Equal(t, "upscale_image", skill.Name)
	assert.NotEmpty(t, skill.Description)

	props, ok := skill.Parameters["properties"].(map[string]interface{})
	require.True(t, ok, "parameters should have properties")
	assert.Contains(t, props, "image_path")
	assert.Contains(t, props, "scale")
	assert.Contains(t, props, "creativity")

	required, ok := skill.Parameters["required"].([]string)
	require.True(t, ok, "parameters should have required list")
	assert.Contains(t, required, "image_path")
}

func TestUpscaleImageHandler_MissingVeniceConfig(t *testing.T) {
	loader := NewMockConfigLoaderWithErrors()
	args := map[string]interface{}{"image_path": "/tmp/test.png"}

	_, err := UpscaleImageHandler(args, loader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "Venice.ai not configured")
}

func TestUpscaleImageHandler_MissingImagePath(t *testing.T) {
	loader := NewMockConfigLoader()
	args := map[string]interface{}{}

	_, err := UpscaleImageHandler(args, loader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "image_path is required")
}

func TestUpscaleImageHandler_EmptyImagePath(t *testing.T) {
	loader := NewMockConfigLoader()
	args := map[string]interface{}{"image_path": ""}

	_, err := UpscaleImageHandler(args, loader)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "image_path is required")
}

func TestUpscaleImageRegistered(t *testing.T) {
	registry := NewRegistry()
	loader := NewMockConfigLoader()

	RegisterBuiltinSkills(registry, loader)

	skill, exists := registry.GetSkill("upscale_image")
	assert.True(t, exists, "upscale_image should be registered")
	assert.Equal(t, "upscale_image", skill.Name)
}
```

**Step 2: Run tests to confirm they fail**

```bash
go test ./cmd/celeste/skills/... -run "TestUpscaleImage|TestUpscaleImageRegistered" -v
```

Expected: FAIL — `UpscaleImageSkill`, `UpscaleImageHandler` undefined

**Step 3: Add the skill definition and handler to `cmd/celeste/skills/builtin.go`**

At the top of `builtin.go`, add the venice import to the existing import block:

```go
import (
    // ... existing imports ...
    "github.com/whykusanagi/celeste-cli/cmd/celeste/venice"
)
```

Then add these functions at the end of `builtin.go`:

```go
// UpscaleImageSkill returns the skill definition for image upscaling.
func UpscaleImageSkill() Skill {
	return Skill{
		Name:        "upscale_image",
		Description: "Upscale and enhance an image using Venice.ai. Provide the file path to the image. Returns the path to the upscaled file.",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"image_path": map[string]interface{}{
					"type":        "string",
					"description": "Path to the image file to upscale (e.g. ~/Pictures/photo.png)",
				},
				"scale": map[string]interface{}{
					"type":        "integer",
					"description": "Upscale factor, e.g. 2 for 2x resolution (default: 2)",
					"default":     2,
				},
				"creativity": map[string]interface{}{
					"type":        "number",
					"description": "Enhancement creativity level from 0.0 to 1.0 (default: 0.5)",
					"default":     0.5,
				},
			},
			"required": []string{"image_path"},
		},
	}
}

// UpscaleImageHandler handles the upscale_image skill execution.
func UpscaleImageHandler(args map[string]interface{}, configLoader ConfigLoader) (interface{}, error) {
	veniceConfig, err := configLoader.GetVeniceConfig()
	if err != nil {
		return nil, fmt.Errorf("Venice.ai not configured: %w", err)
	}

	imagePath, ok := args["image_path"].(string)
	if !ok || imagePath == "" {
		return nil, fmt.Errorf("image_path is required")
	}

	scale := 2
	if s, ok := args["scale"].(float64); ok {
		scale = int(s)
	}

	creativity := 0.5
	if c, ok := args["creativity"].(float64); ok {
		creativity = c
	}

	config := venice.Config{
		APIKey:  veniceConfig.APIKey,
		BaseURL: veniceConfig.BaseURL,
	}

	params := map[string]interface{}{
		"scale":      scale,
		"creativity": creativity,
	}

	response, err := venice.UpscaleImage(config, imagePath, params)
	if err != nil {
		return nil, fmt.Errorf("upscale failed: %w", err)
	}

	if !response.Success {
		return nil, fmt.Errorf("upscale failed: %s", response.Error)
	}

	result := map[string]interface{}{
		"message": fmt.Sprintf("Image upscaled %dx successfully", scale),
	}
	if response.Path != "" {
		result["output_path"] = response.Path
		result["message"] = fmt.Sprintf("Image upscaled %dx and saved to %s", scale, response.Path)
	} else if response.URL != "" {
		result["url"] = response.URL
		result["message"] = fmt.Sprintf("Image upscaled %dx, available at: %s", scale, response.URL)
	}

	return result, nil
}
```

**Step 4: Register the skill and handler in `RegisterBuiltinSkills`**

In `cmd/celeste/skills/builtin.go`, inside `RegisterBuiltinSkills`, add to the skill registrations block:

```go
registry.RegisterSkill(UpscaleImageSkill())
```

And add to the handler registrations block:

```go
registry.RegisterHandler("upscale_image", func(args map[string]interface{}) (interface{}, error) {
    return UpscaleImageHandler(args, configLoader)
})
```

**Step 5: Run the tests**

```bash
go test ./cmd/celeste/skills/... -run "TestUpscaleImage|TestUpscaleImageRegistered" -v
```

Expected: PASS

**Step 6: Run all skills tests**

```bash
go test ./cmd/celeste/skills/... -v
```

Expected: all PASS

**Step 7: Build to confirm no compile errors**

```bash
go build ./cmd/celeste/...
```

Expected: builds with no errors

**Step 8: Commit**

```bash
git add cmd/celeste/skills/builtin.go cmd/celeste/skills/builtin_upscale_test.go
git commit -m "feat: add upscale_image as always-available tool call skill"
```

---

## Task 6: Final verification

**Step 1: Run the full test suite**

```bash
go test ./cmd/celeste/... -v 2>&1 | tail -30
```

Expected: all packages PASS, no FAIL lines

**Step 2: Build release binary**

```bash
go build -o /tmp/celeste-test ./cmd/celeste/
```

Expected: binary produced with no errors

**Step 3: Smoke test /clear behaviour**

Run the binary and verify:
- `/clear` shows "Session cleared, new session started." and a new session ID in subsequent `/session info`
- Previous messages are gone from context

**Step 4: Smoke test upscale_image skill**

Run the binary and type `/skills` — verify `upscale_image` appears in the list.

**Step 5: Verify `upscale:` no longer works in NSFW mode**

Enable NSFW mode with `/nsfw`, type `upscale: ~/some.png` — it should be sent to the LLM as plain text, not trigger media generation.
