# LLM Providers — Who's Summoning Me Today? 💋

Darlings, v1.16.0 supports **9 providers**. All OpenAI-compatible for my 47 tools. Grok reigns with collections RAG.

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
## Local models (mlx-vlm, Ollama, LM Studio, llama.cpp)

Any OpenAI-compatible server on `127.0.0.1`, `localhost`, `0.0.0.0` or `[::1]`
detects as the **local** provider and is treated as tool-capable, on any port.

```bash
celeste config -config local --set-url http://127.0.0.1:8080/v1
celeste config -config local --set-key not-needed
celeste config -config local --set-model <whatever your server expects>
```

Two things differ from a hosted provider.

**The model name is whatever your server wants.** celeste does not guess one and
will not overwrite yours. mlx-vlm in particular wants the full filesystem path to
the weights; give it a short name and it tries to fetch a HuggingFace repo by
that name and returns 404.

**`GET /v1/models` is not a reliable catalogue.** The mlx-vlm server advertises
only its embedding model, not the chat model it has loaded, so `local` is
registered with model listing disabled. Configure the model by hand.

### Set the context window

celeste cannot know a local server's context window. The model name is an
arbitrary string, so nothing in the model table matches it and the fallback is a
conservative 8192 tokens. Left alone that truncates a model with a 128k window
long before it needs to be, so set it to whatever you started the server with:

```bash
celeste config -config local --set-context-limit 32768
celeste config -config local            # Context Limit: 32768 tokens (configured)
```

`config` reports where the number came from: `configured`, `model default`, or
`fallback, model unknown`. That last one means celeste is guessing, so set it.

`--set-context-limit 0` clears the setting and returns to the model default.

### Which commands can use tools

| | tools |
|---|---|
| `celeste chat` (TUI, incl. claw mode) | yes |
| `celeste agent` | yes |
| `celeste message` | no |

`celeste message` sends no tools for **any** provider, local or hosted. It is a
one-shot chat command with no tool-execution loop. For non-interactive tool use,
`celeste agent` is that loop:

```bash
celeste -config local agent -auto-approve --goal "read README.md and summarise it"
```

Expect local inference to be slow enough that a turn feels stalled. A 27B model
at ~6 tok/s takes roughly half a minute per turn.

## Google (Gemini AI Studio + Vertex)

Google behaves differently from the other providers here in three ways. Each one
cost a debugging session.

### Configuring it

There is no `--init gemini` template, so build the profile by hand. Get a key
from https://aistudio.google.com/apikey (free tier is enough):

```bash
celeste config -config gemini --set-url https://generativelanguage.googleapis.com/v1beta
celeste config -config gemini --set-key AIza...
celeste config -config gemini --set-model gemini-flash-latest
celeste -config gemini chat
```

Vertex is a different service: it authenticates with ADC or a service account
rather than a key, and needs a GCP project with billing.

```bash
gcloud auth application-default login
celeste config -config vertex --set-url https://aiplatform.googleapis.com/v1/projects/PROJECT_ID/locations/LOCATION
```

`celeste providers info gemini` prints the base URL and default model for any
provider if you need to check what is shipped.

**Use the `-latest` alias, not a pinned version.** The default is
`gemini-flash-latest`, which Google maintains. Google retired the whole Gemini
2.x line: `gemini-2.0-flash`, `gemini-2.0-flash-001`, `gemini-2.5-flash` and
`gemini-2.5-flash-lite` all answer *"This model is no longer available."* Pin a
version and you inherit Google's retirement schedule.

**The model listing is wrong.** `/v1/models` still returns `gemini-2.0-flash`
with `generateContent` in its `supportedGenerationMethods`. Call that model and
it fails. Resolving your default from the live listing buys you no more safety
than hardcoding it, so check that before you build on `ListModels`.

**AI Studio needs `v1beta`.** Google stopped serving current models on `v1`:

| API version | Model | Result |
|---|---|---|
| `v1` | `gemini-3.6-flash` | HTTP 404 |
| `v1beta` | `gemini-3.6-flash` | HTTP 200 |
| `v1beta` | `gemini-flash-latest` | HTTP 200 |

Celeste reads the version off the base URL: `v1beta` for AI Studio, `v1` for
Vertex. Vertex is a separate service and keeps its own convention.

### Agent mode and `thought_signature`

Gemini 3.x attaches an opaque `thoughtSignature` to the part carrying a function
call. Echo it back verbatim on the next turn or the request fails:

```
Error 400: Function call is missing a thought_signature
```

Celeste stores the signature on the message, so it survives checkpointing and
history replay. This landed in **v1.15.0**. On earlier versions your chat
sessions work and your agent runs die on the second turn, the moment the model
calls a tool.
