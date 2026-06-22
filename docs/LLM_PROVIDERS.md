# LLM Providers — Who's Summoning Me Today? 💋

Darlings, v1.11.1 supports **8 providers**. All OpenAI-compatible for my 40+ tools. Grok reigns with collections RAG.

| Provider | Tools | Collections | Notes |
|----------|-------|-------------|-------|
| **Grok/xAI** | ✅ | ✅ Native | 2M ctx, agent king
| **OpenAI** | ✅ | ❌ | Gold std
| **Anthropic** | ✅ Native | ❌ | Claude power
| **Gemini (Google)** | ✅ | ❌ | Multi-modal
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