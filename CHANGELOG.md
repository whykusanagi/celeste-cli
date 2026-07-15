# Changelog

All notable changes to Celeste CLI will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.13.0](https://github.com/whykusanagi/celeste-cli/compare/v1.14.0...v1.13.0) (2026-07-15)


### Features

* /clear full reset, upscale_image tool call, NSFW cleanup (v1.5.3) ([9f213e6](https://github.com/whykusanagi/celeste-cli/commit/9f213e61ab0b5e94946e923f4a2a49eee25fd56d))
* /clear now sets NewSession flag for full session reset ([8209a8c](https://github.com/whykusanagi/celeste-cli/commit/8209a8ca1b834abc9b76382123c185c6e3f6767e))
* add agent artifacts and benchmark scaffolding ([72fbe20](https://github.com/whykusanagi/celeste-cli/commit/72fbe2048a3eef445339ac83f91de5513e92c5c6))
* add autonomous agent mode with checkpoints and eval harness ([a3d4ec9](https://github.com/whykusanagi/celeste-cli/commit/a3d4ec9be7106d2bf8694b40d24728637f640a15))
* add celeste index command + improve code_stubs accuracy ([983f9dc](https://github.com/whykusanagi/celeste-cli/commit/983f9dc6d8dd84f1f65b280390dc89d7cb4f0ed1))
* add claw runtime mode orchestration and safety caps ([b5c8395](https://github.com/whykusanagi/celeste-cli/commit/b5c83952f57fc128c8734bfb68ef847c14cae52b))
* add code_stubs built-in tool to find structurally incomplete code ([26cd206](https://github.com/whykusanagi/celeste-cli/commit/26cd206d79b72890f6ed3b1d679314b3c2867cbd))
* add git snapshot and git_status/git_log tools ([87d598e](https://github.com/whykusanagi/celeste-cli/commit/87d598e6a5a813cd64763c4afab2c90e900a2c43))
* add MCP server mode and subagent spawning (Phase D) ([f285a21](https://github.com/whykusanagi/celeste-cli/commit/f285a215316dcdeac2223a03132ac658ac850713))
* add native Anthropic Messages API backend ([7b1e177](https://github.com/whykusanagi/celeste-cli/commit/7b1e177d3fe53ee15b15998dfbfc4f39e2781d4c))
* add OpenAI o3, o4-mini, gpt-4.1-nano models + pricing ([9ab7822](https://github.com/whykusanagi/celeste-cli/commit/9ab78222c1f00fa5605ecba8372c19aa93ef1487))
* add plan mode, todo tool, and memory system (Phase C, Tasks 10-19) ([7dc2465](https://github.com/whykusanagi/celeste-cli/commit/7dc2465794d410a08ada982cc21658cbf306823d))
* add plan-execute-verify orchestration for agent mode ([fbb33c8](https://github.com/whykusanagi/celeste-cli/commit/fbb33c84497b113413d69e8bf4943a5fc11e5467))
* add Sakana AI (Fugu) provider ([7de3e35](https://github.com/whykusanagi/celeste-cli/commit/7de3e35520d0cbe30508b6cb6c5c6a96edc77e43))
* add Sakana AI (Fugu) provider ([a859910](https://github.com/whykusanagi/celeste-cli/commit/a85991005aae1bd10ccb04d47eb76ee4a1de5a87))
* add upscale_image as always-available tool call skill ([adc04e6](https://github.com/whykusanagi/celeste-cli/commit/adc04e6cf4ce8234e2d52559c137c16845e493fe))
* add Venice models from /api/v1/models API ([b17418f](https://github.com/whykusanagi/celeste-cli/commit/b17418f968128b55c36dc20b7508e9d631478a54))
* add Venice-unique model pricing from docs.venice.ai ([5ec2160](https://github.com/whykusanagi/celeste-cli/commit/5ec21608d501c1b5cecb9e0d0ceb5d7820018a1c))
* add web tools, cost tracking, and hooks (Phase C, Tasks 1-9) ([f4d1eea](https://github.com/whykusanagi/celeste-cli/commit/f4d1eeaa118d3c594490667f6f792a021dfa1245))
* auto-init .grimoire and project memory on first visit ([65de83b](https://github.com/whykusanagi/celeste-cli/commit/65de83bd33f20ba6c6089b83def4a09303a9395c))
* celeste phase 1 — autonomous agent mode v1.6.0 ([8e220fc](https://github.com/whykusanagi/celeste-cli/commit/8e220fc10dc4ec81da538099084cc4ff72046648))
* celeste phase 1 — autonomous agent mode v1.6.0 ([8e220fc](https://github.com/whykusanagi/celeste-cli/commit/8e220fc10dc4ec81da538099084cc4ff72046648))
* **checkpoints:** add file tracker, snapshot manager, and diff computation (Tasks 10-13) ([5da845c](https://github.com/whykusanagi/celeste-cli/commit/5da845cb1f01f64049ee158b67ab9c9572b277bb))
* **codegraph:** add code graph index with semantic search (Phase B, Tasks 1-8) ([d49bf24](https://github.com/whykusanagi/celeste-cli/commit/d49bf244926191b6a8c4bd75b23d116764047cd2))
* **codegraph:** add reasoning metadata to SearchResult + code_search tool ([a53f3f2](https://github.com/whykusanagi/celeste-cli/commit/a53f3f2c5963e0d74a0f4122fb5ab0e896d46034))
* **codegraph:** BM25 as additive signal fused with Jaccard via RRF ([ce8e4dd](https://github.com/whykusanagi/celeste-cli/commit/ce8e4dd5737c2e7db16e95077587a5b950096748))
* **codegraph:** embed stopwords.json and apply at runtime (v1.0.0 + patch) ([257c283](https://github.com/whykusanagi/celeste-cli/commit/257c283bb597750a156dd19a203491bc855876c7))
* **codegraph:** emit call edges for decorator [@syntax](https://github.com/syntax) ([#44](https://github.com/whykusanagi/celeste-cli/issues/44)) ([d972250](https://github.com/whykusanagi/celeste-cli/commit/d97225069fb1487b1c187b48516830553e5a2644))
* **codegraph:** link attribute assignments to property setters ([#45](https://github.com/whykusanagi/celeste-cli/issues/45)) ([31fe33b](https://github.com/whykusanagi/celeste-cli/commit/31fe33b5f4e1b37e7ea0ec29f9ab8dec92c4e570))
* **codegraph:** path-based post-ranking filter for SemanticSearch ([c2ab38f](https://github.com/whykusanagi/celeste-cli/commit/c2ab38f131467453565413c283ad2a97207b935f))
* **codegraph:** persist MinHasher seeds across process boundaries ([8ce1ace](https://github.com/whykusanagi/celeste-cli/commit/8ce1acea9092024c6505ca01c9c59cdc5336749e))
* **codegraph:** persisted LSH band table for sub-linear semantic search ([3615529](https://github.com/whykusanagi/celeste-cli/commit/361552998b39589e9e074122bd0d3f53f619d2b6))
* **codegraph:** Protocol/ABC/[@abstractmethod](https://github.com/abstractmethod) awareness for STUB detection ([#43](https://github.com/whykusanagi/celeste-cli/issues/43)) ([e318461](https://github.com/whykusanagi/celeste-cli/commit/e3184613f1509e71da7066ba840868d0d92b7c29))
* **codegraph:** structural feature rerank layer ([5248e29](https://github.com/whykusanagi/celeste-cli/commit/5248e29ae6e54657232a1bdd321437d058004b21))
* **codegraph:** tree-sitter TypeScript parser (TS/TSX) ([734098c](https://github.com/whykusanagi/celeste-cli/commit/734098c492c10de13466c5cd71b7839e8c318c8b))
* complete Plan 1 — unified tool layer replacing skills package ([b209013](https://github.com/whykusanagi/celeste-cli/commit/b209013db93f706e0b6ef5b2ae0306cd552a1634))
* **config:** auto-migrate deprecated models + fill empty on load ([#51](https://github.com/whykusanagi/celeste-cli/issues/51)) ([b0ac8cb](https://github.com/whykusanagi/celeste-cli/commit/b0ac8cba6d421513ecc260ce96ac2b10f51bc553))
* **config:** default to grok-build-0.1 Grok code model ([#51](https://github.com/whykusanagi/celeste-cli/issues/51)) ([baa5c39](https://github.com/whykusanagi/celeste-cli/commit/baa5c39ee12395df7b94df3fca1e20847c2b938f))
* **config:** resolve default profile from a file flag, not hardcoded provider ([#92](https://github.com/whykusanagi/celeste-cli/issues/92)) ([4c9ca8f](https://github.com/whykusanagi/celeste-cli/commit/4c9ca8fd1a7ee9571cef3ee278cb86bd6c841eb6))
* **context:** add TokenBudget, tool capping, compaction engine, and summarizer ([d76167c](https://github.com/whykusanagi/celeste-cli/commit/d76167c96c8278ce7a5ea43050d1feabd67d6791))
* element-named subagents with concurrent dispatch ([0e92b8c](https://github.com/whykusanagi/celeste-cli/commit/0e92b8cf532ebc195a49978d5b2c42e67672f827))
* element-themed subagent animations in tool progress ([981fdc3](https://github.com/whykusanagi/celeste-cli/commit/981fdc31e70d95d6dda3ebef9ac949b8f3d4af75))
* enable spawn_agent in chat mode + persona override parameter ([d5f7d52](https://github.com/whykusanagi/celeste-cli/commit/d5f7d529b7ec8c363f6e10472ce82c6b0bccafb1))
* graceful Ctrl+C interrupt handling and grimoire/git integration wiring (Tasks 14-17) ([ae4a4b9](https://github.com/whykusanagi/celeste-cli/commit/ae4a4b96c088cfedaaaf3f3ab7d3b2e446da0080))
* **grimoire:** add .grimoire project context discovery, parsing, and merging ([78329e3](https://github.com/whykusanagi/celeste-cli/commit/78329e3dbf215cab28a38a084b9df6cc5215e322))
* harden tool-calling flow and validate custom skill schemas ([8cec204](https://github.com/whykusanagi/celeste-cli/commit/8cec20481fabb88dc29297a889b12a354bff24a8))
* harden tool-calling flow and validate custom skill schemas ([018916f](https://github.com/whykusanagi/celeste-cli/commit/018916f0a8f203deba3cee19f1a4b943cf4d1c05))
* implement phase 1 ordering, skills panel, and docs sync ([e57e0b5](https://github.com/whykusanagi/celeste-cli/commit/e57e0b52af05dc2535dfa4e264fea5d655d96f1d))
* increase all limits for 2M token context windows ([8c04cec](https://github.com/whykusanagi/celeste-cli/commit/8c04cec4712a3118655176c77986efb2cd9ff9d6))
* live agent progress + multi-model orchestrator with split-panel TUI ([#7](https://github.com/whykusanagi/celeste-cli/issues/7)) ([a4e2bbf](https://github.com/whykusanagi/celeste-cli/commit/a4e2bbf3e044382e4ef8e1ea7cc070d04e50c764))
* **llm:** add ArgsError signal to ToolCallResult ([ef64b63](https://github.com/whykusanagi/celeste-cli/commit/ef64b639439f46de9997df7b61ca983d1d315e69))
* **llm:** add native Anthropic Messages API backend with prompt caching ([a97da34](https://github.com/whykusanagi/celeste-cli/commit/a97da34c8f76b9366ea41083e3df14bf59ecc8bb))
* **llm:** add SendMessageStreamEvents to all backends ([77d4569](https://github.com/whykusanagi/celeste-cli/commit/77d456950b3ece14b891b3f91f4305da20cb937e))
* **llm:** add validateToolArgs helper for tool-call JSON validation ([09e565b](https://github.com/whykusanagi/celeste-cli/commit/09e565bb06f5befe0856d62ec5f858f13269e1ed))
* **llm:** retry transient errors at the Client delegation layer ([#29](https://github.com/whykusanagi/celeste-cli/issues/29)) ([589037f](https://github.com/whykusanagi/celeste-cli/commit/589037f2d718a26d43aeabb92d4ba2c04bf178a2))
* **llm:** transient-error classifier, backoff, withRetry ([#29](https://github.com/whykusanagi/celeste-cli/issues/29)) ([eb31a19](https://github.com/whykusanagi/celeste-cli/commit/eb31a19e17fc51d30225b378751345d9d296f6c7))
* MCP connectivity + TUI polish (mcp install, ask tool, /mcp panel, HTTP transport) ([#101](https://github.com/whykusanagi/celeste-cli/issues/101)) ([b37b7cd](https://github.com/whykusanagi/celeste-cli/commit/b37b7cd44fb2076f5954bab92692bbc1ac16e7f3))
* **mcp:** add Manager lifecycle and wire into startup ([5f9be1e](https://github.com/whykusanagi/celeste-cli/commit/5f9be1e25d6fb50f0ed669c019a824c3f3b7aee4))
* **mcp:** add MCP client with stdio/SSE transports and tool discovery ([5701218](https://github.com/whykusanagi/celeste-cli/commit/5701218fdd27d0c146b9d5482cfb1165409b02cd))
* **model-router:** agent_model config + per-mode routing + tool-capability guardrail (task e8775b91) ([7afeb8e](https://github.com/whykusanagi/celeste-cli/commit/7afeb8ec73cc51a252b1c4835c16ce1e00314778))
* parallel dispatch for concurrency-safe tools in TUI batch handler ([851d470](https://github.com/whykusanagi/celeste-cli/commit/851d47010d8fbf9c573ab4b806b5881327b7c9a9))
* **permissions:** add multi-layer permission system ([a6c0e36](https://github.com/whykusanagi/celeste-cli/commit/a6c0e36e9d2f0890267a6b60e8c30fdae1185766))
* **providers:** harden OpenRouter tool-capability via live /models catalog ([f7dddb1](https://github.com/whykusanagi/celeste-cli/commit/f7dddb18d5c32557c876da208de0bfb0ad024efd))
* **providers:** harden Venice tool-capability via live /models catalog ([412b1db](https://github.com/whykusanagi/celeste-cli/commit/412b1dbd063ef59a979e9432da6a40101da40e55))
* re-enable persona in agent mode (now safe after v1.1 fix) ([5b9e14e](https://github.com/whykusanagi/celeste-cli/commit/5b9e14ed84ef7781f82535e92f1b352feb01f89c))
* remove upscale case from TUI media handler and help text ([e6c9159](https://github.com/whykusanagi/celeste-cli/commit/e6c9159c7697133f2a3a0d1f5a5ec702c344ac74))
* remove upscale: media command from NSFW parsing ([0b5fa7c](https://github.com/whykusanagi/celeste-cli/commit/0b5fa7c9eb5dda154deeb654a8ad1ee6bcc4f5d8))
* **server:** expose codegraph tools as first-class MCP tools ([55216e4](https://github.com/whykusanagi/celeste-cli/commit/55216e484128bdd06776c20592080ef328d1448e))
* **server:** repetition guard in message/chat loop (mirror of TUI guard) ([#48](https://github.com/whykusanagi/celeste-cli/issues/48) follow-up) ([9705b7f](https://github.com/whykusanagi/celeste-cli/commit/9705b7f0ae36f23791ff5b581862fc514036b3d7))
* **sessions:** add JSONL session persistence package (Tasks 7-9) ([50cd25e](https://github.com/whykusanagi/celeste-cli/commit/50cd25e68debeb61f4d80ad4d03850d473248be1))
* subagent TUI visibility + stagger delay + nested progress ([34db1a7](https://github.com/whykusanagi/celeste-cli/commit/34db1a722b98af026ca6dc764a0819e648f8cbfa))
* **subagents:** /agents kill &lt;id&gt; to cancel a specific in-flight subagent (task 6ffb5a7c) ([42fed8b](https://github.com/whykusanagi/celeste-cli/commit/42fed8bbd72c59d24aca28535ec5bdca1f3f6fea))
* **subagents:** auto-approve tools in subagents (Trust mode) so they can do real work ([d58cb39](https://github.com/whykusanagi/celeste-cli/commit/d58cb39614ec6e6c12d1815b5a73f3ec6ea658a0))
* **subagents:** background subagents with auto-transition ([#30](https://github.com/whykusanagi/celeste-cli/issues/30)) ([a0ed500](https://github.com/whykusanagi/celeste-cli/commit/a0ed500214810a6039d4abf6f3a0a09c75243eda))
* **subagents:** expose isolate_worktree + background_after on spawn_agent ([#30](https://github.com/whykusanagi/celeste-cli/issues/30), [#32](https://github.com/whykusanagi/celeste-cli/issues/32)) ([70a1ded](https://github.com/whykusanagi/celeste-cli/commit/70a1ded5936fe0f20dcf74c189e983950f0a6800))
* **subagents:** git worktree add/remove/merge helper ([#32](https://github.com/whykusanagi/celeste-cli/issues/32)) ([2a761fc](https://github.com/whykusanagi/celeste-cli/commit/2a761fc55341b84550078e6cdda45c55c6ac9cca))
* **subagents:** inter-agent mailbox messaging + post_message tool ([#31](https://github.com/whykusanagi/celeste-cli/issues/31)) ([49fe8c2](https://github.com/whykusanagi/celeste-cli/commit/49fe8c202d32f329f5c6837f1752e5af2f1f872d))
* **subagents:** opt-in worktree isolation per subagent ([#32](https://github.com/whykusanagi/celeste-cli/issues/32)) ([19c0140](https://github.com/whykusanagi/celeste-cli/commit/19c01400845f880c89a8316392c5cc4f0f2c5567))
* **subagents:** persist + resume subagent checkpoints ([#33](https://github.com/whykusanagi/celeste-cli/issues/33)) ([1180b69](https://github.com/whykusanagi/celeste-cli/commit/1180b69d103c74498a67eb503401803f1fb7268a))
* **tools:** add code_review tool for pattern-based code smell detection ([5583926](https://github.com/whykusanagi/celeste-cli/commit/5583926dec23ed7c853c24139d205891b234326e))
* **tools:** add core Tool interface, BaseTool helper, and Registry ([2f0fb0c](https://github.com/whykusanagi/celeste-cli/commit/2f0fb0ce29251d0d6a8f28323b7b7c5477c0cce8))
* **tools:** add StreamEvent types and StreamingToolExecutor ([2e17e9f](https://github.com/whykusanagi/celeste-cli/commit/2e17e9ff8792430d4590ab1b202651b4f7417477))
* **tools:** large-file byte-path fold-in — read cap, retry trim, splice_file ([#103](https://github.com/whykusanagi/celeste-cli/issues/103)) ([41f74d9](https://github.com/whykusanagi/celeste-cli/commit/41f74d930abdf51b2f807d8431f6e265a54e44d7))
* **tools:** migrate all 23 skills to unified tool interface ([2af5102](https://github.com/whykusanagi/celeste-cli/commit/2af51022833d4c99cdad0d85de3bed16c079b93e))
* **tools:** migrate dev tools to unified tool interface ([3394ed5](https://github.com/whykusanagi/celeste-cli/commit/3394ed58e7764f4c98c5feef955fded4e7df8225))
* TUI handles NewSession flag to start fresh session on /clear ([6da1e19](https://github.com/whykusanagi/celeste-cli/commit/6da1e199c4b970bc6f1cff58e0b72709dab1d065))
* **tui:** add /config set-key, set-model, set-url subcommands ([5163ee5](https://github.com/whykusanagi/celeste-cli/commit/5163ee56e4b852ad08e38cd565e1329feb4109d1))
* **tui:** add /index rebuild and /index update subcommands ([94c338c](https://github.com/whykusanagi/celeste-cli/commit/94c338c396ec01499e5cb2a3d20651525c7bc47a))
* **tui:** add descriptors to persona panel sliders ([9b236d0](https://github.com/whykusanagi/celeste-cli/commit/9b236d0c9faa87ddd984685ac8d892ead72623da))
* **tui:** add tool progress, context bar, permission prompt, and MCP panel components ([444e70d](https://github.com/whykusanagi/celeste-cli/commit/444e70db8d0c67d3612f76277e1d0bde22d3c2e2))
* **tui:** embed canonical corrupted-theme color palette (task 7aa133c9) ([dcc772f](https://github.com/whykusanagi/celeste-cli/commit/dcc772fc082656a74695c30e520f5c03b47a49c9))
* **tui:** hard permission gate via modal confirmation ([#34](https://github.com/whykusanagi/celeste-cli/issues/34)) ([213d640](https://github.com/whykusanagi/celeste-cli/commit/213d640324e1438d2a58f8c2f550a2e59d5f113d))
* **tui:** personality sliders via /persona command ([23e7b45](https://github.com/whykusanagi/celeste-cli/commit/23e7b45b1289989d216de652050fdc77582b85e4))
* **tui:** repetition guard — stop a stuck single-tool loop early ([#48](https://github.com/whykusanagi/celeste-cli/issues/48) follow-up) ([7b0a84f](https://github.com/whykusanagi/celeste-cli/commit/7b0a84ffd833865f208963290d74e5824933048c))
* **tui:** surface subagent id + kill hint in /agents output (#d15ac448) ([eb83691](https://github.com/whykusanagi/celeste-cli/commit/eb836913b4892e9f1beca030dc98f5f1ba00e149))
* **tui:** wire tool progress display — show what tools are executing ([649e1c0](https://github.com/whykusanagi/celeste-cli/commit/649e1c0617fff6d9addeb64d1faf3fb6a955c188))
* **tui:** wire tool progress, context bar, permission prompt, and MCP panel into AppModel ([2c6c420](https://github.com/whykusanagi/celeste-cli/commit/2c6c420b976bf4e476f922b537cb82ae080d9dc7))
* update all hardcoded models, pricing, and defaults ([5863b46](https://github.com/whykusanagi/celeste-cli/commit/5863b46986aa3f76af8f597328875161ab173e47))
* update Anthropic models to current (claude-opus-4-6, sonnet-4-6, haiku-4-5) ([3b7b2f0](https://github.com/whykusanagi/celeste-cli/commit/3b7b2f0cf3e7636fa17b85ac90bc938df4c2938b))
* update Grok model limits to 2M tokens + add new model pricing ([f7ee79d](https://github.com/whykusanagi/celeste-cli/commit/f7ee79d599fdbe462e799d68dc362a54c36f2a2b))
* v1.7.0 — unified tools, streaming executor, permissions, MCP, context management ([9d64f5c](https://github.com/whykusanagi/celeste-cli/commit/9d64f5cfd964f8d1ac5215874f1ce52b9126fc83))
* v1.8.0 — project intelligence, MCP server, semantic search, memory ([c299e39](https://github.com/whykusanagi/celeste-cli/commit/c299e39ebc18067207ee6a161988aeb5d48f179a))
* v1.8.3 — graph code review, real streaming, collections search, MCP improvements ([146f180](https://github.com/whykusanagi/celeste-cli/commit/146f1800f7437332a300569e5dc806df2b5c6080))
* v1.8.4 — docs writing skill, version bump ([e5b9193](https://github.com/whykusanagi/celeste-cli/commit/e5b9193c2ded2151ff3910241237b0bd8e4d673a))
* v1.9.3 — subagent DAG orchestration, multi-language code graph, audio production pipeline ([46b9078](https://github.com/whykusanagi/celeste-cli/commit/46b9078c8c9560055c8dbde0bcc4b769eb134b52))
* wire /agent slash command into TUI ([892f0d0](https://github.com/whykusanagi/celeste-cli/commit/892f0d0345d1d9443dd36e5b59cee6504494eddf))
* wire /plan, /diff, /undo, /memories slash commands into TUI ([1896a9b](https://github.com/whykusanagi/celeste-cli/commit/1896a9b3671f9f75001039097e4424074b874871))
* wire 7 missing CLI commands into dispatcher ([c63fde9](https://github.com/whykusanagi/celeste-cli/commit/c63fde9cfac3730f2de847e9f3909e76ce3bf88f))
* wire code graph into startup and register tools ([641483e](https://github.com/whykusanagi/celeste-cli/commit/641483e362726207df939758ba6e4269888b34b1))
* wire image forwarding to LLM via OpenAI multimodal content blocks ([4cc534d](https://github.com/whykusanagi/celeste-cli/commit/4cc534dba2e5e8a992834aa63f62c7c2c0ab5ce5))
* wire permission checker into tool registry and startup ([c699bf0](https://github.com/whykusanagi/celeste-cli/commit/c699bf0b03e47a5b49f1b669de42efebaf4455b1))
* wire streaming events into TUI and agent runtime ([268aff1](https://github.com/whykusanagi/celeste-cli/commit/268aff1feadcfbc8f79f3e5bf08dfef1a106fffc))


### Bug Fixes

* /index dependency tree visualization + file-level graph query ([3895070](https://github.com/whykusanagi/celeste-cli/commit/3895070428749d719e1fdbd3ea801d9e7a864491))
* add call edge extraction for generic parser + auto-init grimoire in MCP mode ([8a4aeac](https://github.com/whykusanagi/celeste-cli/commit/8a4aeac5fe59eda11451dcdf33ea2fd062864655))
* Add Dockerfile.test and unignore it in .gitignore ([3b97123](https://github.com/whykusanagi/celeste-cli/commit/3b9712326b0f721f5c95498e065691125aadd9ed))
* add git to Docker test runner (fixes 2 test failures) ([5a19c72](https://github.com/whykusanagi/celeste-cli/commit/5a19c72257f842c358f05ff4f796da1f97a15b72))
* add missing refusal patterns for content policy test ([a0f8be6](https://github.com/whykusanagi/celeste-cli/commit/a0f8be6ed776b3d01ca21968eace5a132a115b5a))
* address code audit findings — custom tools, hook errors, permission logging ([7168df3](https://github.com/whykusanagi/celeste-cli/commit/7168df3c1e1f60356f5e08957737e4630a717589))
* address code review issues before merge to main ([31fe44a](https://github.com/whykusanagi/celeste-cli/commit/31fe44a35eb8e743a6f0e1cdcb6ba1326cd72fad))
* address Fugu pre-release review ([7b9c742](https://github.com/whykusanagi/celeste-cli/commit/7b9c7423863f8e6e10688f0717253f031193d83e))
* agent tool calling — 4 root causes + text fallback ([e17c657](https://github.com/whykusanagi/celeste-cli/commit/e17c6570a393b8c9ae9ab7047141897d4d2cd1e2))
* agent tool calling — system prompt, text fallback, xAI tool cleanup ([07e49ec](https://github.com/whykusanagi/celeste-cli/commit/07e49eca4e23d90c49f2bb2cae01b164003b4236))
* **agent:** cap consecutive invalid-args turns + surface ArgsError ([#48](https://github.com/whykusanagi/celeste-cli/issues/48)) ([6fef4fd](https://github.com/whykusanagi/celeste-cli/commit/6fef4fdbf3ce6e1a12ecab757e8d3572a879a57c))
* **agent:** enforce tool timeout + honor ctx between turns (task 349f1f14) ([fad9686](https://github.com/whykusanagi/celeste-cli/commit/fad9686983552840cda7d26d4f141c1a20534311))
* always start fresh. Users can explicitly resume via `celeste resume`. ([f717af3](https://github.com/whykusanagi/celeste-cli/commit/f717af30e529497de4258c4b78ca100f0f17199b))
* block chat-mode hallucinated 'Audio saved' claims ([#48](https://github.com/whykusanagi/celeste-cli/issues/48)) ([538b43a](https://github.com/whykusanagi/celeste-cli/commit/538b43afb0f517936ffa90e41552fd12378d2a76))
* block sudo/su + OS/env detection in agent system prompt ([#6](https://github.com/whykusanagi/celeste-cli/issues/6)) ([e62ca5f](https://github.com/whykusanagi/celeste-cli/commit/e62ca5f15bd2fe23c0d9e8eb7b88557eda025060))
* build golangci-lint from source with system Go 1.26.1 ([b29591f](https://github.com/whykusanagi/celeste-cli/commit/b29591fc11f2867bcb0c0cfe5f5a9802f74d74c2))
* cap tool calls before recording them so declared calls match results ([c4d5373](https://github.com/whykusanagi/celeste-cli/commit/c4d537376e4ef2cdba0ae193ca149d1936ac9730))
* **ci:** build-tag TS parser so CGo-disabled cross-builds fall back to regex ([702f950](https://github.com/whykusanagi/celeste-cli/commit/702f9503c995086e5494925461c20778a4f49516))
* **ci:** bump Go to 1.26.2 to clear govulncheck stdlib CVEs ([c62b8ad](https://github.com/whykusanagi/celeste-cli/commit/c62b8ad0e176b61909360e206c09627b59782112))
* **ci:** gofmt v1.9.0 additions and enable CGo in Docker builder ([f65e00f](https://github.com/whykusanagi/celeste-cli/commit/f65e00ff28b7f635e250243d2069b09e0920e1f6))
* **ci:** version tests read serverVersion constant; stop double-running CI per branch ([#71](https://github.com/whykusanagi/celeste-cli/issues/71)) ([ab8c2a4](https://github.com/whykusanagi/celeste-cli/commit/ab8c2a4038a071436d0cbde058a17215b4fdce9b))
* codegraph indexer skips vendor dirs, respects .gitignore, and stores index outside project ([2e10ea3](https://github.com/whykusanagi/celeste-cli/commit/2e10ea36fa0999be77c57c89c1ead3b6dba22787))
* **codegraph:** honor context cancellation in search/build/update (task 349f1f14) ([72a0823](https://github.com/whykusanagi/celeste-cli/commit/72a08235d1dcf62cf634fd7197c3ec9dfbc7703d))
* **codegraph:** LSH fallthrough to brute-force on empty candidate set ([0c6099f](https://github.com/whykusanagi/celeste-cli/commit/0c6099f0227ea57662c82bdebc9d0be99e5f0819))
* **codegraph:** match top-level test dirs + pytest conventions in include_tests ([#46](https://github.com/whykusanagi/celeste-cli/issues/46)) ([af087ec](https://github.com/whykusanagi/celeste-cli/commit/af087ec7465c54aeb6e8982fa0a8dd8587dc2ae4))
* **codegraph:** resolve cross-file and method call edges ([2e06795](https://github.com/whykusanagi/celeste-cli/commit/2e067959c71eb4b919f1751d5d690d6b5cb1345b))
* **codegraph:** splitCamelCase requires 3+ consecutive uppercase for acronyms ([f40c8f0](https://github.com/whykusanagi/celeste-cli/commit/f40c8f0bf797210ddd8fc9012428e06e713b307c))
* **codegraph:** suppress dunder methods from STUB detection ([#42](https://github.com/whykusanagi/celeste-cli/issues/42)) ([4e041f9](https://github.com/whykusanagi/celeste-cli/commit/4e041f97875c0837f4ed7d9b1d946ecc8ece2ae7))
* **codegraph:** two-pass Build() fixes cross-file caller-count ([#47](https://github.com/whykusanagi/celeste-cli/issues/47)) ([1a9515f](https://github.com/whykusanagi/celeste-cli/commit/1a9515f492053f62eba6f69e01b579df9804897e))
* complete /graph feature — detail view, search, edges, lint fixes ([3ae8599](https://github.com/whykusanagi/celeste-cli/commit/3ae8599377af438b22554682fa083fa1c09d0f78))
* complete OpenAI model catalog from API + retire old models ([2290b15](https://github.com/whykusanagi/celeste-cli/commit/2290b1530903a89cd2476d969c91abc869a7ca4b))
* **config:** clamp stale context_limit that exceeds the model window ([#51](https://github.com/whykusanagi/celeste-cli/issues/51)) ([5c19cc0](https://github.com/whykusanagi/celeste-cli/commit/5c19cc04f5d1fe6ca598730b431a9772019fe3e4))
* **config:** grok-build-0.1 context window is 256K ([#51](https://github.com/whykusanagi/celeste-cli/issues/51)) ([a6a6695](https://github.com/whykusanagi/celeste-cli/commit/a6a6695611d3b19779fc6bf515c1e48b76586d33))
* Correct repo URLs in release.yml from celesteCLI to celeste-cli ([03670bd](https://github.com/whykusanagi/celeste-cli/commit/03670bd0a2777b8df8b8acae2fb1f50e7f00483c))
* **costs:** grok-build-0.1 real pricing ($1.00 in / $2.00 out per 1M) ([#51](https://github.com/whykusanagi/celeste-cli/issues/51)) ([53c7e33](https://github.com/whykusanagi/celeste-cli/commit/53c7e33e1e306c6ad379fa38b405e97383742db5))
* deduplicate persona rules, remove hardcoded project refs ([821404c](https://github.com/whykusanagi/celeste-cli/commit/821404c344cc17acc1e647b59f5ba444b871d63f))
* **deps:** bump github.com/anthropics/anthropic-sdk-go ([#88](https://github.com/whykusanagi/celeste-cli/issues/88)) ([a38dc35](https://github.com/whykusanagi/celeste-cli/commit/a38dc356205552c397102d6b32e0b0f36392d102))
* **deps:** bump github.com/anthropics/anthropic-sdk-go ([#94](https://github.com/whykusanagi/celeste-cli/issues/94)) ([412302f](https://github.com/whykusanagi/celeste-cli/commit/412302f5e35a0980419d23e0540bff164ae57696))
* **deps:** bump github.com/charmbracelet/bubbles from 0.21.0 to 1.0.0 ([#69](https://github.com/whykusanagi/celeste-cli/issues/69)) ([ae00976](https://github.com/whykusanagi/celeste-cli/commit/ae009764174b8c35c95f9766b791401f3bd5903b))
* **deps:** bump github.com/ethereum/go-ethereum from 1.17.0 to 1.17.4 ([#73](https://github.com/whykusanagi/celeste-cli/issues/73)) ([46a7ab7](https://github.com/whykusanagi/celeste-cli/commit/46a7ab7b9c5061bd9cc7c5a1f4e63904d8f9a7c7))
* **deps:** bump github.com/ipfs/go-cid from 0.6.0 to 0.6.1 ([#63](https://github.com/whykusanagi/celeste-cli/issues/63)) ([df46df3](https://github.com/whykusanagi/celeste-cli/commit/df46df3e5c1ae1621d219032c396f37d53d3e0f7))
* **deps:** bump github.com/ipfs/go-cid from 0.6.1 to 0.6.2 ([#97](https://github.com/whykusanagi/celeste-cli/issues/97)) ([a80e4f7](https://github.com/whykusanagi/celeste-cli/commit/a80e4f7a8b577087f9f8b7a984ef97725d53f206))
* **deps:** bump github.com/tree-sitter/tree-sitter-php ([#89](https://github.com/whykusanagi/celeste-cli/issues/89)) ([c65921d](https://github.com/whykusanagi/celeste-cli/commit/c65921d59d20998b7d1ceaadf06ea2cae53710d7))
* **deps:** bump github.com/tree-sitter/tree-sitter-python ([#67](https://github.com/whykusanagi/celeste-cli/issues/67)) ([3eb4644](https://github.com/whykusanagi/celeste-cli/commit/3eb4644733546439368ba84317317a1d3d9b40b2))
* **deps:** bump github.com/tree-sitter/tree-sitter-rust ([#72](https://github.com/whykusanagi/celeste-cli/issues/72)) ([6515c94](https://github.com/whykusanagi/celeste-cli/commit/6515c942dc9c575f345a081d4a26498b6ed73508))
* **deps:** bump golang.org/x/text ([#95](https://github.com/whykusanagi/celeste-cli/issues/95)) ([0ed76a7](https://github.com/whykusanagi/celeste-cli/commit/0ed76a7882fc9f3de235fe94123b2720ac416f25))
* **deps:** bump google.golang.org/genai from 1.39.0 to 1.62.0 ([#90](https://github.com/whykusanagi/celeste-cli/issues/90)) ([0302c87](https://github.com/whykusanagi/celeste-cli/commit/0302c87b1cee26bdd8e412d230c6d411e5e4f1ea))
* **deps:** bump google.golang.org/genai from 1.62.0 to 1.63.0 ([#96](https://github.com/whykusanagi/celeste-cli/issues/96)) ([5d45139](https://github.com/whykusanagi/celeste-cli/commit/5d4513970cb9f145b829cdd0e0f489f72d49c4e9))
* **deps:** bump modernc.org/sqlite from 1.48.1 to 1.53.0 ([#65](https://github.com/whykusanagi/celeste-cli/issues/65)) ([6505021](https://github.com/whykusanagi/celeste-cli/commit/65050215cc2596d4479062f11c51fe25ff18b813))
* **deps:** bump the golang-x group across 1 directory with 2 updates ([#74](https://github.com/whykusanagi/celeste-cli/issues/74)) ([3a1475c](https://github.com/whykusanagi/celeste-cli/commit/3a1475c887072e30a474e4f31b6bb686bf3461be))
* **deps:** upgrade anthropic-sdk-go to v1.51.1; pin govulncheck past its generics panic ([#80](https://github.com/whykusanagi/celeste-cli/issues/80)) ([57c907e](https://github.com/whykusanagi/celeste-cli/commit/57c907e4dfa498c30b1ce8a9fd80633cffc9a89d))
* enforce agent tool usage and add surgical file editing ([#4](https://github.com/whykusanagi/celeste-cli/issues/4)) ([9a91596](https://github.com/whykusanagi/celeste-cli/commit/9a915962aea5fb52830f778251338b38d12ed4c6))
* forward image data from tool results to LLM backends ([81e0527](https://github.com/whykusanagi/celeste-cli/commit/81e0527c1ddca7babcbb7ee9223ff43f970e9e4f))
* gofmt backend_xai.go ([9740b56](https://github.com/whykusanagi/celeste-cli/commit/9740b5610d1dcf081dd94d39b48ed28f2781c4f6))
* gofmt formatting across all v1.8 files ([7e6be5a](https://github.com/whykusanagi/celeste-cli/commit/7e6be5a4d9e07b21a146f4887e9efa06694866e6))
* gofmt graph.go ([6c9bb1a](https://github.com/whykusanagi/celeste-cli/commit/6c9bb1af6b3bb4c1271d6bc47abc94f2ebcf2a1f))
* **grok:** correct pricing to $1.25/$2.50 + context to 1M from authoritative xAI catalog ([a561c66](https://github.com/whykusanagi/celeste-cli/commit/a561c666a05eb7390cea5eb6afa1cef83611d404))
* handle errcheck lint warnings for grimoire init and memory save ([3a0e438](https://github.com/whykusanagi/celeste-cli/commit/3a0e43818c5c25249e6ef7086e993c902de61e90))
* honor -config in read commands; /model lists real provider models in TUI ([#82](https://github.com/whykusanagi/celeste-cli/issues/82)) ([cb7e4df](https://github.com/whykusanagi/celeste-cli/commit/cb7e4df9fa6ebc3467aea7f9666900f2a1432b3b))
* implement revert command + add stub classification to code_stubs ([8ce09d6](https://github.com/whykusanagi/celeste-cli/commit/8ce09d6af198169e76fdb6d338284edf7b879bd9))
* implement SendMessageSync for xAI backend ([10afe16](https://github.com/whykusanagi/celeste-cli/commit/10afe16a3682a82599b93b219621334c4a004535))
* **install:** macOS-safe install (build-to-dest + ad-hoc sign) ([63e4f2d](https://github.com/whykusanagi/celeste-cli/commit/63e4f2d110d35d3a32605409665ce664bd7927ae))
* language detection uses file count for multi-language projects ([bae0c1e](https://github.com/whykusanagi/celeste-cli/commit/bae0c1e193166a9da3f2e7ddeeadfc9a142d005b))
* lint — remove unused code, fix gofmt formatting ([a88f452](https://github.com/whykusanagi/celeste-cli/commit/a88f45261cfa43337beb1de4501e760e56f18b86))
* **llm:** flag corrupted xAI tool-call args at stream finish ([#48](https://github.com/whykusanagi/celeste-cli/issues/48)) ([e80b925](https://github.com/whykusanagi/celeste-cli/commit/e80b9258bafb0b0bd527fec367ec92f038f7b6cc))
* **llm:** raise Anthropic default max_tokens from 8192 to 32768 ([5da41de](https://github.com/whykusanagi/celeste-cli/commit/5da41de7beeaf8eb1a826687a25bec8c7093e640))
* **llm:** read_file/trim budget collision + release toolchain + consolidated changelog ([#105](https://github.com/whykusanagi/celeste-cli/issues/105)) ([87709be](https://github.com/whykusanagi/celeste-cli/commit/87709bef746ad0d9b61c9e6c2c3d5baf187a53eb))
* make 'config --set-*' respect the -config &lt;name&gt; profile ([55dcfda](https://github.com/whykusanagi/celeste-cli/commit/55dcfdac5ab8603c34c1049a0dce104ef8aa7364))
* make agent RunID unique to stop checkpoint file collisions ([#78](https://github.com/whykusanagi/celeste-cli/issues/78)) ([57b27bd](https://github.com/whykusanagi/celeste-cli/commit/57b27bd436440d47dd15f4c0897673c6d7f1a758))
* MCP agent mode auto-approves tools + guard fabricated subagent-spawn claims ([77e55f0](https://github.com/whykusanagi/celeste-cli/commit/77e55f07d59a1ffa0c0d0248c2366ed530a6d520))
* **mcp:** gate per-tool registration logging behind CELESTE_MCP_DEBUG ([82a16c9](https://github.com/whykusanagi/celeste-cli/commit/82a16c99e3f9ecfe523b3704b8c71b0db2fbd93f))
* **model:** default to grok-4.20-0309-non-reasoning; never route to grok-4.3; bulk-safe guard ([ebc4df6](https://github.com/whykusanagi/celeste-cli/commit/ebc4df67f772b1cabc5fdeb712cebdfe4a71a977))
* **release:** bump Go 1.26.2 + pin CGO_ENABLED=0 for v1.9.0 ([0879252](https://github.com/whykusanagi/celeste-cli/commit/08792522ba6a95d334f98f81620da80f111250eb))
* **release:** bump Go to 1.26.2 and pin CGO_ENABLED=0 for v1.9.0 ([f5f0d44](https://github.com/whykusanagi/celeste-cli/commit/f5f0d4438d62b6b33bcc4d697ece2955a5224822))
* remaining gofmt + deprecated .Copy() for CI ([0571d9e](https://github.com/whykusanagi/celeste-cli/commit/0571d9e73f01cebc0115666164220c7c98173d86))
* remaining lint — gofmt tts files, last deprecated .Copy() ([4ac3a13](https://github.com/whykusanagi/celeste-cli/commit/4ac3a13216beaedbf2b99c8846d77fd4e6749806))
* remove 'distract with flirtation when uncertain' from Celeste persona ([e70124e](https://github.com/whykusanagi/celeste-cli/commit/e70124e504bf949c4c814a9c627111192f613e5a))
* remove accidentally committed temp files ([a990eeb](https://github.com/whykusanagi/celeste-cli/commit/a990eebee9158529748a773c75115cfd44749bd7))
* remove unused renderMessage (lint) ([daafb13](https://github.com/whykusanagi/celeste-cli/commit/daafb1331b1583dbe29ce1cfab60982300305eed))
* remove unused renderMessage (lint) ([5f0c5aa](https://github.com/whykusanagi/celeste-cli/commit/5f0c5aa27dff5a7ded21ed23d3e69b858bde1da9))
* Replace all celesteCLI references with celeste-cli across repo ([09f75bd](https://github.com/whykusanagi/celeste-cli/commit/09f75bd93f099980d7ff995d5dce18dca868dfc1))
* resolve all golangci-lint CI errors ([237a464](https://github.com/whykusanagi/celeste-cli/commit/237a464cdbfc77f25f811f4653b16a14ae0ad793))
* resolve all golangci-lint errors for CI ([54259e7](https://github.com/whykusanagi/celeste-cli/commit/54259e7b5401064b44e4cdcd1378a3cd9e4d65d1))
* resolve CI lint and formatting issues ([f983bac](https://github.com/whykusanagi/celeste-cli/commit/f983bacbf9c4865f4f3b4055fecb95b6a94fe6fc))
* resolve golangci-lint errcheck warnings and Windows test failures ([a49c995](https://github.com/whykusanagi/celeste-cli/commit/a49c995a1c7fe753b320cd1e10ba55d88368abdb))
* resolve remaining golangci-lint errors ([7f048a2](https://github.com/whykusanagi/celeste-cli/commit/7f048a2ddc9f13194f14e7084fcbeb945de0d22f))
* restore missing content policy refusal patterns ([92fa1ba](https://github.com/whykusanagi/celeste-cli/commit/92fa1bac193c5ebad1329c5867ba3eb94dafb1ce))
* rewrite ROUTING.md with proper code blocks, fix version refs ([34e5e0f](https://github.com/whykusanagi/celeste-cli/commit/34e5e0ff53b5bf3abb6bd517a75888a65f7d782e))
* **security:** align GPG release verification with celeste-ops ([ed99a7f](https://github.com/whykusanagi/celeste-cli/commit/ed99a7f9b16fcbd018126eccf04dc28ee1961573))
* **security:** bump golang.org/x/net 0.49.0 -&gt; 0.55.0 (govulncheck GO-2026-5025..5039) ([142811e](https://github.com/whykusanagi/celeste-cli/commit/142811ee132c94ae0096a9e72371adeee2d01505))
* **server:** celeste_code_graph + celeste_code_symbols schema mismatch ([d6ff77e](https://github.com/whykusanagi/celeste-cli/commit/d6ff77e2b94bdb79dc9a0c3fb944a5bfffea0a82))
* **server:** chat mode surfaces tool-call errors instead of running empty args ([#48](https://github.com/whykusanagi/celeste-cli/issues/48)) ([688f7bb](https://github.com/whykusanagi/celeste-cli/commit/688f7bbc18af0cb87be7181782d8e76787dbc307))
* **server:** escape tool-error JSON via json.Marshal; clarify ttsRan scope ([#48](https://github.com/whykusanagi/celeste-cli/issues/48)) ([7b4822b](https://github.com/whykusanagi/celeste-cli/commit/7b4822bc28ae5e534d3b0f2242dbf5c82f5989c5))
* **server:** only set ttsRan on successful TTS, not soft-failures ([#48](https://github.com/whykusanagi/celeste-cli/issues/48)) ([66e4336](https://github.com/whykusanagi/celeste-cli/commit/66e4336ca736394a3fc4c6ed23c0db87c9e31b9e))
* **server:** progress-aware repetition guard for args-varying loops (task 8f02ed3d) ([5240c8d](https://github.com/whykusanagi/celeste-cli/commit/5240c8d35734a498e05dff3853cc5e5fa7e1c857))
* skip bash tool test on Windows (no sh available) ([d0aad84](https://github.com/whykusanagi/celeste-cli/commit/d0aad84f50f8d9e9b744e6e286dec490e44d0a33))
* skip pwd workspace test on Windows to fix CI ([841477f](https://github.com/whykusanagi/celeste-cli/commit/841477fc2328a6bdd0ae318dd5615f9e20f7686a))
* start fresh session on each celeste chat invocation ([f717af3](https://github.com/whykusanagi/celeste-cli/commit/f717af30e529497de4258c4b78ca100f0f17199b))
* **subagents:** /agents kill resolves on-screen name (element/Romaji), not just id (#d15ac448) ([8792c1d](https://github.com/whykusanagi/celeste-cli/commit/8792c1d4e96d3f077168cc07e2093ac502f43dec))
* **subagents:** backgrounded spawn returns 'running in background', not 'completed in 0 turns' (db9b9282) ([41592c9](https://github.com/whykusanagi/celeste-cli/commit/41592c9688cdd9708026705e688dcff76a926915))
* **subagents:** detach background runs from the parent tool ctx (keystone bg bug) ([ed34fe6](https://github.com/whykusanagi/celeste-cli/commit/ed34fe69d19e2c25f5facdfdc8cfe7fe93bf06f4))
* **subagents:** guard run.* writes under m.mu — data race with /agents kill ([05adca9](https://github.com/whykusanagi/celeste-cli/commit/05adca95d59f2cfc2a199244f9ab70f1c377e296))
* **subagents:** ListRuns dedupes task_id runs (no duplicate /agents rows) ([af3952e](https://github.com/whykusanagi/celeste-cli/commit/af3952e8e46c998027141cfeb41b07c600d89542))
* **subagents:** propagate error on pre-threshold failure; lock OnBackgroundComplete read; count background in stagger ([#30](https://github.com/whykusanagi/celeste-cli/issues/30)) ([2ee0b13](https://github.com/whykusanagi/celeste-cli/commit/2ee0b13a4182dfa2120d11e473b0f8e9ac836376))
* **subagents:** resume in the subagent's original workspace, not the manager default ([#33](https://github.com/whykusanagi/celeste-cli/issues/33)) ([0229537](https://github.com/whykusanagi/celeste-cli/commit/02295370f72db7e55475ad4270ff94c0431ad3db))
* **subagents:** serialize worktree merges + sanitize worktree names ([#32](https://github.com/whykusanagi/celeste-cli/issues/32)) ([309a66e](https://github.com/whykusanagi/celeste-cli/commit/309a66e4207e5949f1c5b02cc7ccda4a2a4295d1))
* suppress errcheck for fmt.Sscanf and fmt.Scanln patterns ([36892a0](https://github.com/whykusanagi/celeste-cli/commit/36892a0bac944e27471ee4e06cbed57df446b1ea))
* **test:** deterministic MinHash seeds so codegraph ranking test isn't flaky ([#76](https://github.com/whykusanagi/celeste-cli/issues/76)) ([ef76768](https://github.com/whykusanagi/celeste-cli/commit/ef76768c9f947a8eadf9d70d48a773cbfd982a8d))
* **tts:** distinguish missing vs empty text arg ([#48](https://github.com/whykusanagi/celeste-cli/issues/48)) ([09d0567](https://github.com/whykusanagi/celeste-cli/commit/09d05679d88b65e906d415337b23e8e3b3493c24))
* TUI command bugs, context rot, and rename to Celeste CLI ([9b705d7](https://github.com/whykusanagi/celeste-cli/commit/9b705d7b752d373ba7b5e184aebfb4ed02c15cf3))
* TUI crash on startup — textinput width panic (v1.8.3) ([4f955be](https://github.com/whykusanagi/celeste-cli/commit/4f955be90229c772bc04e1e931f1128ce5dfd8f1))
* TUI typing animation freeze and layout corruption after tool completion ([ca1f395](https://github.com/whykusanagi/celeste-cli/commit/ca1f395d423a2d8758655983e45d74fed0109ca0))
* **tui:** add /costs /grimoire /index slash commands + update typeahead ([62784a1](https://github.com/whykusanagi/celeste-cli/commit/62784a1aabd5d5914b1deb80622516d57a3d4722))
* **tui:** align corruption cyan/red to canonical corrupted-theme 0.2.0 ([#49](https://github.com/whykusanagi/celeste-cli/issues/49)) ([9bbec88](https://github.com/whykusanagi/celeste-cli/commit/9bbec886994d8e2c01bf355af383e85e6a0b9a2d))
* **tui:** fix scrolling, copy, and permission spam in TUI ([ca7003b](https://github.com/whykusanagi/celeste-cli/commit/ca7003bb92fc27e2992fa23b2fc33a67941a1d6b))
* **tui:** guard checker access, I/O outside lock, matchable permission patterns ([#34](https://github.com/whykusanagi/celeste-cli/issues/34)) ([ee87e7b](https://github.com/whykusanagi/celeste-cli/commit/ee87e7b786aca1b5bc0fe2c4f4b9f5c7418ef519))
* **tui:** searchable + paginated skills browser ([#102](https://github.com/whykusanagi/celeste-cli/issues/102)) ([0b3dafa](https://github.com/whykusanagi/celeste-cli/commit/0b3dafaa5c01f974bb7fded5f93fa6c4980d7e69))
* **tui:** show actual data for /grimoire /index /costs slash commands ([d08dc1f](https://github.com/whykusanagi/celeste-cli/commit/d08dc1f361ed382e81a92ff7497fa19db73fc037))
* **tui:** streaming tick-complete race truncated short first chunks ([6e43e91](https://github.com/whykusanagi/celeste-cli/commit/6e43e91b915c0e99963bb75dc3c075c833c273a2))
* **tui:** streaming tick-complete race truncated short first chunks (v1.9.1) ([7ab290d](https://github.com/whykusanagi/celeste-cli/commit/7ab290dc369f5a8c8469e049d5fabc2d869bd3fe))
* **tui:** switch input from textinput to textarea for word wrap ([4370eef](https://github.com/whykusanagi/celeste-cli/commit/4370eef3e6e04441b50f790ab3403973ce51373b))
* unescape literal \n \t in write_file and patch_file ([1076806](https://github.com/whykusanagi/celeste-cli/commit/10768062aea39c6bc1411d880d9996144ee48862))
* Update .gitignore to allow source code directory ([a369e18](https://github.com/whykusanagi/celeste-cli/commit/a369e18e67af12bf7fb90959a2c75b96c5c8053c))
* update .golangci.yml for golangci-lint v1.64 compatibility ([0d76c44](https://github.com/whykusanagi/celeste-cli/commit/0d76c44119f8fda938765a066b132b0450a614c5))
* update docker-compose.yml to reference renamed Dockerfile ([635b9b5](https://github.com/whykusanagi/celeste-cli/commit/635b9b553ff1eab924b37fc63a01886df9595684))
* update Dockerfile and test runner for v1.8 packages ([f5e7aed](https://github.com/whykusanagi/celeste-cli/commit/f5e7aed405d6828e32988fa4ecbcf993d44c3420))
* update golangci-lint and Dockerfiles for Go 1.26 ([93a0d1e](https://github.com/whykusanagi/celeste-cli/commit/93a0d1efbac616ec150f845f2603a1f88eacc19d))
* update golangci-lint config for tools package rename ([d19cf50](https://github.com/whykusanagi/celeste-cli/commit/d19cf50e98aba6ecc0a482b88961843fffaffcaa))
* update golangci-lint config for v1.64 compatibility ([6e9702e](https://github.com/whykusanagi/celeste-cli/commit/6e9702e369c815716227493b7fd92c0eb3b21656))
* Update module path to match new repository name ([9c26c7d](https://github.com/whykusanagi/celeste-cli/commit/9c26c7d0f7e2b18b7cc56b75dc43db0b3879722b))
* upgrade Go to 1.26.x to resolve 5 govulncheck CVEs ([8af50b4](https://github.com/whykusanagi/celeste-cli/commit/8af50b4fb57a03e89e136fc870cf03a97bf9a8cd))
* upgrade go-ethereum v1.16.8 → v1.17.0 to fix GO-2026-4508 DoS vuln ([11bd59d](https://github.com/whykusanagi/celeste-cli/commit/11bd59d0cd59905c11ed25b87da21581e669e5ab))
* use lowercase language names consistently in .grimoire ([5cc009d](https://github.com/whykusanagi/celeste-cli/commit/5cc009d0a4c216c46a82aecf41e771ea0b24f6ec))
* use real model IDs from provider APIs + correct pricing ([f0fe595](https://github.com/whykusanagi/celeste-cli/commit/f0fe5956acbd3472f91b4690610f8bd757c519b7))
* v1.8.1 — native Anthropic backend, code graph edges, audit fixes ([200f177](https://github.com/whykusanagi/celeste-cli/commit/200f17758a648f49c93fce65c0d7351070b65ec2))
* v1.8.3 — TUI crash on startup (textinput width panic) ([beafc33](https://github.com/whykusanagi/celeste-cli/commit/beafc33602f0db0a92a7015041cff6d965a276b8))
* Windows CI — set USERPROFILE for user identity tests ([99213ba](https://github.com/whykusanagi/celeste-cli/commit/99213ba344eaaee07986c2b6065a766acb4cce13))
* Windows compatibility for memories index and server auth test ([ea82069](https://github.com/whykusanagi/celeste-cli/commit/ea82069ad24c02ae6bd196fc914a64e54ed75a96))
* Windows user test — set USERPROFILE for os.UserHomeDir() ([f70a97f](https://github.com/whykusanagi/celeste-cli/commit/f70a97f8fc2d738561a454128fe45308eae41881))
* wire FileTracker and SnapshotManager into file tools via RegisterAll ([724ad12](https://github.com/whykusanagi/celeste-cli/commit/724ad126cc6ee1522390d6f0afd62eb4ed5c5558))
* write TTS audio to the workspace, not the process cwd ([fa519a4](https://github.com/whykusanagi/celeste-cli/commit/fa519a48202ccaa23d01b43daf3106b879cccc77))

## [1.14.0](https://github.com/whykusanagi/celeste-cli/compare/v1.13.0...v1.14.0) (2026-07-15)

The large-file reliability release. Reading one big or minified file could poison
a whole session. This release closes that across the read path, the retry path,
and the write path, and makes the skills browser usable at scale.

### Skills browser

* Type-ahead search, pagination, and a full-description line for the selected
  skill. The filter ranks with the same BM25 index that powers tool discovery,
  and falls back to substring matching so partial words still hit. ([#102](https://github.com/whykusanagi/celeste-cli/issues/102))

### Large-file byte path

Three bugs compounded so a single oversized tool payload corrupted the session:

* **read_file** bounds the returned bytes on a line boundary (48 KiB default) and
  reports `total_bytes`, `returned_bytes`, and `next_offset_line` with a paging
  hint. A minified single line no longer returns the whole file for the 128 KiB
  history cap to mid-byte-truncate into garbage. ([#103](https://github.com/whykusanagi/celeste-cli/issues/103))
* **Retry trim.** Each LLM retry gets its own deadline instead of reusing the
  first attempt's expired one, so a timeout retry runs instead of failing
  instantly. Before each retry the trim shrinks the largest tool result
  copy-on-write, so an oversized payload never replays unchanged. ([#103](https://github.com/whykusanagi/celeste-cli/issues/103))
* **splice_file** moves a region between files by anchors or line ranges. The
  bytes copy on disk and the model never regenerates them, so relocating a large
  block cannot corrupt it in transit. `patch_file` refuses a literal over 16 KiB
  and points to `splice_file`. ([#103](https://github.com/whykusanagi/celeste-cli/issues/103))
* A capped read_file result plus its JSON wrapper tripped the retry trim and
  mangled its own metadata; the two byte budgets no longer overlap. ([#105](https://github.com/whykusanagi/celeste-cli/issues/105))

### CI

* Release binaries build with Go 1.26.5, which carries the crypto/tls fix for
  GO-2026-5856. ([#105](https://github.com/whykusanagi/celeste-cli/issues/105))

## [1.13.0](https://github.com/whykusanagi/celeste-cli/compare/v1.12.0...v1.13.0) (2026-07-11)

The MCP-connectivity release. Celeste installs itself into other MCP clients,
consumes the servers they already have, speaks the modern Streamable-HTTP
transport, and gains a set of TUI features around all of it.

### Features

#### MCP connectivity

* **`celeste mcp install`** — a self-locating installer. It resolves celeste's
  own absolute path and writes it into Claude Desktop, Claude Code, and Cursor
  MCP configs, so GUI clients that don't inherit your shell PATH can still launch
  it. Merges without touching your other servers, backs up to `<file>.bak`,
  refuses to write through a symlink, and supports `--dry-run` and `--client`
  (`claude-desktop` / `claude-code` / `cursor` / `celeste-cli` / `all`; `codex`
  prints a TOML block to paste). ([#98](https://github.com/whykusanagi/celeste-cli/pull/98))
* **Foreign / project config discovery** — celeste merges MCP servers you've
  already defined for Claude Code or Cursor (`~/.claude/mcp.json`,
  `~/.cursor/mcp.json`, project `.mcp.json`), gated behind an opt-in
  `"enabled": true` so nothing connects until you ask.
* **Runtime `/mcp` panel** — connect, disconnect, reconnect, or toggle a server
  without restarting (`c` / `d` / `r` / `space`). Actions run async, so an OAuth
  handshake never freezes the UI.
* **Streamable-HTTP transport** — set `"transport": "http"` to reach modern MCP
  servers over POST + SSE with the negotiated `MCP-Protocol-Version` header, no
  stdio bridge.
* **Protocol-version negotiation** — celeste accepts any supported MCP revision
  it's offered (2024-11-05 through 2025-06-18) instead of demanding an exact
  match.
* **`find_tools` dynamic discovery** — a BM25-ranked tool that surfaces hidden or
  external tools on demand. It turns on once the registered tool list crosses 40,
  keeping the prompt lean as MCP servers pile up.

#### TUI

* **`ask` tool** — the model can ask you a multiple-choice question mid-turn. An
  interactive picker (single- or multi-select) blocks the turn until you answer,
  and degrades to a clear error in headless / one-shot contexts.
* **Segmented status line** — git branch, dirty count, ahead/behind, project,
  model, effort, permission mode, session, and skill count in one bar; it narrows
  its segments below 80 columns.
* **Contextual key hints** per view and **boxed tool-call cards** (running / done
  / failed with elapsed time and a detail line), plus a consolidated bottom
  layout that hands the reclaimed rows back to the chat.

### Bug Fixes

* **deps:** bump github.com/anthropics/anthropic-sdk-go ([#94](https://github.com/whykusanagi/celeste-cli/pull/94))
* **deps:** bump golang.org/x/text ([#95](https://github.com/whykusanagi/celeste-cli/pull/95))
* **deps:** bump google.golang.org/genai from 1.62.0 to 1.63.0 ([#96](https://github.com/whykusanagi/celeste-cli/pull/96))
* **deps:** bump github.com/ipfs/go-cid from 0.6.1 to 0.6.2 ([#97](https://github.com/whykusanagi/celeste-cli/pull/97))
* **deps:** bump goldmark to v1.7.17 (GO-2026-5320) and CI Go to 1.26.5 (GO-2026-5856)

### CI / internal

* Run the cross-platform test matrix post-merge instead of on every PR ([#93](https://github.com/whykusanagi/celeste-cli/pull/93))
* Visual TUI + live-model CLI smoke tests wired into the release gate (`make smoke`) ([#100](https://github.com/whykusanagi/celeste-cli/pull/100))

## [1.12.0](https://github.com/whykusanagi/celeste-cli/compare/v1.11.2...v1.12.0) (2026-06-28)


### Features

* **config:** resolve default profile from a file flag, not hardcoded provider ([#92](https://github.com/whykusanagi/celeste-cli/issues/92)) ([4c9ca8f](https://github.com/whykusanagi/celeste-cli/commit/4c9ca8fd1a7ee9571cef3ee278cb86bd6c841eb6))


### Bug Fixes

* **deps:** bump github.com/anthropics/anthropic-sdk-go ([#88](https://github.com/whykusanagi/celeste-cli/issues/88)) ([a38dc35](https://github.com/whykusanagi/celeste-cli/commit/a38dc356205552c397102d6b32e0b0f36392d102))
* **deps:** bump github.com/tree-sitter/tree-sitter-php ([#89](https://github.com/whykusanagi/celeste-cli/issues/89)) ([c65921d](https://github.com/whykusanagi/celeste-cli/commit/c65921d59d20998b7d1ceaadf06ea2cae53710d7))
* **deps:** bump google.golang.org/genai from 1.39.0 to 1.62.0 ([#90](https://github.com/whykusanagi/celeste-cli/issues/90)) ([0302c87](https://github.com/whykusanagi/celeste-cli/commit/0302c87b1cee26bdd8e412d230c6d411e5e4f1ea))

## [1.11.2](https://github.com/whykusanagi/celeste-cli/compare/v1.11.1...v1.11.2) (2026-06-23)


### Bug Fixes

* honor -config in read commands; /model lists real provider models in TUI ([#82](https://github.com/whykusanagi/celeste-cli/issues/82)) ([cb7e4df](https://github.com/whykusanagi/celeste-cli/commit/cb7e4df9fa6ebc3467aea7f9666900f2a1432b3b))

## [1.11.1](https://github.com/whykusanagi/celeste-cli/compare/v1.11.0...v1.11.1) (2026-06-22)


### Bug Fixes

* **deps:** bump github.com/charmbracelet/bubbles from 0.21.0 to 1.0.0 ([#69](https://github.com/whykusanagi/celeste-cli/issues/69)) ([ae00976](https://github.com/whykusanagi/celeste-cli/commit/ae009764174b8c35c95f9766b791401f3bd5903b))
* **deps:** bump github.com/ethereum/go-ethereum from 1.17.0 to 1.17.4 ([#73](https://github.com/whykusanagi/celeste-cli/issues/73)) ([46a7ab7](https://github.com/whykusanagi/celeste-cli/commit/46a7ab7b9c5061bd9cc7c5a1f4e63904d8f9a7c7))
* **deps:** bump github.com/ipfs/go-cid from 0.6.0 to 0.6.1 ([#63](https://github.com/whykusanagi/celeste-cli/issues/63)) ([df46df3](https://github.com/whykusanagi/celeste-cli/commit/df46df3e5c1ae1621d219032c396f37d53d3e0f7))
* **deps:** bump github.com/tree-sitter/tree-sitter-python ([#67](https://github.com/whykusanagi/celeste-cli/issues/67)) ([3eb4644](https://github.com/whykusanagi/celeste-cli/commit/3eb4644733546439368ba84317317a1d3d9b40b2))
* **deps:** bump github.com/tree-sitter/tree-sitter-rust ([#72](https://github.com/whykusanagi/celeste-cli/issues/72)) ([6515c94](https://github.com/whykusanagi/celeste-cli/commit/6515c942dc9c575f345a081d4a26498b6ed73508))
* **deps:** bump modernc.org/sqlite from 1.48.1 to 1.53.0 ([#65](https://github.com/whykusanagi/celeste-cli/issues/65)) ([6505021](https://github.com/whykusanagi/celeste-cli/commit/65050215cc2596d4479062f11c51fe25ff18b813))
* **deps:** upgrade anthropic-sdk-go to v1.51.1; pin govulncheck past its generics panic ([#80](https://github.com/whykusanagi/celeste-cli/issues/80)) ([57c907e](https://github.com/whykusanagi/celeste-cli/commit/57c907e4dfa498c30b1ce8a9fd80633cffc9a89d))
* make agent RunID unique to stop checkpoint file collisions ([#78](https://github.com/whykusanagi/celeste-cli/issues/78)) ([57b27bd](https://github.com/whykusanagi/celeste-cli/commit/57b27bd436440d47dd15f4c0897673c6d7f1a758))
* **test:** deterministic MinHash seeds so codegraph ranking test isn't flaky ([#76](https://github.com/whykusanagi/celeste-cli/issues/76)) ([ef76768](https://github.com/whykusanagi/celeste-cli/commit/ef76768c9f947a8eadf9d70d48a773cbfd982a8d))

## [1.11.0](https://github.com/whykusanagi/celeste-cli/compare/v1.10.0...v1.11.0) (2026-06-22)


### Features

* add Sakana AI (Fugu) provider ([7de3e35](https://github.com/whykusanagi/celeste-cli/commit/7de3e35520d0cbe30508b6cb6c5c6a96edc77e43))
* add Sakana AI (Fugu) provider ([a859910](https://github.com/whykusanagi/celeste-cli/commit/a85991005aae1bd10ccb04d47eb76ee4a1de5a87))


### Bug Fixes

* address Fugu pre-release review ([7b9c742](https://github.com/whykusanagi/celeste-cli/commit/7b9c7423863f8e6e10688f0717253f031193d83e))
* cap tool calls before recording them so declared calls match results ([c4d5373](https://github.com/whykusanagi/celeste-cli/commit/c4d537376e4ef2cdba0ae193ca149d1936ac9730))
* **ci:** version tests read serverVersion constant; stop double-running CI per branch ([#71](https://github.com/whykusanagi/celeste-cli/issues/71)) ([ab8c2a4](https://github.com/whykusanagi/celeste-cli/commit/ab8c2a4038a071436d0cbde058a17215b4fdce9b))
* **deps:** bump the golang-x group across 1 directory with 2 updates ([#74](https://github.com/whykusanagi/celeste-cli/issues/74)) ([3a1475c](https://github.com/whykusanagi/celeste-cli/commit/3a1475c887072e30a474e4f31b6bb686bf3461be))
* make 'config --set-*' respect the -config &lt;name&gt; profile ([55dcfda](https://github.com/whykusanagi/celeste-cli/commit/55dcfdac5ab8603c34c1049a0dce104ef8aa7364))
* **security:** align GPG release verification with celeste-ops ([ed99a7f](https://github.com/whykusanagi/celeste-cli/commit/ed99a7f9b16fcbd018126eccf04dc28ee1961573))
* write TTS audio to the workspace, not the process cwd ([fa519a4](https://github.com/whykusanagi/celeste-cli/commit/fa519a48202ccaa23d01b43daf3106b879cccc77))

## [Unreleased]

## [1.10.0] - 2026-06-03

### Added

- **Model router + capability guardrail (task e8775b91).** New `agent_model` config
  field: agent / orchestrate / subagent work uses `ResolveAgentModel()` (agent_model
  if set, else the chat model), so you can pin a reasoning/tool-capable model for
  agent work while keeping a cheap non-reasoning model for chat. The agent runner
  warns loudly when the chosen model doesn't support tool calling (it will flail /
  hallucinate in agent mode). `reconcileModel` migrates the grok-4-1-* trap on
  `agent_model` too. (`Options.Model` is the per-run override seam.)
- **Subagents + MCP agent mode auto-approve tools** so they can actually do work
  (write/commit/bash) headlessly — spawning / invoking is the approval. The
  interactive main agent stays `/confirm`-gated.
- **Corruption colors are sourced from the canonical corrupted-theme palette
  (task 7aa133c9).** New `cmd/celeste/tui/theme` package embeds `colors.json`
  (synced from corrupted-theme `src/data/colors.json` via `make sync-theme`);
  `streaming.go` consumes it via `theme.Hex(...)` instead of hardcoded hex, so the
  #00ffff/#ff0000 colors track the theme repo. Corruption phrase/glitch pools are
  intentionally left in code (animation-critical).
- **`/agents kill <id>` cancels a specific in-flight subagent (task 6ffb5a7c).** The
  manager now tracks a per-run cancel function (keyed by run id and task id) and
  exposes `Manager.Kill`; the TUI wires `/agents kill <id>` (with autocomplete and
  help text). Combined with the runtime ctx-honoring fix, this gives a manual escape
  hatch for a stuck subagent instead of having to kill the whole TUI.
- **Subagent resilience and orchestration.** Transient-error retry with backoff at the
  LLM client layer — 429 honors `Retry-After`, 5xx and network errors retry with capped
  exponential backoff, 4xx fails fast (#29). Background subagents with auto-transition
  after a threshold, exposed via the `spawn_agent` `background_after` param (#30).
  Inter-agent mailbox messaging with a `post_message` tool for spawn-time injection (#31).
  Opt-in git worktree isolation per subagent (`isolate_worktree` param), with serialized
  merges and sanitized worktree names (#32). Checkpoint persistence with `/agents resume`
  to continue a failed subagent from its last completed turn (#33).
- **TUI hard permission gate (#34).** Modal confirmation that blocks tool execution:
  reads auto-approve, writes require approval. Options for yes / no / yes-for-this-tool /
  yes-all-this-session, with rule persistence.
- **Codegraph accuracy improvements.** STUB detection now skips dunder methods (#42),
  `Protocol`/`ABC`/`@abstractmethod` members (#43); decorator `@syntax` calls (#44) and
  `@property.setter` assignments (#45) are captured as call edges; `include_tests` now
  matches top-level test dirs and pytest conventions (#46); two-pass `Build()` fixes
  dropped cross-file caller counts (#47).

### Changed

- **Default model is now `grok-4.20-0309-non-reasoning`** — reliable tool calling, zero
  reasoning-token burn, never routes to the cost-prohibitive grok-4.3.

### Fixed

- **Subagent/agent runtime no longer hangs uncancellably on a stuck tool (task 349f1f14).**
  A tool that ignores its context (e.g. a codegraph call spinning on the DB) used to
  block the turn loop forever — the per-tool timeout fired on the context but nothing
  observed it. Tool execution now runs under `runToolWithTimeout`, which returns at the
  deadline even if the tool keeps running, and the turn loop checks `ctx.Err()` between
  turns so an expired parent deadline (a subagent's overall timeout) stops it promptly.
  New `StatusCancelled` run status.
- **Codegraph operations honor context cancellation (task 349f1f14).** `SemanticSearch`,
  `Build`, and `Update` gained `*WithContext` variants that check `ctx` before and during
  their hot loops, so a `code_search`/`code_index` tool call stops at the tool deadline
  instead of scanning the whole corpus/repo. The `code_search` tool and the server index
  tools pass their request context through; non-ctx signatures are preserved as
  `context.Background()` wrappers.
- **Progress-aware repetition guard (task 8f02ed3d).** The chat/message loop now stops a
  loop where the model re-calls the same tool with slightly-varying args but gets
  byte-identical results turn after turn — a case the args-based guard missed. It keys on
  the result, not the args, so legitimate bulk work (each call producing a distinct
  result, e.g. a new mp3 file) is never blocked.
- **xAI streaming tool-call JSON corruption / TTS hallucination (#48).** Validate
  assembled tool-call JSON at stream finish, chat-mode error-handling parity with agent
  mode, block hallucinated `Audio saved:` claims when no file is written, distinguish
  missing vs empty TTS text, and cap consecutive invalid-args turns instead of retrying
  unbounded.
- **Model routing safety (#51).** Auto-migrate deprecated `grok-4-1-*` models (xAI
  silently routes them to grok-4.3 — the cost trap) and fill empty model on load; clamp
  a stale `context_limit` that exceeds the model window; correct `grok-build-0.1` to its
  real 256K window and pricing.
- **Repetition guard** in both the TUI and server message loops — the call signature now
  includes args, so it only trips on identical repeated calls and does not block bulk
  distinct work (e.g. batch TTS).
- **macOS install** no longer produces `zsh: killed` — `make install` builds to the
  destination and re-applies an ad-hoc code signature instead of `cp`-over-existing.
- **Theme color drift (#49)** — corruption cyan/red aligned to canonical corrupted-theme
  0.2.0 (`#00ffff` / `#ff0000`).
- **Quiet MCP startup** — per-tool registration logging is now gated behind
  `CELESTE_MCP_DEBUG`.

### Security

- **Hard permission gate (#34)** blocks tool execution pending explicit approval,
  closing the prompt-injection bypass of the previous soft `/confirm`.
- **Subagent worktree isolation (#32)** contains the blast radius of any single subagent.
- **grok-4-1-\* migration guard (#51)** prevents silent routing to the cost-prohibitive
  grok-4.3 model.

## [1.9.3] - 2026-04-21

### Added

- **Subagent orchestration with DAG dependencies.** Element-named agents (地火水光闇風)
  with auto-detected dependency chains from goal text. Agents that reference another
  agent's task_id automatically wait until the dependency completes before starting.
  Parallel dispatch for independent tasks, sequential execution for dependent chains.
- **Multi-language tree-sitter code graph.** Native Go parsers for 10 languages
  (Python, Rust, TypeScript, JavaScript, Java, C, C++, Ruby, PHP, TSX). 67-140%
  more symbols extracted vs regex. Node type mappings derived from code-review-graph.
- **Graph snapshots and change impact analysis.** `/index snapshot` saves graph state,
  `/index diff` compares against last snapshot, `/index impact` maps git diff to affected
  symbols with risk scoring and test gap detection. MCP tools: `code_impact`, `code_snapshot`.
- **Audio production pipeline.** ElevenLabs TTS (generate, speak, play, batch, history,
  download), sound effects generation, timeline-aware ffmpeg mixer with loop/delay/volume,
  `audio_render` project pipeline with Gantt chart visualization. Idempotent batch with
  timeout recovery. SSML tags auto-stripped (ElevenLabs v3 doesn't support them).
- **User identity system.** `/user` command with Kusanagi mode vs Summoner default.
  Prompt refreshes mid-session on identity change. Hidden LLM-visible directives.
- **Session picker panel.** Interactive paginated session browser (↑/↓/PgUp/PgDn/Enter/d)
  rendered inline in the TUI. Session resume loads full conversation history.
- **ElevenLabs voice management.** `/voice list`, `/voice set-key`, `/voice set-voice`
  commands. Config persisted to disk, loaded fresh on each tool call.
- **Confirm mode.** `/confirm` toggle with status bar indicator. Task execution prompt
  for sequential multi-step plan completion.
- **Subcommand typeahead.** Purple hints for `/index rebuild`, `/voice list`, etc.
  Escape clears input. Navigation keybinds in status bar.

### Changed

- **v3.0.0 persona sync.** `system_prompt` field used directly for v3.0.0 essence,
  v1.x structured assembly preserved as fallback. Fraternal twin gender clarified.
  Anti-confabulation appearance fix ("No tail. No wings.").
- **Tool timeout tiers.** 30s for reads, 5min for bash/TTS, 10min for subagents,
  2min for audio render. Prevents long-running tools from being killed.
- **Model changes persist to config.** `/model` and `/config set-model` write to
  `config.json` so the setting survives restarts.
- **Empty response detection.** Shows "(No response — try rephrasing)" instead of
  appearing frozen when LLM returns empty.
- **Tool progress auto-clear.** Completed entries clear when response finishes,
  not on next user message.
- **Ctrl+K shows on/off state** for skill call logs visibility.
- **Grimoire injection for MCP content tool.** `celeste_content` now reads `.grimoire`
  from CWD for project-specific rules.
- **Tick animation during tool execution.** Spinners and elapsed timers now animate
  continuously while tools run, without requiring keypress.

### Fixed

- Empty assistant bubble for tool-call-only responses.
- Session resume type assertion mismatch (`tui.SessionMessage` vs `config.SessionMessage`).
- Subagent partial result recovery on failure (returns last assistant response).
- Mix validation rejects stacked audio (all tracks at delay:0 with no loops).
- DAG drain context cancellation (was killing queued agents immediately).
- Stagger delay based on active agent count, not total-ever spawned.
- Nil guard on failed spawn preventing panic.

## [1.9.2] - 2026-04-17

### Added

- **Persisted LSH band table for sub-linear semantic search.** 64 bands × 2 elements from
  the 128-element MinHash signature, stored in a new `lsh_bands` SQLite table with an index
  on `(band_id, band_hash)`. At query time, `SemanticSearchWithOptions` computes the query's
  64 band hashes and retrieves candidate symbol IDs directly from the table — typically 0.1-1%
  of the corpus — then ranks only those candidates by exact Jaccard similarity. Falls back to
  brute-force for pre-LSH indexes that haven't been rebuilt.

  The 64×2 band configuration is empirically derived from the CODEGRAPH_LSH_RESEARCH.md
  validation: conventional 16×8 and 32×4 configs produce 0% recall on code search because the
  Jaccard similarity range for code queries (0.05-0.20) is far below document similarity
  (0.5-0.8). At grafana scale (77,420 symbols), validated at 20× speedup over brute-force
  (89ms → 4.3ms) by eliminating the full signature BLOB load.

- **TUI `/index rebuild` and `/index update` subcommands.** Previously `/index` was
  display-only — users had to exit the TUI and run `celeste index --rebuild` from the shell.
  Now the TUI can trigger a full or incremental re-index directly, with async execution and
  status reporting via the typing animation.

- **TUI `/config set-key`, `/config set-model`, `/config set-url` subcommands.** Users can
  now modify API key, model, and base URL from the TUI without exiting. Previously required
  shelling out to `celeste config --set-*`.

### Changed

- `SemanticSearchWithOptions` now uses LSH candidate lookup when `lsh_bands` data exists,
  brute-force fallback when it doesn't. BM25, RRF fusion, structural rerank, and path filter
  are all unchanged — LSH is a pre-filter that narrows the candidate pool without affecting
  downstream scoring.
- Users must rebuild their index once to populate LSH band data:
  `/index rebuild` in TUI or `celeste_index { operation: "rebuild" }` via MCP.

## [1.9.1] - 2026-04-14

### Fixed

- **TUI streaming tick-complete race truncated short first chunks.** When grok streamed an
  assistant reply that started with a 1-3 character first delta (e.g. `"O"`), the typing
  animation exhausted its buffer on the very first `TickMsg` (50ms later, `charsPerTick=3`),
  committed that single character to session history, and stopped the ticker. Subsequent SSE
  chunks appended to a zombie buffer with no active renderer — the TUI chat bubble froze at
  the first chunk, session persistence wrote the 1-char string to disk, and the next LLM
  request was sent with `content_len=1` for that assistant message, losing the rest of the
  response as conversation context.

  Root cause was in `cmd/celeste/tui/app.go` — the `TickMsg` handler used
  `typingPos == len(typingContent)` as the sole termination condition without checking whether
  the network stream had actually finished. Fix adds an `AppModel.streamDone bool` that tracks
  `EventMessageDone` receipt; the tick-complete branch now has three cases:

  | `typingPos` vs `len` | `streamDone` | Action |
  |---|---|---|
  | `pos < len` | any | advance + reschedule (unchanged) |
  | `pos == len` | `false` | **idle**: reschedule a no-op tick so any late chunk gets picked up |
  | `pos == len` | `true` | commit to session + stop |

  `StreamChunkMsg(IsFirst=true)` resets `streamDone=false`. `StreamDoneMsg` sets it to true.
  `AgentProgressResponse` (non-streaming agent reply path that reuses the typing animation)
  primes `streamDone=true` up front so its animation commits on catch-up instead of idling
  forever waiting for a done event the agent path never sends.

  Validation: four new regression tests in `cmd/celeste/tui/streaming_race_test.go` reproduce
  the exact scenario and pin the new invariants. Confirmed in a live TUI session against
  grok-4-1-fast with a 630-character response rendered correctly on the first try — no
  re-prompt needed, no trailing anomalies in `~/.celeste/logs/celeste_2026-04-13.log`.

### Details

- Changed files: `cmd/celeste/tui/app.go`, `cmd/celeste/tui/streaming_race_test.go` (new),
  `cmd/celeste/main.go` (version constant), `cmd/celeste/server/server.go` (version constant),
  matching test assertions.
- Scope: this is the **only** behavioral change in v1.9.1. Everything else from v1.9.0 is
  untouched.

## [1.9.0] - 2026-04-13

Major search-quality bundle and MCP architecture rework. Eleven commits, ten closed feature
tasks, empirical A/B validation archives under
[celeste-stopwords/results/](https://github.com/whykusanagi/celeste-stopwords/tree/main/results).

SPEC §5.3 acceptance criteria all met on the grafana benchmark:
`JQueryStatic` absent from Q3 top 10, aggregate relevance 22/50 → 35/50 (+59% absolute),
no TP regressions on Q2/Q5, stopword list < 500 entries, corpus Apache-2.0/MIT.

### Added

- **Direct codegraph MCP tools.** Five new first-class MCP tools that bypass the chat LLM
  entirely so Claude Code / Cursor / any MCP client can call the codegraph directly:
  - `celeste_index` (operations: `status`, `update`, `rebuild`) — indexing is explicit; the
    query tools never auto-reindex. Progress notifications (`notifications/progress`) stream
    back through the stdio transport when the client provides a `progressToken`.
  - `celeste_code_search` — semantic search with MinHash+BM25 fusion + structural rerank
  - `celeste_code_review` — structural code review findings as verbatim JSON
  - `celeste_code_graph` — symbol callers/callees/references
  - `celeste_code_symbols` — list all symbols in a file or package

  Per-workspace `*codegraph.Indexer` cached on the server, lazy-opened, released on shutdown.
  Results returned verbatim as MCP `ContentBlock`s with no chat-LLM summarization and no
  `max_tokens` ceiling.

- **BM25 fused ranking.** Per-symbol term frequency and per-corpus IDF persisted in two new
  SQLite tables (`symbol_tokens`, `token_stats`). Query time merges Jaccard and BM25 via
  Reciprocal Rank Fusion (k=60). `SearchResult` gains `BM25Score` + `MatchedTokens`. Q1
  ("authentication session token validate") flipped from 2/10 relevant to 8/10 relevant on
  grafana after this landed — IDF-weighted tiebreaking on session/token tokens lifted the
  real auth API functions above the noise.

- **Tree-sitter TypeScript parser** (behind `//go:build cgo`). Replaces the regex-based
  `GenericParser` for `.ts` and `.tsx` files when celeste is built with CGo enabled. Accurate
  `call_expression` edge resolution instead of the old `\bname(` body-scan heuristic. On
  content-control (21 TS files) the edge count drops 445 → 211 (real, not overcounted) and
  zero-edge top-10 warnings drop 3 → 0. Python and Rust stay on the regex parser for v1.9.0;
  tracked for v2.1.0 as #18 and #19.

  `parser_ts_stub.go` provides a `//go:build !cgo` fallback that delegates to `GenericParser`
  so pure-Go cross-compile release binaries still work — they just lose the tree-sitter
  improvement. Users building from source get the full experience automatically.

- **Structural feature rerank layer.** `StructuralReranker` rescores candidates using features
  the RRF fusion can't see: matched-token-ratio, log-normalized edge density, function/method
  kind boost, zero-edge penalty. Pure Go, zero dependencies. Exposed via a `Reranker` interface
  on `SemanticSearchOptions` so a future embedding-based reranker (local llama.cpp bridge,
  grok/xAI embeddings, ONNX) can drop in without touching the pipeline.

- **Stopwords runtime integration.** Embedded `celeste-stopwords` v1.0.0 (CC BY 4.0) applied
  at both index time and query time. Filters universal + per-language noise tokens from
  shingle sets before MinHash so common tokens like `get`/`set`/`error`/`string` don't consume
  signature slots. Includes a downstream patch removing `query` from the TypeScript set to
  avoid asymmetric filtering breaking Q3 on TS codebases, plus `TestStopWords_PreserveTokensNotStopped`
  regression guard.

- **Reasoning metadata on `SearchResult`.** `EdgeCount`, `PathFlags`, `ConfidenceWarnings`,
  `MatchedTokens` — downstream LLMs can audit every search result instead of trusting a
  single similarity number. Confidence warnings surface zero-edge interfaces, low-confidence
  scores, declaration-only types, and path-demotion reasons.

- **Path-based post-ranking filter.** Tier-partitions `test` / `mock` / `generated` /
  `vendored` / `declaration` results below clean-path results with explicit `[mock]` etc.
  flags. Q2 ("http request handler middleware") previously had 100% mock handlers dominating
  the top 10 on grafana; v1.9.0 tiers them below production handlers. If the query itself
  asks for test/mock code (`queryWantsTests`), the filter backs off to respect user intent.

- **MinHash seed persistence.** Replaced `hash/maphash` (opaque, unserializable) with seeded
  FNV-1a (serializable uint64 seeds). The 128 seed values are stored in a new `meta` SQLite
  table at first `Build()`, so a subsequent process loading the same index restores the same
  hash family and signatures stay comparable across process invocations. Without this, the
  `celeste serve` MCP bridge silently returned noise because every new server process rolled
  fresh random seeds that had no relationship to the signatures already stored in the database.

### Changed

- **Anthropic backend default `max_tokens` raised from 8192 → 32768.** The old 8192 ceiling
  truncated the chat-mode MCP path mid-response when a sub-tool returned a multi-KB JSON blob
  and the chat LLM was asked to echo it. Claude opus/sonnet 4.x support up to 64K output
  tokens; 32K is a 4× budget with no downside (only used tokens are billed).

- **`ShinglesForSymbol` signature.** Now takes `lang string` so the embedded stopwords
  filter can apply the per-language set alongside the universal set. Callers that don't know
  the file language should pass `""` to get universal-only filtering.

- **`SearchResult` struct.** Extended with `BM25Score`, `MatchedTokens`, `EdgeCount`,
  `PathFlags`, `ConfidenceWarnings`. `Similarity` (Jaccard) is unchanged — existing callers
  that only read `Symbol` + `Similarity` keep working.

- **MinHash signatures computed by celeste-cli < 1.9.0 are semantically stale.** Existing
  indexes still work for symbol lookups, edges, and keyword search, but semantic search
  accuracy improves if you rebuild:

  ```bash
  # Via the CLI
  celeste index --rebuild

  # Via the MCP bridge
  # call celeste_index { operation: "rebuild", workspace: "/path/to/project" }
  ```

### Fixed

- **`splitCamelCase` end-of-acronym rule now requires 3+ consecutive uppercase letters.** Two
  uppercase letters followed by a lowercase word is a PascalCase boundary, not an acronym edge.
  Previously, `JQueryStatic` decomposed into `["J", "Query", "Static"]`, which caused the
  `jQueryStatic` pollution problem documented in
  [celeste-stopwords Issue #1](https://github.com/whykusanagi/celeste-stopwords/blob/main/docs/KNOWN_QUALITY_ISSUES.md):
  searches for "query" would match `JQueryStatic` via the stray `query` token, and ~1,650 other
  identifiers exhibited the same bug across a 31-repo training corpus. `HTTPServer`,
  `HTMLToMarkdown`, `CSVParser`, and every other 3+ consecutive-uppercase acronym still splits
  correctly.

  Affected identifier families now atomize correctly:
  - `JQuery` / `JQueryStatic` / `JQueryElement` / `JQueryPromise`
  - `IFoo` / `IArguments` / `IPromise` / `ICache` / any `I`-prefixed TypeScript interface
  - `IPv4` / `IPv6` / `VNode` / `ETag` / `OAuth2` / `XDist`

- **Two direct-tool MCP schema mismatches.** `celeste_code_symbols` advertised a `name` field
  that the underlying builtin tool doesn't accept; `celeste_code_graph` advertised an `action`
  discriminator that doesn't exist. Both schemas now mirror the real builtin tool parameters
  1:1 — name-based lookups go through `celeste_code_search` instead.

### CI / Release

- **Go toolchain bumped 1.26.1 → 1.26.2** in `.github/workflows/ci.yml` and `release.yml`.
  1.26.1 stdlib has 5 CVEs (`crypto/x509` × 3, `crypto/tls`, `html/template`) fixed in 1.26.2;
  `govulncheck` now returns "No vulnerabilities found".
- **Release workflow pins `CGO_ENABLED=0`** for cross-platform binary builds. Released
  artifacts ship with the pure-Go `parser_ts_stub.go` fallback for portability; users who want
  the tree-sitter improvement can `go install` from source with CGo enabled. A proper CGo
  cross-toolchain release workflow (zig cc or native-runner matrix) is queued for v2.1.0.
- **Dockerfile builder adds `build-base`** and sets `CGO_ENABLED=1` on the test-compile step
  so the codegraph test binary can link the tree-sitter C runtime. Runtime container still
  uses `CGO_ENABLED=0`; only test compilation needs the C toolchain.

### Details

- 14 commits on `feat/v1.9-quality-bundle` (PR #16) plus a schema fix direct to main plus
  PR #17 hardening `release.yml` for the tag build.
- Empirical validation archives under `celeste-stopwords/results/`:
  - `ab_test_TASK19_v1.9.0_grafana_app.txt` (path filter + reasoning metadata)
  - `ab_test_TASK20_v1.9.0_grafana_app.txt` (BM25 fused ranking)
  - `ab_test_TASK21_v1.9.0_grafana_app.txt` (stopwords runtime)
  - `ab_test_TASK24_rerank_content_control.txt` (structural rerank, shared-index A/B)
  - `ship_decision_v1.9.0.md` (full GO decision document)
- Known v2.1.0 follow-ups: #18 (tree-sitter Python), #19 (tree-sitter Rust), release workflow
  CGo cross-toolchain for pre-built TS parser.

## [1.8.0] - 2026-04-03

### Added
- **`.grimoire` Project Context**: Persona-themed project config files with auto-discovery, `@include` support, and `celeste init` auto-detection
- **Code Graph Index**: Structural code graph with Go AST parsing, regex extraction for other languages, SQLite storage via `modernc.org/sqlite`
- **Semantic Code Search**: MinHash over enriched shingles — concept-based search without embeddings or API calls
- **MCP Server Mode**: `celeste serve` exposes Celeste via stdio and authenticated SSE transports for Claude Code, Codex, or any MCP client
- **Git-Aware Context**: Startup snapshot injected into system prompt, plus `git_status` and `git_log` tools
- **Session Persistence**: JSONL append-only session logs with `celeste resume` and auto-resume
- **File Checkpointing**: Snapshots before writes, stale file detection, `/diff` session summary, `/undo` revert
- **Extended Thinking**: Provider-specific reasoning tokens (Claude, Gemini, xAI) with `/effort` command
- **Prompt Caching**: Static prefix / dynamic suffix structure for cache-friendly system prompts
- **Image Input**: Multimodal support — `read_file` detects images, base64-encodes for vision models
- **Web Search & Fetch**: DuckDuckGo search and URL-to-markdown tools (no API key required)
- **Cost Tracking**: Per-model pricing table, session cost accumulation, display in context bar
- **Hooks System**: Pre/post tool execution hooks defined in `.grimoire` with template variables
- **Plan Mode**: `/plan` enters read-only mode, writes plan file for user review, `/plan execute` runs it
- **Task Tracking**: `todo` tool for model self-management with TUI panel
- **Memory System**: Persistent learned knowledge at `~/.celeste/projects/`, heuristic extraction, staleness detection
- **Subagent Spawning**: `/spawn` and `spawn_agent` tool for foreground task delegation
- **Graceful Ctrl+C**: Single interrupt cancels current task, double exits. AbortSignal propagation through tools.
- **Build-time Version Injection**: Version, build tag, and commit SHA injected via ldflags in CI/CD

### Changed
- Version information now uses `var` instead of `const` for ldflags injection
- Release workflow injects commit SHA into binaries
- CI build artifacts include version + commit metadata

## [1.7.0] - 2026-03-31

### Added
- **Unified Tool Layer**: Single `Tool` interface replacing the old `skills/` package, with `tools/builtin/` for all implementations
- **Streaming Tool Executor**: Tools begin executing as LLM generates, with concurrent dispatch for read-only tools
- **Context Window Management**: Automatic token budget tracking, reactive/proactive compaction, tool result capping
- **Permission System**: Multi-layer allow/deny/ask rules with pattern matching, denial tracking, persistent config
- **MCP Client**: Model Context Protocol support with stdio/SSE transports for external tool servers
- **TUI Enhancements**: Tool progress indicators, context budget bar, permission prompts, MCP server panel

### Changed
- Replaced `cmd/celeste/skills/` package with `cmd/celeste/tools/` unified tool system
- All 23 built-in skills migrated to `tools/builtin/` with individual files
- All 6 dev tools migrated from `agent/dev_skills.go` to `tools/builtin/`
- LLM backends now support `SendMessageStreamEvents()` for granular streaming
- Agent runtime uses streaming events instead of sync batch execution
- Config token tracking delegates to new `context/` package

### Removed
- `cmd/celeste/skills/` package (replaced by `cmd/celeste/tools/`)
- `cmd/celeste/agent/dev_skills.go` (replaced by `tools/builtin/`)
- `cmd/celeste/llm/summarize.go` (replaced by `context/summarizer.go`)
- `cmd/celeste/config/context.go` token tracking (replaced by `context/budget.go`)

## [Unreleased - Pre-1.7]

### Added
- New autonomous `agent` command family for multi-turn task execution:
  - `celeste agent --goal ...`
  - `celeste agent --resume <run-id>`
  - `celeste agent --list-runs`
  - `celeste agent --eval <cases.json>`
- Agent runtime loop package (`cmd/celeste/agent`) with:
  - max-turn and max-tool safety controls
  - completion-marker controls (default `TASK_COMPLETE:`)
  - no-progress stop behavior
- Checkpointed long-horizon run persistence under `~/.celeste/agent/runs`.
- Agent-only development skills for coding/content workflows:
  - `dev_list_files`, `dev_read_file`, `dev_write_file`, `dev_search_files`, `dev_run_command`
- Eval harness for JSON-defined scenarios with pass/fail scoring.
- Phase 2 agent controls:
  - explicit planning phase with extracted plan steps
  - execution progress markers via `STEP_DONE: <n>`
  - verification gate via repeatable `--verify-cmd` commands and `--require-verify`
- Phase 3 agent deliverables:
  - per-run artifact bundles (`summary`, `run_state`, `plan`, `steps`, `verification`, optional git status/diff)
  - benchmark suite scaffolding via `celeste agent --benchmark <suite.json>`
  - optional benchmark JSON report export via `--benchmark-out`

### Testing
- Added new unit coverage for:
  - checkpoint save/load/list behavior
  - workspace path traversal guards
  - development skill execution paths
  - eval file parsing and result scoring

## [1.5.5] - 2026-02-27

### Fixed
- Restored tool definition delivery in chat mode so skill/function metadata is sent from the live LLM client instead of the TUI skills panel stub.
- Added multi-tool execution flow support: all tool calls returned in a single assistant turn are now executed and replied with matching `tool_call_id` results before continuation.
- Hardened tool argument parsing so malformed JSON arguments surface as explicit tool errors rather than silently falling back to empty argument maps.

### Changed
- Added schema validation for disk-loaded custom skills to reject malformed function definitions before they reach provider APIs.
- Hardened OpenAI/xAI tool serialization by skipping invalid tool payloads gracefully instead of sending malformed definitions.
- Improved Google/Vertex schema conversion compatibility for `required` fields across both `[]string` and `[]interface{}` input forms.

### Testing
- Added regression coverage for:
  - custom skill schema validation pass/fail paths
  - OpenAI and xAI tool serialization skip-on-error behavior
  - Google/Vertex `required` schema conversion compatibility
  - multi-tool TUI execution sequencing and single follow-up request semantics

## [1.5.4] - 2026-02-25

### Security
- Upgraded `go-ethereum` v1.16.8 → v1.17.0 to remediate GO-2026-4508 (DoS via malicious p2p message)

## [1.5.3] - 2026-02-25

### Added
- `upscale_image` tool call skill — upscale and enhance images via Venice.ai, now available as an LLM-callable tool in all modes (not limited to NSFW)
- `docs/plans/2026-02-24-clear-nsfw-upscale-design.md` — design doc for this release's changes
- `docs/plans/2026-02-24-clear-nsfw-upscale.md` — implementation plan used to build this release

### Changed
- `/clear` now performs a full session reset — clears chat history **and** starts a fresh session (equivalent to `/clear` + `/session new` in one command)
- Removed `upscale:` as an NSFW-mode media command; upscaling is now handled exclusively by the `upscale_image` tool call

## [1.5.2] - 2026-02-22

### Fixed
- `/tools` command from menu now correctly opens the skills browser instead of returning "Unknown command"
- `/nsfw` now toggles — typing it a second time disables NSFW mode (previously required `/safe` to exit)
- Context rot: UI notification messages (`role=system`) were being sent to the LLM on every request, bloating context with phantom system messages per session; LLM requests now only include user/assistant/tool messages
- Menu item selection now correctly executes the selected command (value-type `InputModel` mutation was being discarded)
- Skill browser selection now correctly populates the input field

### Changed
- Renamed project references from `CelesteCLI` to `Celeste CLI` across all documentation, scripts, and workflows

## [1.5.1] - 2026-02-19

### Added
- **Collections Support (xAI RAG)** - Upload custom documents for semantic search during chat
  - Create and manage collections via CLI and TUI
  - Upload documents (.md, .txt, .pdf, .html) up to 10MB each
  - Enable/disable collections for chat with active set management
  - Automatic semantic search integration with Grok models
  - 7 CLI commands: `create`, `list`, `upload`, `delete`, `enable`, `disable`, `show`
  - Interactive TUI: `/collections` command with navigation and toggle support
  - Management API client for xAI Collections API
  - High-level Collections Manager with config integration
  - Server-side RAG via xAI's built-in `collections_search` tool
  - Support for recursive directory uploads
  - Persistent configuration in `~/.celeste/config.json`
  - Document validation (format and size checking)

### Changed
- Extended `config.json` structure with Collections configuration:
  - Added `xai_management_api_key` field for Collections API authentication
  - Added `collections` object with enabled status and active collections list
  - Added `xai_features` object for xAI-specific feature flags
- Updated LLM backend to inject xAI built-in tools when collections enabled
- Enhanced TUI with collections view mode and `/collections` command
- Main application wired for Collections config propagation to LLM client

### Documentation
- Added `docs/COLLECTIONS.md` - Complete Collections user guide with:
  - Quick start tutorial for Collections setup
  - Full CLI commands reference with examples
  - TUI interface usage and keybindings
  - Best practices for organizing collections and documents
  - Troubleshooting guide for common issues
  - Advanced usage patterns (batch operations, git hooks, context switching)
  - API integration details and limitations
  - FAQ section covering common questions
- Updated `README.md` - Added Collections Support section and feature bullet
- Updated `docs/LLM_PROVIDERS.md` - Added Collections column to compatibility matrix showing xAI as only supported provider

## [1.4.0] - 2025-12-18

### Added
- **Wallet Security Monitoring** - Comprehensive wallet threat detection and alerting
  - Monitor multiple wallet addresses across networks (Ethereum, Polygon, Arbitrum, Optimism, Base)
  - Real-time polling every 5 minutes (configurable)
  - 6 wallet management operations: add, remove, list, check security, get alerts, acknowledge alerts
  - 4 threat detection types:
    - **Dust attacks** - Detect tiny value transfers (< 0.001 ETH) used for address poisoning
    - **NFT scams** - Flag unsolicited NFT transfers from unknown contracts
    - **Large transfers** - Alert on significant outgoing funds (> 1 ETH or > 10% of balance)
    - **Dangerous approvals** - Detect unlimited token approvals (2^256-1) and high-value approvals
  - Alert system with severity-based classification (critical, high, medium, low)
  - Persistent alert history stored in `~/.celeste/wallet_alerts.json`
  - Alert acknowledgment system to track reviewed threats
  - CLI commands for wallet management via `celeste skill wallet_security`
- **Background Monitoring Daemon** - Automatic wallet security monitoring
  - Run wallet checks in background at configurable intervals
  - Commands: `celeste wallet-monitor start/stop/status`
  - Fork to background process with PID file management
  - Graceful shutdown with SIGTERM handling
  - Configurable poll interval via `wallet_security_poll_interval`
  - Automatic logging of security events with timestamps
- **Token Approval Monitoring** - ERC20 approval event tracking
  - Monitor `Approval(address,address,uint256)` events via `eth_getLogs`
  - Detect unlimited approvals (max uint256 = 2^256-1)
  - Flag high-value approvals (> 1 million tokens)
  - Alert severity: HIGH for unlimited, MEDIUM for high-value
  - Track spender contracts and approved amounts
- **IPFS File Upload** - Binary file support for IPFS
  - Upload files via `--file_path` parameter
  - Support for all file types: images, PDFs, archives, audio/video, binaries
  - Automatic file size and name detection
  - Returns filename, size, type, and CID in response
  - Preserves original string content upload functionality
- **Wallet Security Storage**
  - `~/.celeste/wallet_security.json` - Monitored wallets configuration
  - `~/.celeste/wallet_alerts.json` - Security alerts history log
  - `~/.celeste/wallet_monitor.pid` - Daemon process ID
  - Automatic directory creation and file management
- **Enhanced Configuration**
  - Added `WalletSecuritySettingsConfig` with poll interval and alert level settings
  - Config fields: `wallet_security_enabled`, `wallet_security_poll_interval`, `wallet_security_alert_level`

### Changed
- Extended Alchemy integration for wallet security monitoring using `alchemy_getAssetTransfers` and `eth_getLogs` APIs
- Enhanced alert display system with severity-based styling (leveraging existing TUI components)
- Updated ConfigLoader interface with `GetWalletSecurityConfig()` method
- IPFS skill description updated to reflect file upload support

### Documentation
- Added `docs/WALLET_SECURITY.md` - Complete wallet security monitoring guide with:
  - Setup instructions for wallet monitoring
  - Threat detection patterns and explanations
  - Background daemon usage and configuration
  - Token approval monitoring details
  - Usage examples for all operations
- Updated `docs/IPFS_SETUP.md` - Added file upload documentation with examples for binary files

## [1.3.0] - 2025-12-18

### Added
- **IPFS Integration** - Decentralized content management
  - Upload and download content via IPFS (returns CID)
  - Pin management (pin, unpin, list pins)
  - Multi-provider support (Infura, Pinata, custom nodes)
  - Gateway URL generation for public access
  - Official go-ipfs-http-client library integration
- **Alchemy Blockchain API** - Comprehensive blockchain data access
  - Wallet operations: ETH/token balances, transaction history, asset transfers
  - Token data: Real-time metadata and comprehensive token information
  - NFT APIs: Query NFTs by owner, metadata, collection info
  - Transaction monitoring: Gas prices, transaction receipts, block information
  - Multi-network support: Ethereum, Arbitrum, Optimism, Polygon, Base (mainnet + testnets)
  - JSON-RPC interface with proper error handling
- **Blockchain Monitoring** - Real-time blockchain event tracking
  - Watch addresses for new transactions across multiple blocks
  - Get latest block information with transaction details
  - Query specific blocks by number (hex or decimal)
  - Asset transfer tracking (external, internal, ERC20, ERC721, ERC1155)
  - Network-specific monitoring with configurable poll intervals
- **Modern Crypto Utilities**
  - Ethereum address validation using go-ethereum (EIP-55 checksumming)
  - Wei ↔ Ether ↔ Gwei conversion helpers with big.Int precision
  - Production-ready rate limiting using golang.org/x/time/rate
  - Multi-network URL construction and validation
  - Chain ID support for all major networks
- **Enhanced Configuration System**
  - Network-specific settings for L2 support
  - Environment variable overrides for CI/CD (`CELESTE_IPFS_API_KEY`, `CELESTE_ALCHEMY_API_KEY`)
  - Flexible provider configuration (Infura, Pinata, custom endpoints)
  - Crypto-specific config fields in config.json and skills.json
  - ConfigLoader interface with GetIPFSConfig(), GetAlchemyConfig(), GetBlockmonConfig()

### Changed
- Upgraded to modern production-grade Go crypto libraries:
  - `github.com/ethereum/go-ethereum@v1.16.7` - Official Ethereum Go implementation
  - `github.com/ipfs/go-ipfs-http-client@v0.7.0` - Official IPFS HTTP client
  - `github.com/ipfs/go-cid@v0.6.0` - Content Identifier handling
  - `golang.org/x/time@v0.14.0` - Token bucket rate limiting
- Improved error handling for external API integrations
- Enhanced skills.json structure for crypto service configuration
- Better address normalization with proper checksum validation

### Documentation
- Added `docs/IPFS_SETUP.md` - Infura IPFS configuration guide
- Added `docs/ALCHEMY_SETUP.md` - Alchemy API setup and usage
- Added `docs/BLOCKCHAIN_MONITORING.md` - Real-time monitoring guide

## [1.1.0] - 2025-12-14

### Added
- **One-shot CLI commands** for all features (context, stats, export, session, config, skills)
  - Execute any command without entering TUI: `./celeste context`, `./celeste stats`
  - Direct skill execution: `./celeste skill <name> [--args]`
  - Comprehensive skill testing with `./celeste skill generate_uuid`, etc.
- **Context Management System**
  - Token usage tracking with input/output breakdown
  - Retroactive token calculation for session history
  - Context window monitoring and warnings
  - Auto-summarization when approaching limits
- **Enhanced Session Persistence**
  - Message persistence across sessions
  - Session metadata tracking (token counts, model info)
  - Improved session loading and restoration
- Interactive model selector with arrow key navigation
- Flickering corruption animation for stats dashboard
- GitHub Actions CI/CD pipeline
- Comprehensive test coverage
- Security vulnerability scanning
- Cross-platform build support

### Fixed
- **Token counting** - Now correctly displays input/output token breakdown
- **All 18 skills** - 100% functional from CLI one-shot commands:
  - Type conversion for numeric arguments (length, value, amount)
  - Parameter name corrections (encoded, text, from_timezone, etc.)
  - Weather skill accepts both string and numeric zip codes
- Session persistence and provider detection issues
- Code formatting issues
- Dependency version compatibility

### Changed
- Improved documentation structure
- Enhanced error handling
- Model selector with arrow key navigation
- Stats dashboard with corruption animation effects

### Documentation
- Added `ONESHOT_COMMANDS.md` - Complete CLI command reference
- Added `docs/TEST_RESULTS.md` - Test verification results for all skills
- Added corruption aesthetic validation guides
- Added brand system documentation (migrated to corrupted-theme package)

## [1.0.2] - 2025-12-03

### Added
- **Bubble Tea TUI**: Complete rewrite with flicker-free terminal UI
  - Scrollable chat viewport with PgUp/PgDown navigation
  - Input history with arrow key navigation
  - Real-time skills panel showing execution status
  - Corrupted theme styling (pink/purple aesthetic)
- **Named Configurations**: Multi-profile config support
  - `celeste -config openai chat` for OpenAI
  - `celeste -config grok chat` for xAI/Grok
  - Template system for quick config creation
- **Skills System**: OpenAI function calling support
  - Tarot reading (3-card and Celtic Cross)
  - NSFW mode (Venice.ai integration)
  - Content generation (Twitter, TikTok, YouTube, Discord)
  - Image generation (Venice.ai)
  - Weather lookup
  - Unit/timezone/currency converters
  - Hash/Base64/UUID/Password generators
  - QR code generation
  - Twitch live status checking
  - YouTube video lookup
  - Reminders and notes
- **Session Management**: Conversation persistence
  - Auto-save and resume sessions
  - Session listing and loading
  - Message history with timestamps
- **Simulated Typing**: Smooth text rendering
  - Configurable typing speed
  - Corruption effects during typing
  - Better UX for streamed responses

### Changed
- **Architecture**: Modular package structure
  - `cmd/Celeste/tui/` - Bubble Tea components
  - `cmd/Celeste/llm/` - LLM client
  - `cmd/Celeste/config/` - Configuration management
  - `cmd/Celeste/skills/` - Skills registry and execution
  - `cmd/Celeste/prompts/` - System prompts
- **Configuration**: JSON-based config system
  - Migrated from `.celesteAI` to `~/.celeste/config.json`
  - Separate `secrets.json` for sensitive data
  - Environment variable override support
- **Binary Name**: Renamed from `celestecli` to `Celeste`

### Removed
- Legacy main_old.go (3,481 lines)
- Old configuration format
- Deprecated Python utilities

### Fixed
- API key exposure in error messages
- Config file permission issues
- Session not saving in some scenarios
- Weather skill error handling

### Security
- Added SECURITY.md with vulnerability reporting process
- Implemented secret masking in config display
- Improved API key storage with separate secrets file
- Added .gitignore protection for sensitive files

## [2.0.0] - Previous Release

### Added
- Initial CLI implementation
- Basic LLM integration
- Configuration file support

## [1.0.0] - Initial Release

### Added
- Basic functionality
- Simple command-line interface

---

## Release Links

- [Unreleased](https://github.com/whykusanagi/celeste-cli/compare/v1.5.2...HEAD)
- [1.5.2](https://github.com/whykusanagi/celeste-cli/compare/v1.5.1...v1.5.2)
- [1.5.1](https://github.com/whykusanagi/celeste-cli/compare/v1.4.0...v1.5.1)
- [1.4.0](https://github.com/whykusanagi/celeste-cli/compare/v1.3.0...v1.4.0)
- [1.3.0](https://github.com/whykusanagi/celeste-cli/compare/v1.1.0...v1.3.0)
- [1.1.0](https://github.com/whykusanagi/celeste-cli/compare/v1.0.2...v1.1.0)
- [1.0.2](https://github.com/whykusanagi/celeste-cli/releases/tag/v1.0.2)
- [1.0.0](https://github.com/whykusanagi/celeste-cli/releases/tag/v1.0.0)

## How to Update

### From 0.x to 1.0+

The configuration format has changed:

**Old format** (`.celesteAI`):
```
api_key=sk-xxx
base_url=https://api.openai.com/v1
```

**New format** (`~/.celeste/config.json`):
```json
{
  "api_key": "",
  "base_url": "https://api.openai.com/v1",
  "model": "gpt-4o-mini",
  "timeout": 60,
  "skip_persona_prompt": false,
  "simulate_typing": true,
  "typing_speed": 40
}
```

**Migration steps**:
1. Backup your old config: `cp ~/.celesteAI ~/.celesteAI.backup`
2. Install new version: `make install`
3. Run config migration: `celeste config --show` (auto-migrates)
4. Verify settings: `celeste config --show`
5. Test: `celeste chat`

### Breaking Changes in 1.0+

- Command name changed from `celestecli` to `Celeste`
- Config file location changed to `~/.celeste/`
- Session format incompatible with 2.x (will create new sessions)
- Some command flags renamed for consistency

---

## Support

- **Issues**: [GitHub Issues](https://github.com/whykusanagi/celeste-cli/issues)
- **Security**: See [SECURITY.md](SECURITY.md)
- **Contributing**: See [CONTRIBUTING.md](CONTRIBUTING.md)
