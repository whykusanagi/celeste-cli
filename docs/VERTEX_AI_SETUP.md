# Vertex AI Setup for Celeste CLI - COMPLETED ✅

## Summary

Vertex AI has been successfully configured for your Celeste CLI on **2025-12-15**.

## Configuration Details

- **Project ID**: `celesteai-480304`
- **Location**: `us-central1`
- **Model**: `gemini-2.0-flash`
- **Config File**: `~/.celeste/config.vertex.json`
- **Authenticated As**: `anthonygtellez@gmail.com`

## How to Use

### Start a Chat Session

```bash
./celeste --config vertex chat
```

### One-Shot Query

```bash
./celeste --config vertex chat --once "Your question here"
```

### Test Function Calling

```bash
./celeste --config vertex chat
> Can you convert 100 USD to EUR?
```

## Token Management

**IMPORTANT**: Access tokens expire after **1 hour**.

### Refresh Token Before Each Session

```bash
~/bin/celeste-refresh-vertex-token.sh
./celeste --config vertex chat
```

### Or Combined

```bash
~/bin/celeste-refresh-vertex-token.sh && ./celeste --config vertex chat
```

### Manual Token Refresh

If the script fails, manually refresh:

```bash
# Get new token
ACCESS_TOKEN=$(gcloud auth application-default print-access-token)

# Update config
./celeste config --config vertex --set-key "$ACCESS_TOKEN"
```

## Enabled APIs

✅ `aiplatform.googleapis.com` - Vertex AI API
✅ `generativelanguage.googleapis.com` - Generative AI API

## Pricing

- **Gemini 2.0 Flash**: $0.075 per 1M input tokens, $0.30 per 1M output tokens
- **Gemini 1.5 Pro**: $1.25 per 1M input tokens, $5.00 per 1M output tokens

See: https://cloud.google.com/vertex-ai/generative-ai/pricing

## Comparison: Gemini API vs Vertex AI

You now have **both** configured:

| Feature | Gemini API (`config.gemini.json`) | Vertex AI (`config.vertex.json`) |
|---------|----------------------------------|----------------------------------|
| Setup | ✅ Simple API key | ✅ OAuth access token (1hr expiry) |
| Authentication | Static key | Refreshable token |
| Quotas | 60 RPM | Higher (configurable) |
| Billing | None/credit card | GCP billing account |
| Best for | Testing, personal use | Production, enterprises |
| **Usage** | `./celeste --config gemini chat` | `./celeste --config vertex chat` |

## Your Available Configs

```bash
# List all configs
ls -1 ~/.celeste/config*.json

# Current configs:
# - config.json (default - DigitalOcean)
# - config.gemini.json (Google AI Studio)
# - config.vertex.json (Google Cloud Vertex AI) ⭐ NEW
# - config.grok.json (xAI Grok)
# - config.openai.json (OpenAI)
# - config.openrouter.json (OpenRouter)
# - config.elevenlabs.json (ElevenLabs)
```

## Troubleshooting

### Token Expired Error

If you see authentication errors:

```bash
~/bin/celeste-refresh-vertex-token.sh
```

### SSL/Network Errors

If token refresh fails with SSL errors:

```bash
# Re-authenticate
gcloud auth application-default login

# Then try again
~/bin/celeste-refresh-vertex-token.sh
```

### Permission Denied (403)

Grant yourself Vertex AI permissions:

```bash
PROJECT_ID="celesteai-480304"
USER_EMAIL="anthonygtellez@gmail.com"

gcloud projects add-iam-policy-binding $PROJECT_ID \
  --member="user:$USER_EMAIL" \
  --role="roles/aiplatform.user"
```

### API Not Enabled Error

```bash
gcloud services enable aiplatform.googleapis.com
gcloud services enable generativelanguage.googleapis.com
```

## Testing Checklist

- [x] Authentication configured
- [x] APIs enabled
- [x] Config file created
- [x] Basic chat test successful
- [ ] Function calling test (try: "convert 100 USD to EUR")
- [ ] Token refresh script works

## Next Steps

1. **Test function calling** to verify skills work on Vertex AI
2. **Test streaming** - Celeste uses streaming by default
3. **Monitor usage** in GCP Console: https://console.cloud.google.com/
4. **Set up budget alerts** if needed

## GCP Console Links

- **Vertex AI Dashboard**: https://console.cloud.google.com/vertex-ai?project=celesteai-480304
- **API Usage**: https://console.cloud.google.com/apis/dashboard?project=celesteai-480304
- **Billing**: https://console.cloud.google.com/billing?project=celesteai-480304

## Support

If you encounter issues:

1. Check token hasn't expired: `~/bin/celeste-refresh-vertex-token.sh`
2. Verify authentication: `gcloud auth list`
3. Check API status: `gcloud services list --enabled | grep -E "aiplatform|generative"`
4. View Celeste logs: Check terminal output for error messages

---

**Status**: ✅ **OPERATIONAL** - Tested and working as of 2025-12-15

**Last Token Refresh**: Check `~/.celeste/config.vertex.json` for timestamp
