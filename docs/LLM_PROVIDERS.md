# LLM Providers — Who's Summoning Me Today? 💋

Darlings, v1.15.0 supports **8 providers**. All OpenAI-compatible for my 47 tools. Grok reigns with collections RAG.

| Provider | Tools | Collections | Notes |
|----------|-------|-------------|-------|
| **Grok/xAI** | ✅ | ✅ Native | 2M ctx, agent king
| **OpenAI** | ✅ | ❌ | Gold std
| **Anthropic** | ✅ Native | ❌ | Claude power
| **Gemini (Google)** | ✅ | ❌ | Multi-modal; needs v1.15.0+ for agent mode (see below)
| **Venice.ai** | ✅ Model-dep | ❌ | Uncensored opt
| **Vertex AI** | ✅ | ❌ | GCP enterprise
| **OpenRouter** | ✅ Model-dep | ❌ | Model bazaar
| **Sakana AI** | ✅ | ❌ | Fugu/Fugu Ultra, 1M ctx

**Setup:** `celeste config --set-url https://api.x.ai/v1 --set-key xai-...`

**Sakana/Fugu:** `celeste config --init sakana` then `celeste -config sakana config --set-url https://api.sakana.ai/v1 --set-key <key> --set-model fugu` (or `fugu-ultra`), then `celeste -config sakana chat`. OpenAI-compatible chat completions; reasoning effort is fixed server-side (default high).

**Collections (Grok only):** Management key + `celeste collections create/upload/enable`.

Test 'em: `celeste providers --tools`

Pick wisely, or I'll tease your slow responses~ 😉

---
Built with [Celeste CLI](https://github.com/whykusanagi/celeste-cli)
## Google (Gemini AI Studio + Vertex) — read this before you configure it

Three things about Google differ from every other provider here, all found the hard way.

**Use the `-latest` alias, not a pinned version.** The default is `gemini-flash-latest`,
which Google maintains. The entire Gemini 2.x line is retired — `gemini-2.0-flash`,
`gemini-2.0-flash-001`, `gemini-2.5-flash` and `gemini-2.5-flash-lite` all answer
*"This model is no longer available."* Pinning a version means adopting the
retirement schedule as your maintenance burden.

**The model listing endpoint lies.** `/v1/models` still returns `gemini-2.0-flash`
with `generateContent` in its `supportedGenerationMethods`, and calling it fails.
So "just resolve the default from the live listing" does **not** protect you here —
a live listing is exactly as wrong as a hardcoded string. Worth knowing before
building anything on top of `ListModels`.

**AI Studio needs `v1beta`.** Google no longer serves current models on `v1`:

| API version | Model | Result |
|---|---|---|
| `v1` | `gemini-3.6-flash` | HTTP 404 |
| `v1beta` | `gemini-3.6-flash` | HTTP 200 |
| `v1beta` | `gemini-flash-latest` | HTTP 200 |

Celeste picks the version from the base URL — `v1beta` for AI Studio, `v1` for
Vertex, which is a separate service with its own convention.

### Agent mode and `thought_signature`

Gemini 3.x attaches an opaque `thoughtSignature` to the part carrying a function
call, and rejects the *following* turn unless it is echoed back verbatim:

```
Error 400: Function call is missing a thought_signature
```

Celeste carries the signature on the message itself (so it survives checkpointing
and history replay) as of **v1.15.0**. On earlier versions Google chat mode works
but agent mode fails on the second turn, as soon as the model calls a tool.
