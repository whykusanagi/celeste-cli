# Testing Guide

Comprehensive guide to testing Celeste CLI.

## Table of Contents

- [Running Tests](#running-tests)
- [Test Coverage](#test-coverage)
- [Writing Tests](#writing-tests)
- [Package-Specific Testing](#package-specific-testing)
- [Integration Testing](#integration-testing)
- [Continuous Integration](#continuous-integration)

---

## Running Tests

### All Tests

Run all unit tests across the project:

```bash
go test ./...
```

### Specific Package

Test a single package:

```bash
go test ./cmd/celeste/providers/
go test ./cmd/celeste/commands/
go test ./cmd/celeste/skills/
```

### With Coverage

Generate coverage report:

```bash
# Terminal output
go test -cover ./cmd/celeste/...

# Coverage profile
go test -coverprofile=coverage.out ./cmd/celeste/...

# HTML coverage report (opens in browser)
go tool cover -html=coverage.out
```

### Verbose Output

See detailed test execution:

```bash
go test -v ./cmd/celeste/providers/
```

### Run Specific Test

```bash
go test -run TestProviderRegistry ./cmd/celeste/providers/
go test -run TestExecuteProviders ./cmd/celeste/commands/
```

---

## Test Coverage

### Current Coverage (v1.2.0)

**Overall**: 17.4%

**By Package**:
- ✅ **prompts**: 97.1% (excellent - comprehensive persona testing)
- ✅ **providers**: 72.8% (excellent - registry and model detection)
- ✅ **config**: 52.0% (good - session and configuration management)
- ⚠️ **commands**: 25.8% (moderate - command parsing and execution)
- ⚠️ **venice**: 22.6% (moderate - media parsing and file handling)
- ⚠️ **skills**: 12.2% (low - registry only, handlers need mocking)
- ❌ **llm**: 0% (requires HTTP client mocking)
- ❌ **tui**: 0% (requires Bubble Tea/tcell mocking)

### Coverage Goals

- **Critical packages** (providers, config, prompts): >70% ✅
- **Feature packages** (commands, skills, venice): >20% ✅
- **Infrastructure packages** (llm, tui): Requires mocking infrastructure 🔜

### Checking Coverage

View coverage by function:

```bash
go test -coverprofile=coverage.out ./cmd/celeste/...
go tool cover -func=coverage.out | grep -E "(registry|models|prompts)"
go tool cover -func=coverage.out | grep total
```

---

## Writing Tests

### Test File Structure

Test files follow Go conventions:

```
cmd/celeste/providers/
  ├── registry.go
  ├── registry_test.go      # Tests for registry.go
  ├── models.go
  └── models_test.go         # Tests for models.go
```

### Basic Test Template

```go
package providers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFeatureName(t *testing.T) {
	// Setup
	registry := NewRegistry()

	// Execute
	result := registry.SomeMethod()

	// Assert
	assert.NotNil(t, result)
	assert.Equal(t, expected, result)
}
```

### Table-Driven Tests

For testing multiple cases:

```go
func TestParseMediaCommand(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		expectType     string
		expectPrompt   string
		expectIsMedia  bool
	}{
		{
			name:          "Anime shortcut",
			input:         "anime: cute girl",
			expectType:    "image",
			expectPrompt:  "cute girl",
			expectIsMedia: true,
		},
		{
			name:          "Not a media command",
			input:         "Tell me a joke",
			expectIsMedia: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mediaType, prompt, _, isMedia := ParseMediaCommand(tt.input)

			assert.Equal(t, tt.expectIsMedia, isMedia)
			if tt.expectIsMedia {
				assert.Equal(t, tt.expectType, mediaType)
				assert.Equal(t, tt.expectPrompt, prompt)
			}
		})
	}
}
```

### Subtests

Organize related tests:

```go
func TestProviderDetection(t *testing.T) {
	t.Run("OpenAI URL detection", func(t *testing.T) {
		provider := DetectProvider("https://api.openai.com/v1")
		assert.Equal(t, "openai", provider)
	})

	t.Run("Grok URL detection", func(t *testing.T) {
		provider := DetectProvider("https://api.x.ai/v1")
		assert.Equal(t, "grok", provider)
	})
}
```

### Using Testify Assertions

**assert** vs **require**:

```go
// assert: Test continues after failure
assert.NotNil(t, obj, "object should not be nil")
assert.Equal(t, expected, actual, "values should match")

// require: Test stops immediately after failure
require.NoError(t, err, "operation should not error")
require.NotNil(t, obj, "object is required for next assertions")
```

### Testing with Temporary Files

```go
func TestConfigFile(t *testing.T) {
	// Create temp directory (automatically cleaned up)
	tmpDir := t.TempDir()

	// Create test file
	configPath := filepath.Join(tmpDir, "config.json")
	err := os.WriteFile(configPath, []byte(`{"key": "value"}`), 0644)
	require.NoError(t, err)

	// Test with file
	config, err := LoadConfig(configPath)
	require.NoError(t, err)
	assert.Equal(t, "value", config.Key)
}
```

### Mocking Environment Variables

```go
func TestHomeDirectory(t *testing.T) {
	// Save original
	originalHome := os.Getenv("HOME")
	defer os.Setenv("HOME", originalHome)

	// Set test value
	testHome := t.TempDir()
	os.Setenv("HOME", testHome)

	// Test with mocked HOME
	dir := getDownloadsDir()
	assert.Equal(t, filepath.Join(testHome, "Downloads"), dir)
}
```

---

## Package-Specific Testing

### Providers Package

Tests provider registry and model detection:

```bash
go test -v ./cmd/celeste/providers/ -cover
```

**What's tested**:
- Provider registration (all 9 providers)
- URL pattern detection
- Model lists and recommendations
- Tool-calling capability filtering
- OpenAI compatibility detection

**Files**:
- `registry_test.go` (13 test functions)
- `models_test.go` (14 test functions)

### Commands Package

Tests command parsing and execution:

```bash
go test -v ./cmd/celeste/commands/ -cover
```

**What's tested**:
- Command parsing (`/help`, `/providers`, `/nsfw`, etc.)
- Provider command handling
- NSFW/Safe mode toggling
- Endpoint switching
- Error handling for invalid commands

**Files**:
- `commands_test.go` (17 test functions)

### Skills Package

Tests skill registry and definitions:

```bash
go test -v ./cmd/celeste/skills/ -cover
```

**What's tested**:
- Skill registration
- Handler registration
- Skill retrieval and execution
- Tool definition generation
- Built-in skill registration (18 skills)

**What's NOT tested** (requires mocking):
- Skill handlers (weather, currency, QR codes, etc.)
- External API calls
- File system operations

**Files**:
- `registry_test.go` (18 test functions)

### Prompts Package

Tests persona and system prompt generation:

```bash
go test -v ./cmd/celeste/prompts/ -cover
```

**What's tested**:
- Loading persona essence (embedded and file-based)
- System prompt generation
- NSFW mode prompts
- Content generation prompts (Twitter, TikTok, YouTube, Discord)
- Prompt consistency and structure

**Files**:
- `celeste_test.go` (16 test functions)

### Venice Package

Tests media command parsing:

```bash
go test -v ./cmd/celeste/venice/ -cover
```

**What's tested**:
- Media command parsing (`anime:`, `dream:`, `image:`, `upscale:`)
- Custom model syntax (`image[model]: prompt`)
- Base64 image encoding/decoding
- Downloads directory resolution
- File saving logic

**Files**:
- `media_test.go` (9 test functions)

---

## Integration Testing

### Provider Integration Tests

Located in `cmd/celeste/providers/integration_test.go`.

**Requires**:
- Real API keys (set via environment variables)
- Build tag: `-tags=integration`

**Run**:

```bash
export OPENAI_API_KEY="sk-..."
export GROK_API_KEY="xai-..."
go test -tags=integration -v ./cmd/celeste/providers/
```

**What's tested**:
- Actual API calls to providers
- Function calling with real models
- Streaming responses
- Model listing
- Error handling

**Status**: Framework ready, needs API keys for full validation

### One-Shot Command Tests

Located in `test/test_oneshot_commands.sh`.

Tests CLI commands without TUI:

```bash
./test/test_oneshot_commands.sh
```

**Tests** (21 total):
- `./celeste version`
- `./celeste providers`
- `./celeste providers --tools`
- `./celeste providers info openai`
- `./celeste skills list`
- Configuration commands

---

## Continuous Integration

### GitHub Actions

Tests run automatically on:
- Push to main branch
- Pull requests
- Manual workflow dispatch

**Workflow file**: `.github/workflows/test.yml` (if configured)

### Running CI Tests Locally

Simulate CI environment:

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./cmd/celeste/...

# Run one-shot tests
./test/test_oneshot_commands.sh

# Check formatting
go fmt ./...
go vet ./...
```

---

## Best Practices

### DO ✅

- **Test public APIs**: Focus on exported functions and types
- **Use table-driven tests**: For testing multiple scenarios
- **Test error cases**: Verify error handling
- **Use t.TempDir()**: For file-based tests (auto-cleanup)
- **Use descriptive names**: `TestProviderRegistration`, not `TestFunc1`
- **Test edge cases**: Empty strings, nil values, boundary conditions
- **Use require for setup**: Stop test if setup fails
- **Use assert for checks**: Continue test to see all failures

### DON'T ❌

- **Don't test private functions**: Test behavior through public API
- **Don't use real API keys in unit tests**: Use mocks or skip tests
- **Don't hard-code file paths**: Use t.TempDir() or relative paths
- **Don't test external dependencies**: Mock HTTP clients, APIs
- **Don't write flaky tests**: Tests should be deterministic
- **Don't ignore test failures**: Fix or skip with clear reason

### Example: Good vs Bad

**Bad** ❌:

```go
func TestWeather(t *testing.T) {
	// Makes real API call - flaky, slow, requires API key
	result, _ := GetWeather("10001")
	assert.Contains(t, result, "weather")
}
```

**Good** ✅:

```go
func TestWeatherHandlerRegistration(t *testing.T) {
	// Tests that skill is registered correctly
	registry := NewRegistry()
	mockConfig := NewMockConfigLoader()
	RegisterBuiltinSkills(registry, mockConfig)

	skill, exists := registry.GetSkill("get_weather")
	assert.True(t, exists)
	assert.Equal(t, "get_weather", skill.Name)
	assert.NotEmpty(t, skill.Description)
}
```

---

## Troubleshooting

### Test Failures

**"no such file or directory"**
- Use absolute paths or `t.TempDir()`
- Check working directory: `os.Getwd()`

**"undefined: X"**
- Missing import
- Typo in function/variable name
- Not exported (lowercase)

**"cannot use X as type Y"**
- Type mismatch in assertion
- Use type assertion: `result.(map[string]interface{})`

### Coverage Not Updating

```bash
# Clear test cache
go clean -testcache

# Re-run tests
go test -cover ./cmd/celeste/...
```

### Tests Hang

- Check for infinite loops
- Add timeout: `go test -timeout 30s`
- Check for blocking channel operations

---

## Further Reading

- [Go Testing Package](https://pkg.go.dev/testing)
- [Testify Documentation](https://github.com/stretchr/testify)
- [Table-Driven Tests](https://go.dev/wiki/TableDrivenTests)
- [Integration Test Guide](./cmd/celeste/providers/INTEGRATION_TESTS.md)
- [Provider Audit Matrix](./PROVIDER_AUDIT_MATRIX.md)

---

**Last Updated**: December 14, 2024
**Version**: v1.2.0
**Test Coverage**: 17.4%
