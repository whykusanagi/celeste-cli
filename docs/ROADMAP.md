# CelesteCLI Roadmap

This document tracks planned features, enhancements, and potential migrations for CelesteCLI.

## Current Status (December 2025)

### ✅ Completed
- Enterprise-grade RPG menu system with contextual help
- 18 AI-powered skills via function calling
- Multi-provider support (OpenAI, Grok, Venice, Anthropic, OpenRouter, DigitalOcean)
- Vertex AI support via OpenAI-compatible endpoint
- Session persistence and auto-save
- NSFW mode with Venice.ai image generation
- Context-aware /safe command
- Tool result filtering (hide raw JSON from UI)

---

## Short-Term (Q1 2025)

### High Priority

#### 1. Vertex AI (Google Cloud) Setup Guide
- **Status**: Requires complex Google Cloud configuration
- **Goal**: Document complete Vertex AI setup for enterprise users
- **Provider Clarification**:
  - **Gemini AI (AI Studio)**: Simple API keys ✅ READY
  - **Vertex AI (Google Cloud)**: OAuth2 tokens, requires setup ⚠️ ENTERPRISE ONLY

**Vertex AI Requirements:**
1. **Google Cloud Project**: Active GCP project with billing enabled
2. **Vertex AI API**: Enable Vertex AI API in GCP console
3. **Service Account**: Create service account with `Vertex AI User` role
4. **Authentication**: Use OAuth2 tokens (NOT simple API keys)
5. **Token Management**: Tokens expire after 1 hour, need refresh mechanism

**Endpoint URL Format:**
```
https://aiplatform.googleapis.com/v1/projects/{PROJECT_ID}/locations/{LOCATION}/endpoints/openapi
```

**Configuration Example:**
```json
{
  "api_key": "ya29.c.c0ASRK0Ga...",  // OAuth2 access token
  "base_url": "https://aiplatform.googleapis.com/v1/projects/my-project/locations/us-central1/endpoints/openapi",
  "model": "gemini-2.0-flash",
  "timeout": 60
}
```

**Token Generation (Go example):**
```go
import "golang.org/x/oauth2/google"

creds, _ := google.FindDefaultCredentials(ctx, "https://www.googleapis.com/auth/cloud-platform")
token, _ := creds.TokenSource.Token()
config.APIKey = token.AccessToken
```

**Why Most Users Should Use Gemini Instead:**
- Gemini AI: Simple API key from https://aistudio.google.com/apikey
- Vertex AI: Requires GCP project, billing, OAuth2 setup
- Both use same Gemini models, same function calling capabilities
- Vertex AI is for enterprise users with existing GCP infrastructure

- **Decision**: Gemini AI is the recommended provider for most users

#### 2. Improve Error Messaging
- **Goal**: Better error messages for common issues
- **Tasks**:
  - [ ] API key validation on startup
  - [ ] Clear messages for unsupported model features
  - [ ] Network timeout handling with retry suggestions
  - [ ] Configuration validation with fix suggestions

#### 3. Performance Optimization
- **Goal**: Reduce latency and improve responsiveness
- **Tasks**:
  - [ ] Stream parsing optimization
  - [ ] Reduce re-renders in TUI
  - [ ] Cache provider model lists
  - [ ] Lazy-load skill definitions

### Medium Priority

#### 4. Enhanced Skill Management
- **Goal**: Better skill discovery and usage
- **Tasks**:
  - [ ] /skills menu shows skill descriptions on hover/selection
  - [ ] Skill usage examples in help text
  - [ ] Skill execution history (recent skills used)
  - [ ] Skill favorites/pinning

#### 5. Configuration Management
- **Goal**: Easier config switching and management
- **Tasks**:
  - [ ] /config list shows model details and capabilities
  - [ ] /config test <profile> - test API connectivity
  - [ ] Config templates for common providers
  - [ ] Import/export config profiles

---

## Medium-Term (Q2-Q3 2025)

### Research & Evaluation

#### 6. Native Vertex AI SDK Migration (Option B)
- **Status**: Documented as potential enhancement
- **Trigger**: If Google deprecates OpenAI-compatible endpoint OR performance benefits are significant
- **Complexity**: MEDIUM (8-12 hours estimated)
- **Impact**: Better long-term stability, native Gemini features

**Migration Plan**:

