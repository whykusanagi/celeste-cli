package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected *Command
	}{
		{
			name:  "simple command",
			input: "/help",
			expected: &Command{
				Name: "help",
				Args: nil,
				Raw:  "/help",
			},
		},
		{
			name:  "command with args",
			input: "/endpoint venice",
			expected: &Command{
				Name: "endpoint",
				Args: []string{"venice"},
				Raw:  "/endpoint venice",
			},
		},
		{
			name:  "command with multiple args",
			input: "/model gpt-4.1-nano",
			expected: &Command{
				Name: "model",
				Args: []string{"gpt-4.1-nano"},
				Raw:  "/model gpt-4.1-nano",
			},
		},
		{
			name:     "not a command",
			input:    "hello world",
			expected: nil,
		},
		{
			name:     "empty string",
			input:    "",
			expected: nil,
		},
		{
			name:  "command with extra spaces",
			input: "  /nsfw  ",
			expected: &Command{
				Name: "nsfw",
				Args: nil,
				Raw:  "/nsfw",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Parse(tt.input)

			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				require.NotNil(t, result)
				assert.Equal(t, tt.expected.Name, result.Name)
				assert.Equal(t, tt.expected.Args, result.Args)
			}
		})
	}
}

func TestExecuteNSFW(t *testing.T) {
	cmd := &Command{Name: "nsfw"}
	ctx := &CommandContext{NSFWMode: false}
	result := Execute(cmd, ctx)

	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "NSFW Mode Enabled")
	assert.True(t, result.ShouldRender)
	require.NotNil(t, result.StateChange)
	require.NotNil(t, result.StateChange.NSFWMode)
	assert.True(t, *result.StateChange.NSFWMode)
	require.NotNil(t, result.StateChange.ImageModel)
	assert.Equal(t, "lustify-sdxl", *result.StateChange.ImageModel)
}

func TestExecuteSafe(t *testing.T) {
	cmd := &Command{Name: "safe"}
	ctx := &CommandContext{NSFWMode: true}
	result := Execute(cmd, ctx)

	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "Safe Mode Enabled")
	assert.True(t, result.ShouldRender)
	require.NotNil(t, result.StateChange)
	require.NotNil(t, result.StateChange.NSFWMode)
	assert.False(t, *result.StateChange.NSFWMode)
}

func TestExecuteEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		contains    string
	}{
		{
			name:        "valid endpoint - venice",
			args:        []string{"venice"},
			expectError: false,
			contains:    "Venice.ai",
		},
		{
			name:        "valid endpoint - openai",
			args:        []string{"openai"},
			expectError: false,
			contains:    "OpenAI",
		},
		{
			name:        "invalid endpoint",
			args:        []string{"invalid"},
			expectError: true,
			contains:    "Unknown endpoint",
		},
		{
			name:        "no args",
			args:        []string{},
			expectError: true,
			contains:    "Usage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &Command{Name: "endpoint", Args: tt.args}
			ctx := &CommandContext{}
			result := Execute(cmd, ctx)

			assert.Equal(t, !tt.expectError, result.Success)
			assert.Contains(t, result.Message, tt.contains)
		})
	}
}

func TestExecuteModel(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		expectError bool
		modelName   string
	}{
		{
			name:        "set model",
			args:        []string{"gpt-4.1"},
			expectError: false,
			modelName:   "gpt-4.1",
		},
		{
			name:        "model with hyphens",
			args:        []string{"gpt-4.1-nano"},
			expectError: false,
			modelName:   "gpt-4.1-nano",
		},
		{
			name:        "no args",
			args:        []string{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &Command{Name: "model", Args: tt.args}
			ctx := &CommandContext{}
			result := Execute(cmd, ctx)

			assert.Equal(t, !tt.expectError, result.Success)

			if !tt.expectError {
				require.NotNil(t, result.StateChange)
				require.NotNil(t, result.StateChange.Model)
				assert.Equal(t, tt.modelName, *result.StateChange.Model)
			}
		})
	}
}

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

func TestExecuteHelp(t *testing.T) {
	cmd := &Command{Name: "help"}
	ctx := &CommandContext{NSFWMode: false}
	result := Execute(cmd, ctx)

	assert.True(t, result.Success)
	assert.True(t, result.ShouldRender)
	assert.Contains(t, result.Message, "Available Commands")
	assert.Contains(t, result.Message, "/nsfw")
	assert.Contains(t, result.Message, "/safe")
}

