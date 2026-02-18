# Celeste ↔ NIKKE Sub-Agent Routing Architecture

## Overview

Celeste uses intelligent intent detection to route queries:
- **NIKKE queries** → NIKKE sub-agent (dedicated for game-specific data)
- **General queries** → Main Celeste agent (personality + streaming + general chat)

This separation reduces context window bloat and improves response accuracy for both contexts.

---

## How Routing Works

```
User Input
    ↓
[celeste-widget.js: Fetch routing rules]
    ↓
[Detect Intent: NIKKE keywords/patterns?]
    ├─ YES (NIKKE detected)
    │  └─ Route to NIKKE sub-agent
    │     └─ Return formatted response
    └─ NO (General query)
       └─ Send to Main Celeste agent
          └─ Celeste responds
```

---

## Intent Detection

### Keywords Trigger NIKKE Routing
```
"nikke", "union raid", "ur", "intercept",
"character", "tier", "tier list", "meta", "best squad",
"farm", "build", "equipment", "weapon",
"chapter", "raid", "boss", "strategy", "guide"
```

### Pattern Matching
```regex
^(who|which).*(nikke|character).*(best|good|tier)
^(how|tips).*(farm|clear|beat)
^(what).*(meta|tier|build)
^(nikke).*(tier|rank|list)
union.*raid|ur.*guide|nikke.*build
```

### Examples

**Triggers NIKKE Routing:**
- "Who's the best NIKKE character?"
- "How do I farm efficiently?"
- "What's the current meta?"
- "Union Raid tier list"
- "Build guide for Rapture"
- "Best squad for tower?"

**Does NOT Trigger NIKKE Routing:**
- "Hi Celeste, how are you?"
- "Tell me about yourself"
- "What's your favorite outfit?"
- "Stream recommendations?"
- "Tell me some lore"

---

## Configuration Files

### `routing/routing_rules.json`
**Contains:** Intent detection patterns, NIKKE keywords, fallback messages

**Key Section:**
```json
{
  "nikke_detection": {
    "keywords": ["nikke", "union raid", ...],
    "patterns": [
      "^(who|which).*(nikke|character).*(best|good|tier)",
      ...
    ]
  },
  "routing_rules": [
    {
      "condition": "query_matches(nikke_detection)",
      "action": "route_to_sub_agent",
      "sub_agent": "nikke_agent",
      "fallback_message": "The NIKKE archives are temporarily sealed..."
    }
  ]
}
```

### `routing/nikke_agent_config.json`
**Contains:** Sub-agent endpoint, auth, timeouts, OpenSearch indices

**Key Section:**
```json
{
  "nikke_agent": {
    "endpoint": "${NIKKE_AGENT_ENDPOINT}",
    "auth": {
      "scheme": "bearer",
      "key": "${NIKKE_AGENT_KEY}"
    },
    "timeouts": {
      "connect_ms": 5000,
      "read_ms": 10000,
      "total_ms": 10000
    }
  }
}
```

---

## Environment Variables

Set these in your `.env` or deployment configuration:

```bash
# Main Celeste Agent (Digital Ocean)
AGENT_ENDPOINT=https://svzuwvds2ipgf4ysetvzxvai.agents.do-ai.run
AGENT_KEY=sk-xxxxx

# NIKKE Sub-Agent
NIKKE_AGENT_ENDPOINT=https://nikke-agent.whykusanagi.xyz/v1/nikke/query
NIKKE_AGENT_KEY=sk-nikke-xxxxx
```

---

## Web Widget Integration

The `celeste-widget.js` loads routing rules from celesteCLI repo:

```javascript
const CELESTE_CONFIG = {
  routingRulesUrl: 'https://raw.githubusercontent.com/whykusanagi/celesteCLI/main/routing/routing_rules.json'
};

async function sendToCeleste(message) {
  // Fetch routing rules
  const routingRules = await fetch(CELESTE_CONFIG.routingRulesUrl).then(r => r.json());

  // Detect intent
  const intent = detectIntent(message, routingRules);

  // Route accordingly
  if (intent === 'nikke') {
    // Call NIKKE sub-agent
  } else {
    // Call main Celeste agent
  }
}
```

---

## Fallback Behavior

If the NIKKE sub-agent is unavailable:

