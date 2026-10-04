# LLM Providers

Celeste CLI supports **9 chat providers**: eight call tools, and Venice's tool calling depends on the model (checked against the live Venice catalog — some Venice models support tools, some don't). `celeste providers` lists 11, adding DigitalOcean (its tools run in its own cloud) and ElevenLabs (voice). All OpenAI-compatible for Celeste's 48 tools. Grok reigns with collections RAG.

| Provider | Tools | Collections | Notes |
|----------|-------|-------------|-------|
| **Grok/xAI** | ✅ | ✅ Native | 2M ctx, agent king
| **OpenAI** | ✅ | ❌ | Gold std
| **Anthropic** | ✅ Native | ❌ | Claude power
| **Gemini (Google)** | ✅ | ❌ | Multi-modal; needs v1.15.0+ for agent mode (see below)
| **Venice.ai** | ⚠️ Per model | ❌ | Uncensored opt; tool calling depends on the model (live catalog-checked)
| **Vertex AI** | ✅ | ❌ | GCP enterprise; default `gemini-2.0-flash` unverified
| **OpenRouter** | ✅ Model-dep | ❌ | Model bazaar
| **Sakana AI** | ✅ | ❌ | Fugu/Fugu Ultra, 1M ctx (the default)
| **Local** | ✅ | ❌ | mlx-vlm, Ollama, LM Studio, llama.cpp (see below)

**Which model runs:** celeste uses the model the provider serves now. It reads the provider's `/models` list at startup (OpenAI, Grok, Anthropic, OpenRouter, Sakana, Venice), caches it for 24 hours in `~/.celeste/cache/models`, and if your configured model has been retired it falls back to the provider's current default and says so (in the chat, or on stderr for `celeste agent`, `celeste message` and the `serve` log). The config file is not rewritten. A model is replaced only when celeste is sure it's gone: IDs match case-insensitively; OpenRouter presets (`@...`), fine-tunes (`ft:...`) and path-like IDs are never replaced; and for Anthropic, OpenAI and xAI a model missing from the list is checked with `GET /models/{id}` first (200 keeps it, 404 replaces it, anything else keeps it). A Grok fallback never picks grok-4.3 or the grok-4-1-* family. Turn resolution off with `"pin_model": true` or `CELESTE_PIN_MODEL=1`; `/set-model <name> --force` pins a model for the session until the next endpoint switch. Right after switching endpoints with no cached list, the first message may still go to the old profile's model until the list loads (a few seconds at most). Orchestrator lanes resolve on their own endpoints. Providers without a list (Gemini, Vertex, DigitalOcean, local) use the configured model as is. Registry defaults are offline fallbacks only.

**Setup:** `celeste config --set-url https://api.x.ai/v1 --set-key xai-...`

**Sakana/Fugu:** `celeste config --init sakana` then `celeste -config sakana config --set-url https://api.sakana.ai/v1 --set-key <key> --set-model fugu` (or `fugu-ultra`), then `celeste -config sakana chat`. OpenAI-compatible chat completions; reasoning effort is fixed server-side (default high).

**Anthropic:** `celeste -config anthropic config --set-url https://api.anthropic.com --set-key sk-ant-...`. Use the host without `/v1`: celeste adds `/v1/messages` for chat and `/v1/models` for the model list. A base URL that already ends in `/v1` (what earlier versions printed) still works.

**Anthropic prompt caching and `/effort`:** celeste caches the tools, the system prompt and the conversation, and a toggle of `/effort` changes nothing in them: only the request's thinking settings change. Anthropic counts those settings as part of the cache, though, so the first request after you change `/effort` (or turn it off or on) reads the conversation from scratch and writes it to the cache again. That is one re-write per change, then caching resumes. celeste itself never changes the thinking setting within a tool turn: on budget-thinking models (Sonnet 4.5, Opus 4.5, Haiku 4.5) a turn that started with thinking keeps it until the turn ends, and a turn that started without it stays without it. If you run `/effort` while a tool loop is running, turning thinking off (or, on adaptive models, changing the effort) applies to the next request in that turn; turning it on for a budget-thinking model waits for the next turn. To avoid the re-write, pick an effort for the session rather than switching back and forth.

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
celeste config -config local --set-model <whatever your server expects>   # REQUIRED
```

`--set-model` is not optional. A new profile inherits the default profile's
model, so skipping that line leaves your local server receiving a hosted
model's name and returning 404. Confirm before you start:

```bash
celeste config -config local     # Model: must be YOUR model, not fugu
```

`local` above is just a profile name; pick anything. There is no
`--init local` template, the profile is created by the first `--set-*`.

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
`fallback, model unknown, set --set-context-limit`. That last one means celeste
is guessing, so set it. The guess is 8192 on a local endpoint and 128k for a
hosted model missing from the table.

`--set-context-limit 0` clears the setting and returns to the model default.

### Which commands can use tools

| | tools |
|---|---|
| `celeste chat` (TUI) | 48 built-in, plus `spawn_agent` and `post_message` |
| `celeste agent` | 24 built-in (dev, git, web, code graph, memory, todo) |
| `celeste message`, and the `celeste "..."` shorthand | none |

Tools from MCP servers and custom skills come on top of the built-in counts.

`celeste message` sends no tools for **any** provider, local or hosted. It is a
one-shot chat command with no tool-execution loop. For non-interactive tool use,
`celeste agent` is that loop:

```bash
celeste -config local agent -auto-approve --goal "read README.md and summarise it"
```

Expect local inference to be slow enough that a turn feels stalled. A 27B model
at ~6 tok/s takes roughly half a minute per turn.

### Timeouts

The profile's `timeout` is a **stall timeout**: a request fails only when
nothing arrives from the server for that many seconds. A reply that keeps
streaming, including reasoning the model sends but celeste does not show
(qwen3's thinking on Ollama), can take as long as it needs. No single request
runs longer than 30 minutes, or three times the timeout if that is longer.

A local server sends nothing while it reads the prompt, and the first turn of
a chat is long: on a 14B model at a 32K window, ~16K prompt tokens plus
thinking took more than 300 s before the first byte. So a local endpoint
(`127.0.0.1`, `localhost`, a private or link-local address, a single-label or
`.local`/`.lan` host) whose timeout is unset or still the 60 s default gets
**600 s**. `config` shows the value in use:

```bash
celeste config -config local                       # Request Timeout: 600s without data (local default)
celeste config -config local --set-timeout 1200    # a slower machine or a bigger model
celeste config -config local --set-timeout 0       # back to the default
```

Hosted providers keep 60 s by default, so a dead connection still fails after
a minute of silence.

`celeste agent` uses the same timeout. `-request-timeout <seconds>` bounds each
whole model turn (without it, the 30-minute cap does); the client then also
waits at least that long for data.

If a turn fails before any reply, its message stays in the chat. Send the same
text again to retry it: the request carries it once, not twice.

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

That note covers AI Studio. Vertex still ships `gemini-2.0-flash` as its default,
unverified: Vertex runs its own model lifecycle, and checking it needs a billed
GCP project. `celeste providers` marks it `(unverified)`. If Vertex answers
`NOT_FOUND`, set `--set-model` to a current Gemini model.

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