func TestExecuteNSFWToggle(t *testing.T) {
	t.Run("enable when off", func(t *testing.T) {
		cmd := &Command{Name: "nsfw"}
		ctx := &CommandContext{NSFWMode: false}
		result := Execute(cmd, ctx)
		require.NotNil(t, result.StateChange)
		require.NotNil(t, result.StateChange.NSFWMode)
		assert.True(t, *result.StateChange.NSFWMode, "should enable NSFW when currently off")
	})

	t.Run("disable when on (toggle)", func(t *testing.T) {
		cmd := &Command{Name: "nsfw"}
		ctx := &CommandContext{NSFWMode: true}
		result := Execute(cmd, ctx)
		require.NotNil(t, result.StateChange)
		require.NotNil(t, result.StateChange.NSFWMode)
		assert.False(t, *result.StateChange.NSFWMode, "should disable NSFW when already on")
		assert.Contains(t, result.Message, "Safe Mode Enabled")
	})
}

func TestExecuteTools(t *testing.T) {
	cmd := &Command{Name: "tools"}
	ctx := &CommandContext{}
	result := Execute(cmd, ctx)
	assert.True(t, result.Success, "tools command should not return unknown command error")
	assert.NotContains(t, result.Message, "Unknown command")
}

func TestExecuteUnknownCommand(t *testing.T) {
	cmd := &Command{Name: "unknown"}
	ctx := &CommandContext{}
	result := Execute(cmd, ctx)

	assert.False(t, result.Success)
	assert.Contains(t, result.Message, "Unknown command")
	assert.Contains(t, result.Message, "/help")
}

