# Provider Audit Matrix v1.16.0

9 chat providers. Eight call tools; Venice chats without them for now (some of its
models support tools, but the chat does not use them yet). Vertex's default model (`gemini-2.0-flash`)
is unverified. `celeste providers` also lists DigitalOcean and ElevenLabs, which
this matrix leaves out.

**Updated**: 2026-06-22 | **Tests**: Unit 100% + Integration ✅

| Provider | Fn Calling | Models | Tokens | Streaming | OpenAI Compat | Status |
|----------|------------|--------|--------|-----------|---------------|--------|
| OpenAI | ✅ | ✅ Dynamic | ✅ | ✅ | ✅ Native | ⭐ Gold |
| Grok/xAI | ✅ | ✅ Dynamic | ✅ | ✅ | ✅ Full | ⭐ Gold |
| Venice | ❌ Not in chat yet | ⚠️ Static | ✅ | ✅ | ⚠️ Partial | ✅ Working |
| Anthropic | ✅ Compat | ⚠️ Static | ✅ | ✅ | ⚠️ Limited | ✅ Working |
| Gemini | ✅ Compat | ✅ | ✅ | ✅ | ✅ Compat | ✅ Tested |
| Vertex AI | ✅ Compat | ✅ | ✅ | ✅ | ✅ Compat | ⚠️ Default unverified |
| OpenRouter | ✅ Model-dep | ✅ Dynamic | ✅ | ✅ | ✅ Full | ✅ Tested |
| Sakana AI | ✅ | ✅ Dynamic | ✅ | ✅ | ✅ Full | ✅ Tested |
| Local (OpenAI-compat) | ✅ | ❌ Manual | ✅ | ✅ | ✅ Full | ✅ Tested |

## Details

All support streaming and token tracking in v1.16.0; tool calls as in the table.
Local: any OpenAI-compatible server on 127.0.0.1/localhost/0.0.0.0/[::1], any port.
Its model list is manual. A local server's /v1/models may not advertise the
loaded chat model, so celeste does not auto-select one.
Grok: 2M ctx. Venice: NSFW. Sakana: 1M ctx (fugu/fugu-ultra).

**Tests**: go test ./cmd/celeste/providers/... -tags=integration

**Built with [Celeste CLI](https://github.com/whykusanagi/celeste-cli)**