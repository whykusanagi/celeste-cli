# Celeste One-Shot Commands Reference

All Celeste functionality is now available as one-shot CLI commands without entering the TUI. Perfect for scripting, testing, and quick operations.

## Core Commands

### Session & Context

```bash
# Show token usage from most recent session
./celeste context
./celeste context status

# Display analytics dashboard with corruption theme
./celeste stats

# Export session data
./celeste export          # Export most recent session
./celeste export sessions # Export all sessions
```

### Session Management

```bash
# List all sessions
./celeste session --list

# Load a specific session
./celeste session --load <session-id>

# Clear all sessions
./celeste session --clear
```

### Configuration

```bash
# Show current configuration
./celeste config --show

# List all config profiles
./celeste config --list

# Initialize new config
./celeste config --init <name>

# Set API key
./celeste config --set-key <key>
```

## Provider Management

Query and list AI providers:

```bash
# List all providers
./celeste providers

# List only tool-capable providers
./celeste providers --tools

# Show detailed provider information
./celeste providers info openai
./celeste providers info grok
./celeste providers info anthropic

# Show current provider
./celeste providers current
```

## Skills Management

Manage skills without entering TUI:

```bash
# List all skills
./celeste skills --list

# Show skill information
./celeste skills --info generate_uuid
./celeste skills --info get_weather

# Delete a custom skill
./celeste skills --delete my_custom_skill

# Reload skills from disk
./celeste skills --reload

# Initialize default skill files
./celeste skills --init
```

## Skill Execution

All 18 built-in skills can be executed directly:

### No Arguments

```bash
# Generate UUID
./celeste skill generate_uuid

# Generate password (default 16 chars)
./celeste skill generate_password

# List all notes
./celeste skill list_notes

# List all reminders
./celeste skill list_reminders
```

### With Arguments

```bash
# Generate password with custom length
./celeste skill generate_password --length 20

# Get weather for specific zip
./celeste skill get_weather --zip_code 90210

# Convert units
./celeste skill convert_units --value 100 --from_unit fahrenheit --to_unit celsius

# Convert currency
./celeste skill convert_currency --amount 100 --from_currency USD --to_currency EUR

# Save a note
./celeste skill save_note --title "My Note" --content "Note content here"

# Get a note
./celeste skill get_note --title "My Note"

# Set reminder (format: YYYY-MM-DD HH:MM or just HH:MM for today)
./celeste skill set_reminder --message "Call mom" --time "2024-12-15 14:00"

# Tarot reading
./celeste skill tarot_reading --spread three_card

# Generate QR code
./celeste skill generate_qr_code --text "https://example.com"

# Base64 decode
./celeste skill base64_decode --encoded "SGVsbG8gV29ybGQ="

# Hash data
./celeste skill generate_hash --text "mydata" --algorithm "sha256"

# Convert timezone
./celeste skill convert_timezone --time "14:30" --from_timezone "America/New_York" --to_timezone "America/Los_Angeles"

# Get YouTube videos
./celeste skill get_youtube_videos --channel "@someChannel"

# Check Twitch status
./celeste skill check_twitch_live --streamer "someTwitchUser"
```

### Alternative Syntax

```bash
# Using --exec flag
./celeste skills --exec generate_uuid
./celeste skills --exec generate_password --length 20
```

## Available Skills

Run `./celeste skills --list` to see all 18 built-in skills:

1. **set_reminder** - Set a reminder with time and message
2. **get_youtube_videos** - Get recent videos from a YouTube channel
3. **list_reminders** - List all active reminders
4. **save_note** - Save a note with optional title
5. **tarot_reading** - Generate tarot card reading (three-card or celtic cross)
6. **base64_decode** - Decode a base64 string
7. **generate_uuid** - Generate random UUID (v4)
8. **generate_password** - Generate secure random password
9. **get_note** - Retrieve a note by title
10. **list_notes** - List all saved notes
11. **generate_qr_code** - Generate QR code from text/URL
12. **get_weather** - Get weather forecast for location
13. **convert_units** - Convert between units
14. **convert_currency** - Convert between currencies
15. **convert_timezone** - Convert between timezones
16. **generate_hash** - Generate cryptographic hash
17. **base64_encode** - Encode data to base64
18. **check_twitch_live** - Check if Twitch streamer is live

## Testing & Development

One-shot commands are perfect for:

```bash
# Quick testing of token tracking
./celeste context

# Testing corruption rendering
./celeste stats

# Testing skill execution
./celeste skill generate_uuid

# Scripting with skills
UUID=$(./celeste skill generate_uuid | jq -r '.uuid')
echo "Generated: $UUID"

# Export data for backup
./celeste export

# Check session history
./celeste session --list
```

## Combining with TUI

You can still use the TUI for interactive work:

```bash
# Enter interactive mode
./celeste chat

# Or send a single message
./celeste message "What's the weather?"
```

All slash commands in the TUI (/context, /stats, /export, etc.) are now also available as standalone CLI commands for maximum flexibility!

## Testing Framework

Test all one-shot commands automatically:

### Local Testing

```bash
# Run the test suite
./test/test_oneshot_commands.sh

# Test with custom binary location
CELESTE_BIN=./celeste ./test/test_oneshot_commands.sh
```

### Docker Testing

Test in an isolated container environment:

```bash
# Build and run tests
docker build -f Dockerfile.oneshot --target tester -t celeste-test .
docker run celeste-test

# Or use docker-compose
docker-compose -f docker-compose.oneshot.yml up oneshot-test

# Interactive testing container
docker-compose -f docker-compose.oneshot.yml run oneshot-runtime

# Inside the container, test commands:
./celeste providers
./celeste skills --list
./celeste skill generate_uuid
```

### Test Coverage

The test framework (`test/test_oneshot_commands.sh`) validates:

- ✓ Version and help commands
- ✓ Configuration commands (--show, --list)
- ✓ Skills management (--list, --info, --reload, --delete)
- ✓ Provider commands (list, --tools, info, current)
- ✓ Session commands (--list)
- ✓ Context and stats commands
- ✓ Skill execution (generate_uuid, generate_password, etc.)

### CI/CD Integration

Add to your CI pipeline:

```yaml
# .github/workflows/test.yml
- name: Build Celeste
  run: go build -o celeste cmd/celeste/*.go

- name: Run One-Shot Tests
  run: ./test/test_oneshot_commands.sh
```
