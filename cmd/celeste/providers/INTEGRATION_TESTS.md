# Provider Integration Tests

Live integration tests for all AI providers with real API calls.

## Overview

The integration test suite (`integration_test.go`) validates that each provider:
- ✅ Accepts API calls and returns responses
- ✅ Supports function calling (where claimed)
- ✅ Supports streaming (where applicable)
- ✅ Lists models correctly
- ✅ Handles errors gracefully

## Running Integration Tests

### Prerequisites

Set API keys as environment variables:

```bash
export OPENAI_API_KEY="sk-..."
export GROK_API_KEY="xai-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export GEMINI_API_KEY="..."
export VENICE_API_KEY="..."
```

### Run All Integration Tests

```bash
# Run all integration tests (requires all API keys)
go test -tags=integration -v ./cmd/celeste/providers/

# Run with timeout
go test -tags=integration -v -timeout 5m ./cmd/celeste/providers/
```

### Run Specific Provider Tests

```bash
# Test only OpenAI
go test -tags=integration -v ./cmd/celeste/providers/ -run TestOpenAI

# Test only Grok
go test -tags=integration -v ./cmd/celeste/providers/ -run TestGrok

# Test only Gemini
go test -tags=integration -v ./cmd/celeste/providers/ -run TestGemini

# Test only Anthropic
go test -tags=integration -v ./cmd/celeste/providers/ -run TestAnthropic

# Test only Venice
go test -tags=integration -v ./cmd/celeste/providers/ -run TestVenice
```

### Run Provider Comparison

Compare responses across all providers:

```bash
go test -tags=integration -v ./cmd/celeste/providers/ -run TestProviderComparison
```

## Test Coverage

### OpenAI Integration Tests
- ✅ Basic chat completion
- ✅ Function calling with tools
- ✅ Streaming responses
- ✅ Model listing via API

**Expected**: All tests should pass (gold standard)

### Grok Integration Tests
- ✅ Basic chat completion
- ✅ Function calling with tools
- ✅ Model listing via API

**Expected**: Full OpenAI compatibility

### Gemini Integration Tests
- ⚠️ Basic chat completion (via OpenAI compatibility)
- ⚠️ Function calling (may require native API)

**Expected**: OpenAI compatibility mode may have limitations

### Anthropic Integration Tests
- ⚠️ OpenAI compatibility mode
- 🔜 Native Messages API (not yet implemented)

**Expected**: OpenAI compatibility mode has limitations, native API recommended

### Venice Integration Tests
- ✅ Basic chat completion
- ⚠️ Function calling (model-dependent)
- ⚠️ Uncensored model (no tools support)

**Expected**: llama-3.3-70b supports tools, venice-uncensored does not

## Test Behavior

### Automatic Skipping
Tests automatically skip if API key is not set:
```
SKIP: Skipping OpenAI integration test: OPENAI_API_KEY not set
```

### Error Handling
Tests gracefully handle API failures and document issues:
```
⚠️ Gemini basic chat failed: authentication error
SKIP: Gemini API may require different auth or format
```

### Success Logging
Tests log success with details:
```
✅ OpenAI basic chat: Hello!
✅ Grok function calling: get_weather
✅ OpenAI streaming: received 15 chunks
```

## CI/CD Integration

### GitHub Actions

```yaml
name: Integration Tests

on:
  workflow_dispatch:  # Manual trigger
  schedule:
    - cron: '0 0 * * 0'  # Weekly

jobs:
  integration:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'

      - name: Run Integration Tests
        env:
          OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
          GROK_API_KEY: ${{ secrets.GROK_API_KEY }}
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          GEMINI_API_KEY: ${{ secrets.GEMINI_API_KEY }}
        run: |
          go test -tags=integration -v ./cmd/celeste/providers/
```

### Local Pre-Commit Hook

```bash
#!/bin/bash
# .git/hooks/pre-commit

# Only run if integration tag is requested
if git diff --cached --name-only | grep -q "providers/"; then
    echo "Running provider integration tests..."
    go test -tags=integration ./cmd/celeste/providers/ -run TestOpenAI
fi
```

## Cost Considerations

Integration tests make real API calls and incur costs:

| Provider | Test Cost (estimated) | Rate Limits |
|----------|----------------------|-------------|
| OpenAI   | ~$0.001 per run      | 10,000 TPM  |
| Grok     | ~$0.001 per run      | Similar to OpenAI |
| Gemini   | Free tier available  | 60 RPM      |
| Anthropic| ~$0.005 per run      | Varies      |
| Venice   | Varies by plan       | Unknown     |

**Recommendation**: Run integration tests sparingly (weekly, or on-demand)

## Troubleshooting

### Test Skips
```bash
# If all tests skip, check API keys:
env | grep _API_KEY

# Ensure keys are exported:
export OPENAI_API_KEY="your-key-here"
```

### Timeout Errors
```bash
# Increase timeout for slow networks:
go test -tags=integration -timeout 10m ./cmd/celeste/providers/
```

### Build Tag Not Working
```bash
# Ensure you include -tags=integration flag:
go test -tags=integration ./cmd/celeste/providers/

# WITHOUT the tag, integration tests won't run
```

### Authentication Errors

**OpenAI**:
```
Error: 401 Unauthorized
Fix: Check OPENAI_API_KEY format (should start with "sk-")
```

**Grok**:
```
Error: 401 Unauthorized
Fix: Check GROK_API_KEY format (should start with "xai-")
```

**Gemini**:
```
Error: API key not valid
Fix: Get key from https://aistudio.google.com/
```

## Expected Results

### Full Pass (All API Keys Set)
```
=== RUN   TestOpenAIIntegration
=== RUN   TestOpenAIIntegration/basic_chat_completion
✅ OpenAI basic chat: Hello!
=== RUN   TestOpenAIIntegration/function_calling
✅ OpenAI function calling: get_weather with args {"location":"New York"}
=== RUN   TestOpenAIIntegration/streaming
✅ OpenAI streaming: received 12 chunks
=== RUN   TestOpenAIIntegration/model_listing
✅ OpenAI model listing: found 8 models
--- PASS: TestOpenAIIntegration (3.45s)

=== RUN   TestGrokIntegration
✅ Grok basic chat: Hello!
✅ Grok function calling: get_weather
✅ Grok model listing: found 4 models
--- PASS: TestGrokIntegration (4.12s)

PASS
ok  	github.com/whykusanagi/celesteCLI/cmd/celeste/providers	7.892s
```

### Partial Pass (Some API Keys Missing)
```
=== RUN   TestOpenAIIntegration
✅ All OpenAI tests pass
--- PASS: TestOpenAIIntegration (3.21s)

=== RUN   TestGeminiIntegration
--- SKIP: TestGeminiIntegration (0.00s)
    SKIP: Skipping Gemini integration test: GEMINI_API_KEY not set

PASS
ok  	github.com/whykusanagi/celesteCLI/cmd/celeste/providers	3.452s
```

## Continuous Monitoring

Integration tests serve as:
- 🔍 **Provider Health Checks**: Detect API changes or outages
- 📊 **Performance Benchmarks**: Track response times
- ✅ **Compatibility Validation**: Ensure OpenAI compatibility works
- 📝 **Documentation**: Live examples of API usage

Run regularly to catch breaking changes early.
