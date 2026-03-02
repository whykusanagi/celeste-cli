# Changelog

All notable changes to Celeste CLI will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
