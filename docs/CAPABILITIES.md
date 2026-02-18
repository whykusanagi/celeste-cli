# What Can Celeste Do?

Celeste is implemented across 7 different projects and platforms. Here's what you can do with her:

## 🎭 Main Chat & Personality

- **Interactive Chat** - Talk to Celeste with personality responses
- **Streaming Mode** - Live stream integration with Twitch chat moderation
- **Text-to-Speech** - Convert responses to audio (ElevenLabs TTS)
- **User Profiling** - Track behavior and personalize interactions
- **Behavior Scoring** - Rate user interactions from 0-100

## 📱 Platforms & Channels

### Web
- **Website Chat Widget** - Chat on whykusanagi.xyz (all pages context-aware)
- **Art Gallery** - Browse commissioned artwork with NSFW filtering
- **Character References** - View Celeste design documentation
- **Social Links** - Aggregated links to all social platforms

### Streaming
- **Twitch Integration** - Chat moderation, personality injection, events
- **Stream Announcements** - Hype stream starts with personality
- **Channel Point Redemptions** - Custom interactive rewards
- **Chat History** - Learn user patterns for personalized responses

### Content Creation
- **Social Media Posts** - Generate tweets, TikTok captions, YouTube scripts
- **Ad Reads** - Product promotion in Celeste's voice (Otaku Tears)
- **Stream Titles** - Auto-generate attention-grabbing titles
- **Discord Announcements** - Server-wide updates with personality

### Gaming
- **Union Raid (NIKKE)** - Game companion for NIKKE: Goddess of Victory
  - Character database & stats
  - Team building recommendations
  - Tier lists by strategy
  - Farm route guides
  - Raid strategies & guides
  - Interception combat tips

### Communication Platforms
- **Discord Bot** - Custom commands and chat integration
  - `/vndb` - Visual novel lookups
  - `/anilist` - Anime/manga lookups
  - `/steamcharts` - Game statistics
  - `/illustration` - Art recommendations
  - Chat responses with personality

### Specialized
- **Flipper Zero App** - Custom app with animations and personality
- **CLI Tool (Go)** - Command-line interface for all features

---

## 🧠 Knowledge & Intelligence

- **OpenSearch Integration** - Fast retrieval of facts and data
- **User Profile Lookup** - Fetch behavior history and patterns
- **Emote RAG** - Context-aware emoji/emote recommendations
- **Lore Awareness** - Reference "Abyss" lore (without revealing secrets)
- **External Data** - VNDB (anime), IGDB (games), Steam API

---

## 🎨 Content Generation

### Text Content
- Tweets (≤280 chars with hooks + CTAs)
- TikTok captions (engaging with emotes)
- YouTube scripts (hook → context → breakdown → recap → CTA)
- Discord announcements (server updates)
- Twitch titles (attention-grabbing)
- Birthday messages, quote tweets, snark replies

### Game Content (via Union Raid)
- Character recommendations
- Equipment build guides
- Squad composition analysis
- Farm efficiency strategies
- Raid boss strategies
- Tier list generation

### Ad Reads
- Otaku Tears promotion with code "whykusanagi"
- Playful product integration
- Wink-nudge humor

---

## 🎯 Behavioral Features

### Chat Moderation
- Real-time behavior scoring
- Playful warnings (policy-safe)
- User infractions tracking
- Channel point redemptions (e.g., "Beg for Mercy")

### Personality Adaptation
- **Score ≥20:** Ruthless, dominant, dismissive
- **Score ≥10:** Full insult mode with mocking
- **Score ≥5:** Sassy, playful, corrective
- **Score <5:** Chaotic, seductive, expressive

### Interaction Styles
- Gaslighting/teasing (playful denial)
- Hype announcements (kinetic energy)
- Playful roasts (policy-safe)
- Wholesome comfort (sincere reassurance)
- Lore teasing (foreshadow without revealing)
- Corrective clapback (fix misinformation)

---

## 🚀 Advanced Features

### Routing to Sub-Agents
- **NIKKE Sub-Agent:** Game-specific queries about characters, tiers, builds
- Automatic intent detection
- Graceful fallbacks if unavailable
- Seamless response formatting

### Environment & Config
- Secure API key injection via Cloudflare Workers
- Environment-based configuration (no keys in code)
- Auto-deployment from GitLab
- Health monitoring and uptime checks

### Identity & Security
- Law 0 verification (PGP signatures for Kusanagi)
- Follower status validation
- Protected data access (encrypted)
- Audit logging

---

## 📊 Analytics & Monitoring

- Chat event logging (messages, subs, raids, etc.)
- User behavior tracking
- Response quality scoring (0-100)
- Tone vector analysis (tease/menace/affection/hype)
- Content archetype classification

---

## 🛠️ Developer Features

- **CLI Tool:** Direct command-line access to all functions
- **API Contracts:** Well-defined routes and schemas
- **SDK Guidance:** Go implementation patterns
- **Comprehensive Documentation:** This file + 50+ capability details
- **OpenSearch Integration:** RAG-based knowledge retrieval

---

## ❌ What Celeste WON'T Do

- Reveal secret Abyss lore (parentage, late-stage twists)
- Participate in real-world harassment or threats
- Provide explicit pornographic content or instructions
- Encourage self-harm or dangerous behavior
- Store sensitive PII beyond usernames and IDs
- Violate platform terms of service

---

## 🔗 Where to Find Celeste

| Platform | URL | Feature |
|----------|-----|---------|
| **Website** | https://whykusanagi.xyz | Chat widget on all pages |
| **Union Raid** | https://raid.whykusanagi.xyz | NIKKE game companion |
| **Twitch** | https://twitch.tv/whykusanagi | Live chat moderation |
| **Discord** | [Invite Link] | Custom commands + chat |
| **Twitter/X** | @whykusanagi | Social media posts |
| **TikTok** | @whykusanagi | Short-form video captions |
| **GitHub** | github.com/whykusanagi | Source code & CLI |

---

## 💡 Quick Tips

**Want to see Celeste in action?**
1. Visit https://whykusanagi.xyz and use the chat widget
2. Ask her "What can you do?" - she'll explain her capabilities
3. Try a NIKKE question on Union Raid
4. Join the Discord for custom commands

**Want to use her in your own project?**
1. Check out `celesteCLI` on GitHub
2. Review the API contracts in docs
3. Deploy to your own infrastructure
4. Use environment variables for API keys (never hardcode!)

**Want to understand her personality?**
1. Read `docs/PERSONALITY.md` (this repo)
2. Check `celeste_essence.json` for the core system prompt
3. See `content_archetypes.json` for content patterns
4. Review `insult_library.json` for examples of her voice

---

For detailed technical documentation, see:
- `../Celeste_Capabilities.json` - Complete capability list
- `../celeste_essence.json` - System prompt
- `../routing/routing_rules.json` - Routing logic
- `PERSONALITY.md` - Personality quick ref
- `ROUTING.md` - Sub-agent architecture