func TestDetectRoutingHints(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected string
	}{
		{
			name:     "explicit nsfw hashtag",
			message:  "Generate an image #nsfw",
			expected: "venice",
		},
		{
			name:     "uncensored hashtag",
			message:  "Create something #uncensored",
			expected: "venice",
		},
		{
			name:     "nsfw as last word",
			message:  "Generate a character image nsfw",
			expected: "venice",
		},
		{
			name:     "explicit as last word",
			message:  "Make this explicit",
			expected: "venice",
		},
		{
			name:     "no hints",
			message:  "What's the weather today?",
			expected: "",
		},
		{
			name:     "nsfw in middle",
			message:  "I want nsfw content generated please",
			expected: "", // Not at end, not hashtag
		},
		{
			name:     "case insensitive",
			message:  "Generate image NSFW",
			expected: "venice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetectRoutingHints(tt.message)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsImageGenerationRequest(t *testing.T) {
	tests := []struct {
		name     string
		message  string
		expected bool
	}{
		{
			name:     "generate image",
			message:  "Generate an image of a cat",
			expected: true,
		},
		{
			name:     "create image",
			message:  "Create an image of a sunset",
			expected: true,
		},
		{
			name:     "draw",
			message:  "Draw a picture of mountains",
			expected: true,
		},
		{
			name:     "generate art",
			message:  "Generate art in cyberpunk style",
			expected: true,
		},
		{
			name:     "not image generation",
			message:  "What's the weather today?",
			expected: false,
		},
		{
			name:     "talking about images",
			message:  "I like images of cats",
			expected: false,
		},
		{
			name:     "case insensitive",
			message:  "GENERATE IMAGE of a dragon",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsImageGenerationRequest(tt.message)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsContentPolicyRefusal(t *testing.T) {
	tests := []struct {
		name     string
		response string
		expected bool
	}{
		{
			name:     "explicit refusal",
			response: "I can't generate explicit content as it violates my content policy.",
			expected: true,
		},
		{
			name:     "cannot create",
			response: "I cannot create inappropriate images.",
			expected: true,
		},
		{
			name:     "not comfortable",
			response: "I don't feel comfortable creating that kind of content.",
			expected: true,
		},
		{
			name:     "against policy",
			response: "This request is against my usage policy.",
			expected: true,
		},
		{
			name:     "normal response",
			response: "Here's the information you requested about weather patterns.",
			expected: false,
		},
		{
			name:     "case insensitive",
			response: "I CAN'T help with that request.",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsContentPolicyRefusal(tt.response)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExecuteProviders tests the providers command
func TestExecuteProviders(t *testing.T) {
	t.Run("list all providers", func(t *testing.T) {
		cmd := &Command{Name: "providers", Args: []string{}}
		ctx := &CommandContext{Provider: "openai", CurrentModel: "gpt-4.1-nano"}
		result := Execute(cmd, ctx)

		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "AI PROVIDERS")
		assert.Contains(t, result.Message, "openai")
		assert.Contains(t, result.Message, "grok")
		assert.Contains(t, result.Message, "Use: /providers info")
	})

	t.Run("list tool providers", func(t *testing.T) {
		cmd := &Command{Name: "providers", Args: []string{"--tools"}}
		ctx := &CommandContext{Provider: "openai"}
		result := Execute(cmd, ctx)

		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "TOOL-CAPABLE")
		assert.Contains(t, result.Message, "openai")
		assert.Contains(t, result.Message, "grok")
	})

	t.Run("provider info openai", func(t *testing.T) {
		cmd := &Command{Name: "providers", Args: []string{"info", "openai"}}
		ctx := &CommandContext{Provider: "grok"}
		result := Execute(cmd, ctx)

		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "PROVIDER: OPENAI")
		assert.Contains(t, result.Message, "CAPABILITIES")
		assert.Contains(t, result.Message, "Function Calling")
		assert.Contains(t, result.Message, "AUTHENTICATION")
		assert.Contains(t, result.Message, "TEST STATUS")
		assert.Contains(t, result.Message, "EXAMPLE USAGE")
	})

	t.Run("provider info unknown", func(t *testing.T) {
		cmd := &Command{Name: "providers", Args: []string{"info", "unknown"}}
		ctx := &CommandContext{}
		result := Execute(cmd, ctx)

		assert.False(t, result.Success)
		assert.Contains(t, result.Message, "not found")
		assert.Contains(t, result.Message, "Available providers")
	})

	t.Run("current provider", func(t *testing.T) {
		cmd := &Command{Name: "providers", Args: []string{"current"}}
		ctx := &CommandContext{Provider: "openai", CurrentModel: "gpt-4.1-nano", BaseURL: "https://api.openai.com/v1"}
		result := Execute(cmd, ctx)

		assert.True(t, result.Success)
		assert.Contains(t, result.Message, "openai")
		assert.Contains(t, result.Message, "gpt-4.1-nano")
	})
}

// TestHandleProvidersCommand tests provider command handling
func TestHandleProvidersCommand(t *testing.T) {
	tests := []struct {
		name           string
		args           []string
		expectSuccess  bool
		expectContains string
	}{
		{
			name:           "list providers",
			args:           []string{},
			expectSuccess:  true,
			expectContains: "openai",
		},
		{
			name:           "tool providers",
			args:           []string{"--tools"},
			expectSuccess:  true,
			expectContains: "TOOL-CAPABLE",
		},
		{
			name:           "provider info",
			args:           []string{"info", "grok"},
			expectSuccess:  true,
			expectContains: "GROK",
		},
		{
			name:           "current provider",
			args:           []string{"current"},
			expectSuccess:  true,
			expectContains: "PROVIDER:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &Command{Name: "providers", Args: tt.args}
			ctx := &CommandContext{Provider: "openai"}
			result := Execute(cmd, ctx)

			assert.Equal(t, tt.expectSuccess, result.Success)
			if tt.expectContains != "" {
				assert.Contains(t, result.Message, tt.expectContains)
			}
		})
	}
}

// TestBoolToStatus tests the helper function
func TestBoolToStatus(t *testing.T) {
	tests := []struct {
		input    bool
		expected string
	}{
		{true, "✓ Yes"},
		{false, "✗ No"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := boolToStatus(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// A pasted absolute path begins with "/" and was therefore parsed as a slash
// command: "/Users/me/a.mp3" became command name "Users/me/a.mp3" and fell to
// the unknown-command branch, so Celeste errored instead of reading the message.
// Two paths were worse — the first became the command, the rest its arguments,
// which is exactly what you type to hand her several files at once.
//
// A real command name never contains a path separator, so that is the
// disambiguator. Typos like /halp still reach the unknown-command branch, which
// is deliberate: silently turning a mistyped command into a prompt hides it.
func TestParseDoesNotTreatPathsAsCommands(t *testing.T) {
	paths := []string{
		"/Users/kusanagi/Downloads/agentic_code",
		"/tmp/foo.mp3",
		"/Users/kusanagi/a.mp3 /Users/kusanagi/b.mp3",
		"/Users/kusanagi/Music/track one.mp3",
		"~/Downloads/x.mp3",
		"./relative/path.mp3",
	}
	for _, p := range paths {
		if cmd := Parse(p); cmd != nil {
			t.Errorf("Parse(%q) = command %q, want nil so it is sent as a message", p, cmd.Name)
		}
	}
}

func TestParseStillRecognisesRealCommands(t *testing.T) {
	cases := []struct {
		in       string
		wantName string
		wantArgs []string
	}{
		{"/help", "help", nil},
		{"/model", "model", nil},
		{"/set-model fugu-ultra", "set-model", []string{"fugu-ultra"}},
		{"/config", "config", nil},
		{"/halp", "halp", nil}, // typo still reaches the unknown-command branch
	}
	for _, c := range cases {
		cmd := Parse(c.in)
		if cmd == nil {
			t.Errorf("Parse(%q) = nil, want a command", c.in)
			continue
		}
		if cmd.Name != c.wantName {
			t.Errorf("Parse(%q).Name = %q, want %q", c.in, cmd.Name, c.wantName)
		}
		if len(c.wantArgs) != len(cmd.Args) {
			t.Errorf("Parse(%q).Args = %v, want %v", c.in, cmd.Args, c.wantArgs)
		}
	}
}
