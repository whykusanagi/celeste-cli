# LLM Provider Compatibility Matrix

Celeste CLI uses OpenAI's function calling feature to power its skills system. This document explains which LLM providers support skills and which ones require alternative setups.

**📊 For comprehensive test results and validation status, see [PROVIDER_AUDIT_MATRIX.md](./PROVIDER_AUDIT_MATRIX.md)**

## Quick Reference

| Provider | Function Calling Support | Collections | Status | Test Coverage | Notes |
|----------|-------------------------|-------------|---------|---------------|-------|
| **OpenAI** | ✅ Native | ❌ | ✅ Tested | Unit ✅ Integration 🔜 | Gold standard, all features working |
| **Grok (xAI)** | ✅ OpenAI-Compatible | ✅ Native | ✅ Tested | Unit ✅ Integration 🔜 | 2M context, function calling, Collections (RAG) |
| **Venice.ai** | ⚠️ Model-Dependent | ❌ | ✅ Tested | Unit ✅ Integration 🔜 | llama-3.3-70b supports tools, uncensored does not |
| **Anthropic** | ⚠️ Via Compatibility | ❌ | ✅ Tested | Unit ✅ Integration 🔜 | OpenAI mode limited, native API recommended |
| **Gemini** | ❓ Via Compatibility | ❌ | ⚠️ Configured | Unit ✅ Integration ❓ | Needs API key for testing |
| **Vertex AI** | ❓ Via Compatibility | ❌ | ⚠️ Configured | Unit ✅ Integration ❓ | Requires OAuth setup |
| **OpenRouter** | ⚠️ Model-Dependent | ❌ | ⚠️ Configured | Unit ✅ Integration ❓ | Aggregator, varies by selected model |
| **DigitalOcean** | 🔒 Cloud Functions Only | ❌ | ⚠️ Limited | Unit ✅ Integration 🔒 | Cloud-hosted only, not for local CLI |
| **ElevenLabs** | ❓ Unknown | ❌ | ⚠️ Registered | Unit ✅ Integration ❓ | Voice API, different use case |

---

## How Skills Work

Celeste CLI's skills system relies on **OpenAI function calling** (also known as tool calling). Here's how it works:

1. **User asks a question**: "What's the weather in NYC?"
2. **Skills are sent to LLM**: The list of available skills is sent as "tools" in the API request
3. **LLM decides to call a skill**: The LLM recognizes it needs the `get_weather` function
4. **LLM returns a tool call**: Instead of text, it returns structured data: `{"name": "get_weather", "arguments": {"location": "NYC"}}`
5. **Celeste executes the skill**: The skill handler fetches weather data
6. **Result sent back to LLM**: The weather data is sent back to the LLM
7. **LLM generates natural response**: "It's 45°F and cloudy in New York City..."

**This requires the LLM to support structured function calling.** Not all providers support this feature.

---

## Supported Providers

### ✅ OpenAI (Fully Supported)

**API Endpoint**: `https://api.openai.com/v1`
**Function Calling**: Native support
**Models**: gpt-4o, gpt-4o-mini, gpt-4-turbo, gpt-3.5-turbo (with function calling)

**Setup**:
```bash
celeste config --set-key YOUR_OPENAI_KEY
celeste config --set-url https://api.openai.com/v1
celeste config --set-model gpt-4o-mini
celeste chat
```

**Why it works**: OpenAI invented function calling and has the most robust implementation.

---

### ✅ Grok (xAI) (Fully Supported + Native Backend)

**API Endpoint**: `https://api.x.ai/v1`
**Backend**: Native xAI backend (auto-detected)
**Function Calling**: Full native support
**Models**: grok-4-1-fast (recommended for tool calling), grok-4-1, grok-beta
**Collections (RAG)**: ✅ Fully supported

**Setup**:
```bash
celeste config --set-key YOUR_GROK_KEY
celeste config --set-url https://api.x.ai/v1
celeste config --set-model grok-4-1-fast
celeste chat
```

**Why it works**:
- Celeste automatically detects xAI URLs and uses the **native xAI backend**
- Bypasses OpenAI SDK limitations for full Collections support
- Direct API control enables xAI-specific features
- `grok-4-1-fast` is specifically trained for agentic tool calling
- 2M context window for large conversations

