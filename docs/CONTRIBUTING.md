# Contributing to Celeste CLI

Thank you for your interest in contributing to Celeste CLI! This guide will help you get started.

## Table of Contents

- [Getting Started](#getting-started)
- [Development Setup](#development-setup)
- [How to Contribute](#how-to-contribute)
- [Adding New Features](#adding-new-features)
- [Code Standards](#code-standards)
- [Testing Requirements](#testing-requirements)
- [Documentation](#documentation)
- [Pull Request Process](#pull-request-process)

---

## Getting Started

### Prerequisites

- Go 1.21 or higher
- Git
- Basic understanding of Go and terminal applications

### First Contribution

Good first issues:
- Add tests for existing untested functions
- Improve documentation
- Fix typos or formatting
- Add new skills (see [Adding Skills](#adding-skills))
- Test and document providers

---

## Development Setup

### 1. Fork and Clone

```bash
# Fork the repository on GitHub, then clone your fork
git clone https://github.com/YOUR_USERNAME/celeste-cli.git
cd celeste-cli

# Add upstream remote
git remote add upstream https://github.com/whykusanagi/celeste-cli.git
```

### 2. Install Dependencies

```bash
# Download Go modules
go mod download

# Build the project
go build -o celeste ./cmd/celeste/
```

### 3. Run Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./cmd/celeste/...

# Run specific package
go test -v ./cmd/celeste/providers/
```

### 4. Run the Application

```bash
# Interactive chat
./celeste chat

# One-shot commands
./celeste version
./celeste providers
./celeste skills list
```

---

## How to Contribute

### Reporting Issues

When reporting a bug, include:
- **Description**: Clear description of the issue
- **Steps to Reproduce**: Exact steps that trigger the bug
- **Expected Behavior**: What should happen
- **Actual Behavior**: What actually happens
- **Environment**: OS, Go version, Celeste version
- **Logs**: Error messages or stack traces

### Suggesting Enhancements

When suggesting a feature:
- **Use Case**: Why is this feature needed?
- **Proposed Solution**: How should it work?
- **Alternatives**: Other approaches considered
- **Examples**: Similar features in other tools

---

## Adding New Features

### Adding Skills

Skills are AI-callable functions that extend Celeste's capabilities.

**1. Define the Skill** (`cmd/celeste/skills/builtin.go`):

```go
func MyNewSkill() Skill {
	return Skill{
		Name:        "my_new_skill",
		Description: "Brief description of what this skill does",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"param1": map[string]interface{}{
					"type":        "string",
					"description": "Description of parameter",
				},
			},
			"required": []string{"param1"},
		},
	}
}
```

**2. Implement the Handler**:

```go
func MyNewSkillHandler(args map[string]interface{}) (interface{}, error) {
	// Validate inputs
	param1, ok := args["param1"].(string)
	if !ok {
		return nil, fmt.Errorf("param1 must be a string")
	}

	// Do the work
	result := doSomethingWith(param1)

	// Return structured result
	return map[string]interface{}{
		"success": true,
		"data":    result,
	}, nil
}
```

**3. Register the Skill** (in `RegisterBuiltinSkills()`):

```go
registry.RegisterSkill(MyNewSkill())
registry.RegisterHandler("my_new_skill", MyNewSkillHandler)
```

**4. Test the Skill** (`cmd/celeste/skills/builtin_test.go`):

```go
func TestMyNewSkill(t *testing.T) {
	skill := MyNewSkill()
	assert.Equal(t, "my_new_skill", skill.Name)
	assert.NotEmpty(t, skill.Description)
	assert.NotNil(t, skill.Parameters)
}
```

**Skill Guidelines**:
- Keep skills focused (single responsibility)
- Validate all inputs
- Return structured JSON results
- Handle errors gracefully
- Add clear descriptions for LLM understanding

---

### Adding Providers

Providers enable Celeste to work with different LLM services.

**1. Add to Provider Registry** (`cmd/celeste/providers/registry.go`):

```go
"newprovider": {
	Name:                    "newprovider",
	BaseURL:                 "https://api.newprovider.com/v1",
	DefaultModel:            "model-name",
	PreferredToolModel:      "model-name-with-tools",
	SupportsFunctionCalling: true,
	SupportsModelListing:    false,
	SupportsTokenTracking:   true,
	IsOpenAICompatible:      true,
	RequiresAPIKey:          true,
},
```

**2. Add URL Detection** (in `DetectProvider()`):

```go
if strings.Contains(baseURL, "newprovider.com") {
	return "newprovider"
}
```

**3. Add Static Model List** (`cmd/celeste/providers/models.go`):

```go
"newprovider": {
	"model-1",
	"model-2",
	"model-with-tools",
},
```

**4. Test the Provider**:

```go
func TestNewProvider(t *testing.T) {
	// Test detection
	provider := DetectProvider("https://api.newprovider.com/v1")
	assert.Equal(t, "newprovider", provider)

	// Test capabilities
	caps, ok := GetProvider("newprovider")
	require.True(t, ok)
	assert.True(t, caps.SupportsFunctionCalling)
}
```

**5. Document in LLM_PROVIDERS.md**:
- Add to Quick Reference table
- Describe setup instructions
- Note any limitations

**Provider Guidelines**:
- Test with real API if possible
- Document authentication requirements
- Note OpenAI compatibility level
- Test function calling support

---

### Adding Commands

Commands are slash commands used in the TUI (e.g., `/help`, `/providers`).

**1. Add Command Handler** (`cmd/celeste/commands/commands.go` or new file):

```go
func handleMyCommand(cmd *Command, ctx *CommandContext) *CommandResult {
	// Parse arguments
	if len(cmd.Args) < 1 {
		return &CommandResult{
			Success:      false,
			Message:      "Usage: /mycommand <arg>",
			ShouldRender: true,
		}
	}

	arg := cmd.Args[0]

	// Do the work
	result := processCommand(arg, ctx)

	return &CommandResult{
		Success:      true,
		Message:      result,
		ShouldRender: true,
	}
}
```

**2. Register Command** (in `Execute()` switch statement):

```go
case "mycommand":
	return handleMyCommand(cmd, ctx)
```

**3. Add to Help Text**:

Update the help command to include your new command.

**4. Test the Command**:

```go
func TestExecuteMyCommand(t *testing.T) {
	cmd := &Command{Name: "mycommand", Args: []string{"test"}}
	ctx := &CommandContext{}
	result := Execute(cmd, ctx)

	assert.True(t, result.Success)
	assert.Contains(t, result.Message, "expected output")
}
```

**Command Guidelines**:
- Keep commands simple and focused
- Provide clear usage messages
- Return user-friendly error messages
- Use context for state access

---

## Code Standards

### Style Guide

Follow the [Celeste Style Guide](./STYLE_GUIDE.md) for:
- Code formatting
- Naming conventions
- Comment standards
- Error handling
- Project organization

### Go Best Practices

**1. Formatting**:

```bash
# Format code before committing
go fmt ./...

# Run linter
go vet ./...
```

**2. Error Handling**:

```go
// DO: Check and handle errors
result, err := doSomething()
if err != nil {
	return fmt.Errorf("failed to do something: %w", err)
}

// DON'T: Ignore errors
result, _ := doSomething() // ❌
```

**3. Naming**:

```go
// Exported functions: PascalCase
func RegisterSkill(skill Skill) { }

// Unexported functions: camelCase
func parseArguments(args []string) { }

// Constants: PascalCase or SCREAMING_SNAKE_CASE
const MaxRetries = 3
const API_TIMEOUT = 30 * time.Second
```

**4. Comments**:

```go
// Package comments at top of package files
// Package skills provides AI-callable function definitions.
package skills

// Exported function comments (required)
// RegisterSkill adds a skill to the registry.
// Returns an error if the skill name is already registered.
func RegisterSkill(skill Skill) error {
	// Implementation comments (as needed)
}
```

---

## Testing Requirements

### Unit Tests Required

All new code must include unit tests:

- **Functions**: Test all exported functions
- **Packages**: Aim for >20% coverage (>70% for critical packages)
- **Edge Cases**: Test error conditions, nil values, empty inputs
- **Table-Driven**: Use table-driven tests for multiple scenarios

### Test File Naming

```
skill.go       → skill_test.go
registry.go    → registry_test.go
providers.go   → providers_test.go
```

### Example Test Structure

```go
func TestMyFunction(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		expected  string
		shouldErr bool
	}{
		{
			name:      "valid input",
			input:     "test",
			expected:  "result",
			shouldErr: false,
		},
		{
			name:      "empty input",
			input:     "",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MyFunction(tt.input)

			if tt.shouldErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
```

### Running Tests

```bash
# Run all tests
go test ./...

# Run with coverage
go test -cover ./cmd/celeste/...

# Run specific package
go test -v ./cmd/celeste/providers/

# Generate coverage report
go test -coverprofile=coverage.out ./cmd/celeste/...
go tool cover -html=coverage.out
```

---

## Documentation

### Required Documentation

When adding features, update:

1. **Code Comments**: Exported functions must have doc comments
2. **README.md**: Add to feature list if user-facing
3. **Relevant Docs**: Update LLM_PROVIDERS.md, TESTING.md, etc.
4. **CHANGELOG.md**: Add entry for new features/fixes

### Doc Comment Format

```go
// SkillName returns a Skill definition for the XYZ feature.
// It includes parameters for A, B, and C.
//
// Example:
//   skill := SkillName()
//   registry.RegisterSkill(skill)
func SkillName() Skill { }
```

### Markdown Documentation

- Use clear headings
- Include code examples
- Provide usage instructions
- Link to related docs

---

## Pull Request Process

### 1. Create a Branch

```bash
# Sync with upstream
git fetch upstream
git checkout main
git merge upstream/main

# Create feature branch
git checkout -b feature/my-feature
```

### 2. Make Changes

- Write code following style guide
- Add tests (required!)
- Update documentation
- Commit with clear messages

### 3. Test Everything

```bash
# Run all tests
go test ./...

# Check formatting
go fmt ./...

# Run linter
go vet ./...

# Test coverage
go test -cover ./cmd/celeste/...
```

### 4. Commit Messages

Follow conventional commits format:

```bash
# Format: <type>(<scope>): <description>

feat(skills): Add weather forecast skill
fix(providers): Fix Grok URL detection
docs(testing): Update testing guide
test(commands): Add provider command tests
refactor(llm): Simplify streaming logic
```

**Types**:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation only
- `test`: Adding/updating tests
- `refactor`: Code refactoring
- `style`: Formatting changes
- `chore`: Maintenance tasks

### 5. Push and Create PR

```bash
# Push to your fork
git push origin feature/my-feature
```

Then create a pull request on GitHub with:

- **Title**: Clear, descriptive (e.g., "Add weather forecast skill")
- **Description**: What changes were made and why
- **Testing**: How you tested the changes
- **Documentation**: What docs were updated
- **Breaking Changes**: If any

### 6. PR Review Process

- Maintainer will review within 1-3 days
- Address feedback and push updates
- Once approved, maintainer will merge

---

## Common Tasks

### Adding a New Test

```bash
# Create test file
touch cmd/celeste/mypackage/myfile_test.go

# Write tests using testify
# See TESTING.md for examples

# Run tests
go test -v ./cmd/celeste/mypackage/
```

### Testing a Provider

```bash
# Set API key
export PROVIDER_API_KEY="your-key"

# Run integration tests
go test -tags=integration -v ./cmd/celeste/providers/

# Test with CLI
./celeste config --set-url https://api.provider.com/v1
./celeste config --set-key "$PROVIDER_API_KEY"
./celeste chat
```

### Updating Documentation

```bash
# Edit docs
vim docs/LLM_PROVIDERS.md

# Preview markdown (optional)
# Use VS Code, GitHub preview, or markdown tool

# Commit
git add docs/
git commit -m "docs: Update provider documentation"
```

---

## Getting Help

- **Documentation**: Read [ARCHITECTURE.md](./ARCHITECTURE.md), [TESTING.md](./TESTING.md)
- **Issues**: Check existing issues on GitHub
- **Discussions**: Start a discussion for questions
- **Examples**: Look at existing code in `cmd/celeste/` for patterns

---

## Code of Conduct

- Be respectful and inclusive
- Provide constructive feedback
- Focus on the code, not the person
- Help others learn and grow

---

## Recognition

Contributors will be:
- Listed in README.md (if desired)
- Credited in release notes
- Thanked in commit messages

Thank you for contributing to Celeste CLI! 🎉

---

**Last Updated**: December 14, 2024
**Version**: v1.2.0

For more information:
- [Architecture Documentation](./ARCHITECTURE.md)
- [Testing Guide](./TESTING.md)
- [Style Guide](./STYLE_GUIDE.md)
- [Provider Documentation](./LLM_PROVIDERS.md)
