# Celeste CLI Test Suite

This directory contains test scripts and fixtures for testing Celeste CLI functionality.

## Test Files

### One-Shot Commands Test (`test_oneshot_commands.sh`)

Comprehensive test suite for all CLI commands that can be executed without entering the TUI.

**Tests Covered**:
- Version and help commands
- Configuration management (--show, --list)
- Provider management (list, info, --tools, current)
- Skills management (--list, --info, --delete, --reload)
- Session management (--list)
- Context and stats commands
- Skill execution (safe skills like UUID generation, password generation)

**Usage**:
```bash
# From project root
./test/test_oneshot_commands.sh

# With custom binary
CELESTE_BIN=/path/to/celeste ./test/test_oneshot_commands.sh
```

**Docker Testing**:
```bash
# Build and run tests in container
docker build -f Dockerfile.oneshot --target tester -t celeste-test .
docker run celeste-test

# Using docker-compose
docker-compose -f docker-compose.oneshot.yml up oneshot-test
```

## Test Output

The test script provides colored output:
- 🟢 **GREEN**: Test passed
- 🔴 **RED**: Test failed
- 🟡 **YELLOW**: Warnings

Example output:
```
═══════════════════════════════════════════════════════════════
  CELESTE ONE-SHOT COMMANDS TEST SUITE
═══════════════════════════════════════════════════════════════

Testing binary: ./celeste

─────────────────────────────────────────────────────────────
  VERSION & HELP COMMANDS
─────────────────────────────────────────────────────────────
[1] Testing: version command ... PASS
[2] Testing: version flag ... PASS
[3] Testing: help command ... PASS

...

═══════════════════════════════════════════════════════════════
  TEST SUMMARY
═══════════════════════════════════════════════════════════════

Tests Run:    25
Tests Passed: 25
Tests Failed: 0

All tests passed!
```

## Adding New Tests

To add a new test, use the `run_test` function:

```bash
run_test "test description" "command to run" "expected output pattern"
```

Example:
```bash
run_test "providers list" "./celeste providers" "openai\|grok"
```

## Test Requirements

### Minimal Requirements
- Go 1.21+ (for building)
- Bash (for running test script)

### Docker Requirements
- Docker 20.10+
- Docker Compose 1.29+ (for docker-compose testing)

## CI/CD Integration

The test suite is designed to run in CI environments:

```yaml
# GitHub Actions example
- name: Build Binary
  run: go build -o celeste cmd/celeste/*.go

- name: Run One-Shot Tests
  run: ./test/test_oneshot_commands.sh
```

## Troubleshooting

### Test fails: "Binary not found"
```bash
# Build the binary first
go build -o celeste cmd/celeste/*.go

# Or specify binary location
CELESTE_BIN=/path/to/celeste ./test/test_oneshot_commands.sh
```

### Docker build fails
```bash
# Ensure you're in project root
cd /path/to/celeste-cli

# Build with verbose output
docker build -f Dockerfile.oneshot --target tester --progress=plain -t celeste-test .
```

### Permission denied on test script
```bash
chmod +x test/test_oneshot_commands.sh
```

## Test Coverage Goals

Current coverage:
- ✅ All one-shot commands (providers, skills, config, session)
- ✅ Safe skill execution (UUID, password generation)
- ⏳ Full skill suite (requires API keys)
- ⏳ TUI interactive testing (manual only)

Future additions:
- Integration tests with mock LLM responses
- Performance benchmarks
- Load testing for session management