**Auto-Detection**: When you use `https://api.x.ai/v1`, Celeste logs:
```
Using xAI backend with Collections support
```

**Testing**: Run provider tests to verify:
```bash
GROK_API_KEY=your-key go test ./cmd/celeste/providers -run TestGrokIntegration -v
```

### Collections Support (RAG)

Grok is the **only provider that supports Collections** - xAI's built-in RAG (Retrieval-Augmented Generation) system.

**What are Collections?**
- Upload your own documents (docs, notes, lore, references)
- Celeste automatically searches them during conversations
- Powered by xAI's Management API and semantic search

**Quick Setup**:

1. **Get Management API Key** (separate from chat key):
   - Visit [https://console.x.ai](https://console.x.ai)
   - Create Management API Key with `AddFileToCollection` permission

2. **Configure Key**:
   ```bash
   celeste config --set-management-key xai-YOUR-MANAGEMENT-KEY
   ```

3. **Create Collection**:
   ```bash
   celeste collections create "my-docs" --description "My documentation"
   ```

4. **Upload Documents**:
   ```bash
   celeste collections upload <collection-id> docs/*.md --recursive
   ```

5. **Enable for Chat**:
   ```bash
   celeste collections enable <collection-id>
   ```

6. **Start Chatting**:
   ```bash
   celeste chat
   ```

Celeste will now search your documents automatically when relevant!

**Supported Formats**: .md, .txt, .pdf, .html (10MB limit per file)

**Full Guide**: See [COLLECTIONS.md](./COLLECTIONS.md) for comprehensive documentation including TUI interface, CLI commands, best practices, and troubleshooting.

---

### ⚠️ DigitalOcean (Limited Support)

**API Endpoint**: `https://api.digitalocean.com/v2/ai`
**Function Calling**: Requires cloud-hosted functions
**Models**: Various (llama-3, mistral, etc.)

**Limitation**: DigitalOcean AI Agent **does not support local function execution**. Instead:

1. You must deploy each skill as a **cloud function** (DigitalOcean Functions, AWS Lambda, etc.)
2. Attach function URLs to your agent via the DigitalOcean API
3. The agent calls these URLs directly (not your local machine)

**Why skills won't work**:
- Celeste CLI executes skills locally (unit converter, QR code generator, etc.)
- DigitalOcean expects HTTP endpoints in the cloud
- No way to bridge local execution with DigitalOcean's architecture

**Workarounds**:
1. **Use a different provider**: OpenAI, Grok, or other OpenAI-compatible providers
2. **Deploy skills as cloud functions**: Rewrite each skill as an HTTP endpoint and deploy to the cloud
3. **Manual invocation**: Don't use AI-driven skills; call skills manually via command line flags (not implemented in v3.0)

**Documentation**: https://docs.digitalocean.com/products/ai/getting-started/ai-agents/

---

### ❓ ElevenLabs (Validation Pending)

**API Endpoint**: `https://api.elevenlabs.io/v1`
**Function Calling**: Unknown
**Models**: Various (conversational AI models)

**Status**: No dedicated integration test exists yet in this repository. ElevenLabs focuses on voice AI, so function-calling behavior for local CLI skills remains uncertain.

**To test**:
1. Run baseline provider unit tests: `go test ./cmd/celeste/providers -v`
2. Run a live manual check in chat mode with an ElevenLabs endpoint/profile.

---

### ⚠️ Venice.ai (Model-Dependent, Integration Covered)

**API Endpoint**: `https://api.venice.ai/api/v1`
**Function Calling**: Model-dependent
**Models**: venice-uncensored, various uncensored models

**Status**: Integration test exists for Venice and confirms model-specific behavior. `venice-uncensored` does not support function calling; tool-capable Venice models can.

**To test**:
```bash
VENICE_API_KEY=your-key go test ./cmd/celeste/providers -run TestVeniceIntegration -v
```

Venice.ai is already used for NSFW skill and image generation, but those use direct API calls, not function calling.

---

### ⚠️ Local Models (Varies)

**Tools**: Ollama, LM Studio, text-generation-webui
**Function Calling**: Depends on model and tool

**Ollama** (with compatible models):
- Some models support function calling (e.g., llama3.1 with tool use)
- Configure like OpenAI:
  ```bash
  celeste config --set-key ollama
  celeste config --set-url http://localhost:11434/v1
  celeste config --set-model llama3.1
  ```
- Test baseline compatibility paths: `go test ./cmd/celeste/llm -v`

**LM Studio**:
- Supports OpenAI-compatible API
- Function calling support depends on loaded model
- Configure similarly to Ollama

**Why it might not work**:
- Many local models don't support structured function calling
- They may hallucinate function calls (produce fake JSON that doesn't work)
- Smaller models struggle with complex tool schemas

---

## Testing Provider Compatibility

To verify if your provider supports skills:

### 1. Run Provider Tests

```bash
# Test OpenAI
OPENAI_API_KEY=your-key go test ./cmd/celeste/providers -run TestOpenAIIntegration -v

# Test Grok
GROK_API_KEY=your-key go test ./cmd/celeste/providers -run TestGrokIntegration -v

# Test Venice.ai
VENICE_API_KEY=your-key go test ./cmd/celeste/providers -run TestVeniceIntegration -v
```

### 2. Manual Test

Try using a skill in chat:

```bash
celeste chat
> What's the weather in 10001?
```

**Expected behavior (works)**:
```
👁️ Thinking...
[Celeste calls get_weather skill]
It's 45°F and cloudy in New York City (10001)...
```

**Problem behavior (doesn't work)**:
```
👁️ Thinking...
I don't have access to real-time weather data.
```

If the LLM says it "doesn't have access" or "can't retrieve real-time data", the provider likely doesn't support function calling.

---

## What If My Provider Doesn't Support Skills?

If your LLM provider doesn't support function calling, you have several options:

### Option 1: Switch to a Compatible Provider

Use OpenAI or Grok, which fully support skills:
```bash
celeste config --init openai
celeste config --set-key YOUR_OPENAI_KEY
celeste -config openai chat
```

### Option 2: Use Skills Separately

While Celeste CLI v3.0 doesn't have direct skill invocation flags, you could:
- Use skills via the chat interface with compatible providers only
- Request manual skill invocation flags (contribute to the project!)

### Option 3: Deploy Cloud Functions (Advanced)

For DigitalOcean or similar platforms:
1. Deploy each skill as a cloud function (AWS Lambda, DigitalOcean Functions, Cloudflare Workers)
2. Create HTTP endpoints for each skill
3. Attach these endpoints to your AI agent via provider API
4. Agent calls cloud functions directly

This is complex and requires infrastructure setup.

---

## Contributing

If you test a provider not listed here, please contribute your findings:

1. Run provider tests (see `cmd/celeste/providers/integration_test.go` and `cmd/celeste/providers/registry_test.go`)
2. Document the results (works, doesn't work, partial support)
3. Create a pull request updating this file
4. Include setup instructions and any gotchas

**Providers to test**:
- Anthropic Claude (via API)
- Google Gemini
- Cohere
- Hugging Face Inference API
- Replicate
- Together.ai
- Perplexity AI
- Mistral AI

---

## Technical Details

### OpenAI Function Calling Format

Celeste CLI sends skills in this format:

```json
{
  "model": "gpt-4o-mini",
  "messages": [...],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get current weather for a location",
        "parameters": {
          "type": "object",
          "properties": {
            "location": {
              "type": "string",
              "description": "City name or zip code"
            }
          },
          "required": ["location"]
        }
      }
    }
  ]
}
```

The LLM responds with:

```json
{
  "choices": [{
    "message": {
      "role": "assistant",
      "tool_calls": [{
        "id": "call_abc123",
        "type": "function",
        "function": {
          "name": "get_weather",
          "arguments": "{\"location\": \"NYC\"}"
        }
      }]
    }
  }]
}
```

### Compatibility Checklist

For a provider to support skills, it must:

1. ✅ Accept `tools` array in chat completion requests
2. ✅ Return `tool_calls` in response messages (not just text)
3. ✅ Parse function parameters correctly (JSON schema validation)
4. ✅ Allow sending tool results back to the LLM
5. ✅ Continue the conversation after tool execution

If any of these fail, skills won't work properly.

---

## Provider Test Results (v1.5.x)

### Unit Test Coverage ✅ COMPLETE

All 9 providers have been validated with comprehensive unit tests:

**Test Files**:
- `cmd/celeste/providers/registry_test.go` (13 test functions)
- `cmd/celeste/providers/models_test.go` (14 test functions)

**Coverage**:
- 27 test functions
- 70+ test cases (including sub-tests)
- 100% pass rate

**Validated**:
- ✅ Provider registration and capabilities
- ✅ Model detection and static model lists
- ✅ Function calling support detection
- ✅ URL pattern recognition
- ✅ Tool-capable provider filtering

**Run Tests**:
```bash
go test ./cmd/celeste/providers/
```

### Integration Test Framework 🔜 READY

Integration tests with real API calls are ready to run:

**Test File**: `cmd/celeste/providers/integration_test.go`

**Providers Covered**:
- ✅ OpenAI (full test suite)
- ✅ Grok (full test suite)
- ✅ Gemini (basic tests)
- ✅ Anthropic (OpenAI mode tests)
- ✅ Venice (model-specific tests)

**Features Tested**:
- Chat completion
- Function calling with tools
- Streaming responses
- Model listing

**Run Integration Tests**:
```bash
export OPENAI_API_KEY="sk-..."
export GROK_API_KEY="xai-..."
go test -tags=integration -v ./cmd/celeste/providers/
```

**Documentation**:
- Integration test guide: `cmd/celeste/providers/INTEGRATION_TESTS.md`
- Full audit matrix: `docs/PROVIDER_AUDIT_MATRIX.md`

### One-Shot Command Tests ✅ PASSING

Provider management commands validated:

```bash
./test/test_oneshot_commands.sh
```

**Tests**: 21/21 passing (including 6 provider-specific tests)

**Commands Tested**:
- `./celeste providers` - List all providers
- `./celeste providers --tools` - List tool-capable providers
- `./celeste providers info <name>` - Show provider details
- `./celeste providers current` - Show current provider

### Overall Test Coverage (v1.2.0)

Comprehensive test suites added across critical packages:

**Package Coverage**:
- ✅ prompts: 97.1% (16 test functions - persona, NSFW mode, content generation)
- ✅ providers: 72.8% (27 test functions - registry, models, capabilities)
- ✅ config: 52.0% (session management, configuration)
- ✅ commands: 25.8% (17 test functions - includes providers command tests)
- ✅ venice: 22.6% (9 test functions - media parsing, downloads, base64)
- ✅ skills: 12.2% (18 test functions - registry, tool definitions)
- ⏳ llm: 0% (requires HTTP mocking infrastructure)
- ⏳ tui: 0% (requires Bubble Tea/tcell mocking)

**Total Coverage**: 17.4%

**Run All Tests**:
```bash
go test ./cmd/celeste/... -cover
go test -coverprofile=coverage.out ./cmd/celeste/... && go tool cover -html=coverage.out
```

**Test Files Added**:
- `cmd/celeste/prompts/celeste_test.go` (new)
- `cmd/celeste/venice/media_test.go` (new)
- `cmd/celeste/commands/commands_test.go` (enhanced)
- `cmd/celeste/tools/registry_test.go` (enhanced)

---

## FAQ

**Q: Can I use skills without function calling?**
A: No, Celeste CLI v3.0 requires function calling. Skills are AI-driven, not manually invoked.

**Q: Will you add support for providers without function calling?**
A: This would require a different architecture (prompt-based skill invocation, which is less reliable). Open an issue to discuss!

**Q: My provider says it supports function calling but skills don't work**
A: Run the provider tests to diagnose. The provider might have partial support or different JSON format requirements.

**Q: Can I use multiple providers (one for chat, one for skills)?**
A: Not currently. Skills are deeply integrated with the chat flow. You'd need custom code to route requests.

**Q: Does streaming work with function calling?**
A: Yes! Celeste CLI uses streaming for all responses, including function calls. The LLM streams the function call data, then streams the final response after skill execution.

---

**Last Updated**: December 14, 2024
**Celeste CLI Version**: v1.2.0-dev
**Test Coverage**: Unit tests complete, integration tests ready
