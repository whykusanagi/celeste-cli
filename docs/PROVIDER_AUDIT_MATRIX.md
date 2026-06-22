# Provider Audit Matrix v1.11.0

8 providers validated. All production-ready.

**Updated**: 2026-06-22 | **Tests**: Unit 100% + Integration ✅

| Provider | Fn Calling | Models | Tokens | Streaming | OpenAI Compat | Status |
|----------|------------|--------|--------|-----------|---------------|--------|
| OpenAI | ✅ | ✅ Dynamic | ✅ | ✅ | ✅ Native | ⭐ Gold |
| Grok/xAI | ✅ | ✅ Dynamic | ✅ | ✅ | ✅ Full | ⭐ Gold |
| Venice | ✅ (llama) | ⚠️ Static | ✅ | ✅ | ⚠️ Partial | ✅ Working |
| Anthropic | ✅ Compat | ⚠️ Static | ✅ | ✅ | ⚠️ Limited | ✅ Working |
| Gemini | ✅ Compat | ✅ | ✅ | ✅ | ✅ Compat | ✅ Tested |
| Vertex AI | ✅ Compat | ✅ | ✅ | ✅ | ✅ Compat | ✅ Tested |
| OpenRouter | ✅ Model-dep | ✅ Dynamic | ✅ | ✅ | ✅ Full | ✅ Tested |
| Sakana AI | ✅ | ✅ Dynamic | ✅ | ✅ | ✅ Full | ✅ Tested |

## Details

All support streaming/tool calls/tokens in v1.11.0.
Grok: 2M ctx. Venice: NSFW. Sakana: 1M ctx (fugu/fugu-ultra).

**Tests**: go test ./cmd/celeste/providers/... -tags=integration

**Built with [Celeste CLI](https://github.com/whykusanagi/celeste-cli)**