```
User Query (NIKKE detected)
    ↓
[Attempt NIKKE sub-agent]
    ├─ Timeout after 10 seconds
    └─ Return fallback message:
       "The NIKKE archives are temporarily sealed. Ask again in a moment, onii-chan."
```

The widget gracefully degrades instead of hanging or throwing an error.

---

## OpenSearch Integration

### Celeste Indices
- `celeste_capabilities` - What Celeste can do
- `celeste_emotes` - Emote recommendations
- `celeste_user_profiles` - User behavior data
- `celeste_chat_logs` - Sample interactions

### NIKKE Sub-Agent Indices
- `nikke_characters` - Character database
- `nikke_tiers` - Tier lists
- `nikke_guides` - Build guides
- `nikke_union_data` - Protected union data

Each agent queries its own indices only. No cross-contamination.

---

## Response Handling

### NIKKE Sub-Agent Response
The sub-agent returns structured data:
```json
{
  "characters": [...],
  "tiers": [...],
  "builds": [...],
  "recommendations": [...],
  "source_indices": [...]
}
```

Main Celeste agent formats this with personality:
```
"Based on the archives, here's what I found:
• Top DPS: Rapi, Viper, Privaty
• Support Meta: Modernia, Ether, Helm

Want me to explain any builds, onii-chan? 💜"
```

### Celeste Direct Response
Main agent responds directly with personality:
```
"Hi there! I'm Celeste, your demon noble co-host.
I can chat, moderate chat, play games... or tease you about your questionable taste. 😈
What'll it be?"
```

---

## Monitoring

### Log Entry Example
```
[2025-11-21 14:32:10] User: "best nikke characters?"
[2025-11-21 14:32:10] Intent Detection: MATCHED (keyword="nikke")
[2025-11-21 14:32:10] Routing: NIKKE_AGENT
[2025-11-21 14:32:10] Sub-agent latency: 342ms
[2025-11-21 14:32:10] Response: [formatted with Celeste personality]
```

### Health Checks
```bash
# Check main Celeste agent
curl ${AGENT_ENDPOINT}/health

# Check NIKKE sub-agent
curl ${NIKKE_AGENT_ENDPOINT}/v1/health
```

---

## Troubleshooting

### Symptom: NIKKE query routed to Celeste instead of sub-agent
**Possible Causes:**
- Routing rules not loaded correctly
- Intent detection pattern doesn't match query
- Widget JavaScript error

**Fix:**
1. Verify routing rules fetch succeeds
2. Check browser console for JavaScript errors
3. Manually test intent detection regex

### Symptom: NIKKE queries timeout
**Possible Causes:**
- Sub-agent endpoint unreachable
- Timeout too short (10s default)
- OpenSearch query is slow

**Fix:**
1. Check `nikke_agent_config.json` endpoint
2. Verify NIKKE_AGENT_ENDPOINT env var is set
3. Test sub-agent health endpoint

### Symptom: Fallback message appears frequently
**Possible Causes:**
- NIKKE sub-agent is down
- Network connectivity issue
- Timeout threshold too low

**Fix:**
1. Check NIKKE sub-agent deployment status
2. Increase timeout in `nikke_agent_config.json` if needed
3. Review error logs for specific failures

---

## Adding New Routing Rules

To route a new query type (e.g., art questions to art agent):

1. **Edit `routing/routing_rules.json`:**
```json
{
  "condition": "query_intent == 'art'",
  "action": "route_to_sub_agent",
  "sub_agent": "art_agent",
  "endpoint_env_var": "ART_AGENT_ENDPOINT"
}
```

2. **Edit `routing/nikke_agent_config.json`** to add the new agent config

3. **Update detection keywords/patterns** in `routing_rules.json`

4. **Deploy** the updated files

---

## Performance Targets

- **Intent detection:** <10ms
- **NIKKE sub-agent call:** <10 seconds (timeout)
- **Fallback response:** <100ms
- **General Celeste response:** <5 seconds

---

## Related Documentation

- `../celeste_essence.json` - System prompt with routing logic
- `../Celeste_Capabilities.json` - What Celeste can do
- `../opensearch/README.md` - Index management
- `../../nikke-agent/docs/` - NIKKE sub-agent documentation