##### Phase 1: Research & Prototyping (2-3 hours)
- [ ] Set up native genai SDK in test branch
- [ ] Prototype skill definition conversion (JSON → genai.FunctionDeclaration)
- [ ] Test response parsing (functionCall vs tool_calls)
- [ ] Benchmark performance vs OpenAI-compatible endpoint

##### Phase 2: Abstraction Layer (3-4 hours)
- [ ] Create provider abstraction interface
- [ ] Implement OpenAI provider adapter
- [ ] Implement native Gemini provider adapter
- [ ] Unified response format converter

##### Phase 3: Testing & Validation (2-3 hours)
- [ ] Test all 18 skills with native SDK
- [ ] Validate function calling accuracy
- [ ] Test multi-turn conversations
- [ ] Compare performance metrics

##### Phase 4: Migration (1-2 hours)
- [ ] Switch default Vertex AI provider to native SDK
- [ ] Update documentation
- [ ] Deprecation notice for OpenAI-compatible endpoint

**Code Structure Example**:
```go
// Future architecture with provider abstraction

type Provider interface {
    SendMessage(ctx context.Context, messages []Message, tools []Tool) (*Response, error)
    SupportsFunctionCalling() bool
    ConvertSkillDefinition(skill SkillDefinition) interface{}
}

type OpenAIProvider struct {
    client *openai.Client
}

type GeminiProvider struct {
    client *genai.Client
}

// Skills remain unchanged - adapters handle conversion
```

**Decision Criteria**:
- Google announces deprecation of OpenAI endpoint → Migrate immediately
- Native SDK shows >20% performance improvement → Evaluate migration
- Native SDK enables exclusive features → Evaluate value vs effort
- Community reports OpenAI endpoint issues → Migrate proactively

**Resources**:
- [Google GenAI SDK Docs](https://pkg.go.dev/google.golang.org/genai)
- [Function Calling Guide](https://ai.google.dev/gemini-api/docs/function-calling)
- [Migration Guide](https://ai.google.dev/gemini-api/docs/migrate)

---

### Feature Enhancements

#### 7. Advanced Skill Features
- **Goal**: More powerful AI capabilities
- **Tasks**:
  - [ ] Parallel skill execution (when LLM calls multiple)
  - [ ] Skill chaining (output of one feeds another)
  - [ ] Custom user-defined skills (plugin system)
  - [ ] Skill templates and generators

#### 8. Conversation Management
- **Goal**: Better session handling
- **Tasks**:
  - [ ] Named sessions (save/load by name)
  - [ ] Session search (find past conversations)
  - [ ] Session export (markdown, JSON)
  - [ ] Session branching (fork conversation)

#### 9. Multi-Modal Support
- **Goal**: Beyond text interactions
- **Tasks**:
  - [ ] Image input support (upload images for analysis)
  - [ ] File attachment support
  - [ ] Voice input/output (ElevenLabs integration)
  - [ ] PDF parsing and analysis

---

## Long-Term (Q4 2025+)

### Major Features

#### 10. Team/Collaboration Features
- **Goal**: Multi-user support
- **Tasks**:
  - [ ] Shared sessions
  - [ ] Team workspaces
  - [ ] Permission management
  - [ ] Audit logs

#### 11. Cloud Sync
- **Goal**: Cross-device session sync
- **Tasks**:
  - [ ] Optional cloud backup
  - [ ] Encrypted session storage
  - [ ] Device sync
  - [ ] Web UI companion

#### 12. Advanced Analytics
- **Goal**: Usage insights
- **Tasks**:
  - [ ] Token usage tracking
  - [ ] Cost estimation per provider
  - [ ] Skill usage analytics
  - [ ] Performance metrics dashboard

---

## Deprecated/Archived

### Considered but Deferred
- **Local LLM support**: Complexity vs benefit ratio too high for v1
- **Browser extension**: Scope too large, focus on CLI first
- **Mobile app**: Desktop/terminal focus more valuable

---

## Contributing to Roadmap

This roadmap is living document. To propose additions:

1. Check existing items to avoid duplicates
2. Open GitHub issue with `[Roadmap]` prefix
3. Describe:
   - Use case and problem being solved
   - Estimated complexity (LOW/MEDIUM/HIGH)
   - Impact on existing features
   - User benefit

Priority is determined by:
- User requests and feedback
- Technical dependencies
- Maintenance burden
- Strategic value

---

## Version History

- **v0.1** (Dec 2025): Initial roadmap creation
  - Enterprise menu system complete
  - Vertex AI OpenAI-compatible endpoint implemented
  - Native SDK migration documented as Option B
