# Contributing to CelesteCLI

Thank you for your interest in contributing to CelesteCLI! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
- [Coding Standards](#coding-standards)
- [Testing](#testing)
- [Pull Request Process](#pull-request-process)
- [Commit Message Guidelines](#commit-message-guidelines)
- [Project Structure](#project-structure)

## Code of Conduct

This project adheres to a simple code of conduct:

- Be respectful and inclusive
- Welcome newcomers and help them learn
- Focus on constructive feedback
- Respect differing viewpoints and experiences
- Accept responsibility and learn from mistakes

## Getting Started

### Prerequisites

- Go 1.21 or higher
- Git
- A terminal with 256-color support (iTerm2, Alacritty, etc.)

### Fork and Clone

1. Fork the repository on GitHub
2. Clone your fork locally:
   ```bash
   git clone https://github.com/YOUR_USERNAME/celesteCLI.git
   cd celesteCLI
   ```

3. Add upstream remote:
   ```bash
   git remote add upstream https://github.com/whykusanagi/celesteCLI.git
   ```

4. Create a branch for your work:
   ```bash
   git checkout -b feature/your-feature-name
   ```

### Building the Project

```bash
# Install dependencies
go mod download

# Build the binary
make build

# Install to PATH
make install

# Development workflow (build + install + test)
make dev
```

## Development Workflow

### Before You Start

1. Check existing issues and PRs to avoid duplicate work
2. For major changes, open an issue first to discuss
3. Keep changes focused - one feature/fix per PR
4. Update documentation as needed

### Development Process

1. **Sync with upstream**:
   ```bash
   git fetch upstream
   git rebase upstream/main
   ```

2. **Make your changes**:
   - Follow coding standards (see below)
   - Write tests for new functionality
   - Update documentation

3. **Test your changes**:
   ```bash
   # Run tests
   go test ./...

   # Run go vet
   go vet ./...

   # Check formatting
   gofmt -l ./cmd
   ```

4. **Commit your changes** (see commit guidelines below)

5. **Push and create PR**:
   ```bash
   git push origin feature/your-feature-name
   ```

## Coding Standards

### Go Style Guide

We follow standard Go conventions:

- **Formatting**: Use `gofmt` (enforced)
  ```bash
  gofmt -w ./cmd
  ```

- **Naming**: Follow Go naming conventions
  - Exported names: `CamelCase`
  - Unexported names: `camelCase`
  - Acronyms: `HTTP`, `URL`, `ID` (not `Http`, `Url`, `Id`)

- **Comments**:
  - Package comments on every package
  - Exported functions must have doc comments
  - Comments should be full sentences
  ```go
  // NewClient creates a new LLM client with the given configuration.
  // It returns an error if the configuration is invalid.
  func NewClient(config *Config) (*Client, error) {
      // ...
  }
  ```

### Project-Specific Guidelines

1. **Error Handling**:
   - Always check errors
   - Wrap errors with context: `fmt.Errorf("load config: %w", err)`
   - Return errors, don't panic

2. **Interfaces**:
   - Keep interfaces small and focused
   - Define interfaces in the consumer package
   - Use dependency injection

3. **Package Organization**:
   ```
   cmd/Celeste/
   ├── main.go           # CLI entry point
   ├── tui/              # Bubble Tea UI components
   ├── llm/              # LLM client logic
   ├── config/           # Configuration management
   ├── skills/           # Skills/tools system
   └── prompts/          # System prompts
   ```

4. **Constants**:
   - Group related constants
   - Use typed constants where appropriate
   ```go
   const (
       Version = "3.0.0"
       Build   = "bubbletea-tui"
   )
   ```

5. **Context Usage**:
   - Pass context as first parameter
   - Use context for cancellation and timeouts
   ```go
   func SendMessage(ctx context.Context, msg string) error {
       // ...
   }
   ```

## Testing

### Writing Tests

We strive for good test coverage:

- **Unit Tests**: Test individual functions/methods
  ```go
  func TestConfigLoad(t *testing.T) {
      cfg, err := config.Load()
      if err != nil {
          t.Fatalf("Load failed: %v", err)
      }
      // assertions...
  }
  ```

- **Table-Driven Tests**: For multiple test cases
  ```go
  func TestMaskKey(t *testing.T) {
      tests := []struct {
          name  string
          input string
          want  string
      }{
          {"empty", "", "(not set)"},
          {"short", "abc", "****"},
          {"normal", "sk-1234567890", "sk-1...7890"},
      }

      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              got := maskKey(tt.input)
              if got != tt.want {
                  t.Errorf("got %q, want %q", got, tt.want)
              }
          })
      }
  }
  ```

### Running Tests

```bash
# Run all tests
go test ./...

# Run tests with coverage
go test -cover ./...

# Run tests with race detector
go test -race ./...

# Run specific test
go test -run TestConfigLoad ./cmd/Celeste/config
```

### Test Requirements

- All new features must have tests
- Bug fixes should include regression tests
- Aim for >60% coverage on new code
- Tests should be fast (<1s per package)

## Pull Request Process

### Before Submitting

- [ ] Code is formatted with `gofmt`
- [ ] All tests pass
- [ ] No `go vet` warnings
- [ ] Documentation is updated
- [ ] CHANGELOG.md is updated (if applicable)
- [ ] Commit messages follow guidelines

### PR Title Format

Use conventional commit format:

```
feat: add tarot reading skill
fix: resolve config loading bug
docs: update README installation steps
refactor: extract skill registry logic
test: add unit tests for session manager
```

### PR Description Template

```markdown
## Description
Brief description of changes

## Type of Change
- [ ] Bug fix (non-breaking change)
- [ ] New feature (non-breaking change)
- [ ] Breaking change (fix or feature that changes existing behavior)
- [ ] Documentation update

## Testing
- [ ] Unit tests added/updated
- [ ] Manual testing performed
- [ ] No regressions observed

## Checklist
- [ ] Code follows project style guidelines
- [ ] Self-review completed
- [ ] Comments added for complex logic
- [ ] Documentation updated
- [ ] No new warnings introduced
```

### Review Process

1. **Automated Checks**: CI must pass (tests, linting)
2. **Code Review**: At least one maintainer approval
3. **Testing**: Manual testing for UI changes
4. **Merge**: Squash and merge (clean commit history)

## Commit Message Guidelines

We follow [Conventional Commits](https://www.conventionalcommits.org/):

### Format

```
<type>(<scope>): <subject>

<body>

<footer>
```

### Types

- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

### Examples

```
feat(skills): add weather skill with zip code support

Implements get_weather skill that fetches current weather data
using OpenWeatherMap API. Supports default zip code from config.

Closes #42
```

```
fix(config): handle missing secrets.json gracefully

Previously crashed when secrets.json didn't exist. Now creates
file with empty defaults and continues.

Fixes #38
```

```
docs: update README with new skill system

- Add skills section with examples
- Document skill configuration
- Update troubleshooting guide
```

## Project Structure

```
celesteCLI/
├── cmd/Celeste/           # Main application
│   ├── main.go            # Entry point
│   ├── tui/               # TUI components
│   │   ├── app.go         # Main app model
│   │   ├── chat.go        # Chat viewport
│   │   ├── input.go       # Input field
│   │   ├── skills.go      # Skills panel
│   │   └── styles.go      # Styling
│   ├── llm/               # LLM integration
│   │   ├── client.go      # OpenAI client
│   │   └── stream.go      # Streaming
│   ├── config/            # Configuration
│   │   ├── config.go      # Config management
│   │   └── session.go     # Session persistence
│   ├── skills/            # Skills system
│   │   ├── registry.go    # Skill registry
│   │   ├── executor.go    # Execution engine
│   │   └── builtin.go     # Built-in skills
│   └── prompts/           # System prompts
│       └── celeste.go     # Prompt loader
├── docs/                  # Documentation
│   ├── ROUTING.md
│   ├── PERSONALITY.md
│   └── CAPABILITIES.md
├── README.md
├── LICENSE
├── CONTRIBUTING.md        # This file
└── SECURITY.md
```

## Need Help?

- **Questions**: Open a GitHub Discussion
- **Bugs**: Open a GitHub Issue
- **Security**: See [SECURITY.md](SECURITY.md)
- **Chat**: Contact @whykusanagi

## Recognition

Contributors will be acknowledged in:
- Release notes
- README contributors section
- GitHub contributors graph

Thank you for contributing to CelesteCLI!
