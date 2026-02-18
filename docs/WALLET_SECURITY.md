# Wallet Security Monitoring Guide

## Overview

Monitor your Ethereum wallets for security threats including dust attacks, NFT scams, dangerous token approvals, and large transfers. Get real-time alerts when suspicious activity is detected on your monitored wallets.

**Supported Networks**: Ethereum, Polygon, Arbitrum, Optimism, Base (mainnet + testnets)

## Setup

### 1. Configure Alchemy API Key

Wallet security monitoring uses the Alchemy API to track blockchain transactions:

```bash
# Add to ~/.celeste/config.json:
# "alchemy_api_key": "your_alchemy_key"
```

Or configure via config file:
```bash
./celeste config --set-alchemy-api-key YOUR_KEY
```

**Get an Alchemy API key**: Sign up at [alchemy.com](https://alchemy.com) and create a free account. The free tier includes sufficient requests for wallet monitoring.

### 2. Add Wallets to Monitor

```bash
./celeste skill wallet_security --operation add_monitored_wallet \
  --address 0xYourWalletAddress \
  --label "Main Wallet" \
  --network eth-mainnet
```

**Example**:
```bash
./celeste skill wallet_security --operation add_monitored_wallet \
  --address 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 \
  --label "Vitalik's Wallet" \
  --network eth-mainnet
```

### 3. Check Wallet Security

```bash
./celeste skill wallet_security --operation check_wallet_security
```

This command checks all monitored wallets for threats since the last check.

## Threat Detection

### 1. Dust Attacks

**Pattern**: Tiny amounts sent to your wallet (< 0.001 ETH or < $1 in tokens)

**Risk**: Address poisoning - Attacker sends tiny amounts hoping you'll copy their address from your transaction history when making future transfers. The malicious address often looks similar to addresses you've interacted with before.

**Alert Severity**: Low

**Detection Logic**:
- Incoming transfers < 0.001 ETH for native currency
- Incoming transfers < 1 token for ERC20 tokens
- From addresses with low transaction counts (likely burner wallets)

**Example Alert**:
```
ℹ️  LOW - Potential dust attack: Received tiny amount (0.000001 ETH) from 0xabcd...
   Wallet: 0xYour...Wallet
   Tx: 0x123...abc
   Block: 24042500
   Alert ID: alert_1702900000_a1b2c3d4
```

**What to Do**:
1. Don't panic - dust attacks are common and mostly harmless
2. Be careful when copying addresses from transaction history
3. Always verify recipient addresses before sending funds
4. Acknowledge the alert once reviewed

### 2. NFT Scams

**Pattern**: Unsolicited ERC721/ERC1155 NFTs from unknown contracts

**Risk**: Scam NFTs often link to malicious websites in their metadata. They may impersonate legitimate projects to steal funds or private keys.

**Alert Severity**: Medium

**Detection Logic**:
- Incoming NFT transfers (ERC721 or ERC1155)
- From addresses not in your whitelist (all NFTs flagged in MVP)
- Unknown contract addresses

**Example Alert**:
```
⚠️  MEDIUM - Unsolicited NFT received from contract 0x1234... (potential scam)
   Wallet: 0xYour...Wallet
   Token ID: 5678
   Tx: 0x456...def
   Block: 24042505
   Alert ID: alert_1702900100_e5f6g7h8
```

**What to Do**:
1. **Never** click links in NFT metadata
2. **Never** connect your wallet to unknown websites
3. Don't attempt to sell or transfer scam NFTs (may contain malicious code)
4. Use a burner wallet to interact with unknown NFTs if you must
5. Acknowledge benign NFT transfers (gifts from friends, airdrops from known projects)

### 3. Dangerous Token Approvals

**Pattern**: Unlimited or very high token approvals to contracts

**Risk**: Approved contracts can drain all your tokens without further permission.

**Alert Severity**: High

**Status**: Coming in future version (requires event log monitoring via `eth_getLogs`)

**Future Detection Logic**:
- Monitor `Approval(address indexed owner, address indexed spender, uint256 value)` events
- Flag unlimited approvals (max uint256)
- Flag high-value approvals to unknown contracts
- Track approval age (old unused approvals are risky)

**Planned Example**:
```
🚨 HIGH - Unlimited token approval detected for USDC to 0x9abc...
   Approved Amount: Unlimited (2^256-1)
   Contract: 0x9abc...def (Unknown)
   Recommendation: Revoke approval immediately
```

### 4. Large Transfers

**Pattern**: Significant funds leaving wallet (> 1 ETH or > 10% of balance)

**Risk**: Unauthorized transaction, compromised wallet, or accidental high-value transfer.

**Alert Severity**: High to Critical

**Detection Logic**:
- Outgoing transfers > 1 ETH
- Outgoing transfers > 10% of current balance (High severity)
- Outgoing transfers > 50% of current balance (Critical severity)
- Token transfers > 1000 tokens (heuristic)

**Example Alerts**:
```
🚨 CRITICAL - Large outgoing transfer: 5.0 ETH sent to 0x5678...
   Wallet: 0xYour...Wallet
   Amount: 5.0 ETH (75% of balance)
   To: 0x5678...xyz
   Tx: 0x789...ghi
   Block: 24042510
   Alert ID: alert_1702900200_i9j0k1l2
```

```
⚠️  HIGH - Large outgoing transfer: 1.5 ETH sent to 0xabc...
   Amount: 1.5 ETH (15% of balance)
```

**What to Do**:
1. Verify you authorized this transaction
2. Check the destination address carefully
3. If unauthorized:
   - Transfer remaining funds to a new wallet immediately
   - Revoke all token approvals
   - Investigate how your private key was compromised
4. If authorized, acknowledge the alert

## Usage Examples

### List Monitored Wallets

```bash
./celeste skill wallet_security --operation list_monitored_wallets
```

**Output**:
```json
{
  "success": true,
  "wallets": [
    {
      "address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
      "label": "Main Wallet",
      "network": "eth-mainnet",
      "added_at": "2025-12-18T10:30:00Z"
    }
  ],
  "count": 1,
  "message": "Monitoring 1 wallet(s)"
}
```

### Remove Wallet

```bash
./celeste skill wallet_security --operation remove_monitored_wallet \
  --address 0xYourWalletAddress
```

To remove from a specific network only:
```bash
./celeste skill wallet_security --operation remove_monitored_wallet \
  --address 0xYourWalletAddress \
  --network eth-mainnet
```

### View All Alerts

```bash
./celeste skill wallet_security --operation get_security_alerts
```

### View Unacknowledged Alerts Only

```bash
./celeste skill wallet_security --operation get_security_alerts --unacknowledged_only
```

**Output**:
```json
{
  "success": true,
  "alerts": [
    {
      "id": "alert_1702900000_a1b2c3d4",
      "wallet_address": "0xYour...Wallet",
      "alert_type": "dust_attack",
      "severity": "low",
      "tx_hash": "0x123...abc",
      "block_number": "24042500",
      "description": "Potential dust attack: Received tiny amount (0.000001 ETH) from 0xabcd...",
      "detected_at": "2025-12-18T10:35:00Z",
      "acknowledged": false
    }
  ],
  "count": 1,
  "message": "Found 1 alert(s)"
}
```

### Acknowledge Alert

```bash
./celeste skill wallet_security --operation acknowledge_alert \
  --alert_id alert_1702900000_a1b2c3d4
```

**Output**:
```json
{
  "success": true,
  "message": "Alert alert_1702900000_a1b2c3d4 acknowledged"
}
```

## Best Practices

1. **Monitor Regularly**: Run `check_wallet_security` every 5 minutes for active wallets. Set up a cron job:
   ```bash
   */5 * * * * cd /path/to/celeste && ./celeste skill wallet_security --operation check_wallet_security
   ```

2. **Review Alerts Daily**: Check unacknowledged alerts at least once per day:
   ```bash
   ./celeste skill wallet_security --operation get_security_alerts --unacknowledged_only
   ```

3. **Whitelist Expected Activity**: Acknowledge benign alerts (airdrops, gifts) to reduce noise

4. **Multi-Network Monitoring**: Monitor the same wallet across different chains:
   ```bash
   ./celeste skill wallet_security --operation add_monitored_wallet \
     --address 0xYourWallet --label "Main" --network eth-mainnet

   ./celeste skill wallet_security --operation add_monitored_wallet \
     --address 0xYourWallet --label "Main" --network polygon-mainnet
   ```

5. **Set Up Notifications**: Integrate with external notification services (Telegram, Discord, email) for critical alerts

6. **Rotate Monitoring**: For hot wallets, check every 5 minutes. For cold storage, check daily.

7. **Document Alert Responses**: Keep a log of how you responded to each alert for audit purposes

## Storage Files

### `~/.celeste/wallet_security.json`

Stores monitored wallets configuration:
```json
{
  "monitored_wallets": [
    {
      "address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
      "label": "Main Wallet",
      "network": "eth-mainnet",
      "added_at": "2025-12-18T10:30:00Z"
    }
  ],
  "last_checked_block": "0x16edc05",
  "poll_interval_seconds": 300
}
```

### `~/.celeste/wallet_alerts.json`

Stores security alerts history:
```json
{
  "alerts": [
    {
      "id": "alert_1702900000_a1b2c3d4",
      "wallet_address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
      "alert_type": "dust_attack",
      "severity": "low",
      "tx_hash": "0x123...abc",
      "block_number": "24042500",
      "description": "Potential dust attack...",
      "details": {
        "from_address": "0xabcd...",
        "amount": 0.000001,
        "asset": "ETH",
        "category": "external"
      },
      "detected_at": "2025-12-18T10:35:00Z",
      "acknowledged": false,
      "acknowledged_at": null
    }
  ]
}
```

## Troubleshooting

### Error: "No wallets configured for monitoring"

**Solution**: Add a wallet first using `add_monitored_wallet` operation.

```bash
./celeste skill wallet_security --operation add_monitored_wallet \
  --address 0xYourWallet --label "Main" --network eth-mainnet
```

### Error: "Alchemy API key not configured"

**Solution**: Configure your Alchemy API key in `~/.celeste/config.json`:

```json
{
  "alchemy_api_key": "your_api_key_here"
}
```

Or use the CLI config command:
```bash
celeste config --set-alchemy-api-key YOUR_KEY
```

### Too Many False Positives

**Solutions**:
1. Adjust alert level in config to only show high-severity alerts:
   ```bash
   ./celeste config --set-wallet-security-alert-level high
   ```

2. Acknowledge benign alerts to clean up the list:
   ```bash
   ./celeste skill wallet_security --operation acknowledge_alert --alert_id alert_xxx
   ```

3. For NFT scams, build a whitelist of trusted contract addresses (future feature)

### Missing Transactions in Check

**Cause**: Monitoring only checks since last checked block. If you skip checks, you may miss transactions.

**Solution**: The first check looks back 100 blocks (~20 minutes). For complete coverage, run checks every 5 minutes:
```bash
*/5 * * * * /path/to/celeste skill wallet_security --operation check_wallet_security >> /var/log/wallet_security.log 2>&1
```

### Wallet Compromised

**Immediate Actions**:
1. Stop using the compromised wallet immediately
2. Transfer all funds to a new wallet with a new private key
3. Revoke all token approvals using tools like [revoke.cash](https://revoke.cash)
4. Change passwords on all related accounts
5. Investigate how the private key was compromised

**Prevention**:
- Never share your private key or seed phrase
- Use hardware wallets for large amounts
- Enable wallet security monitoring before holding significant funds
- Keep software and wallets updated

## Configuration Options

Edit `~/.celeste/config.json` or `~/.celeste/skills.json`:

```json
{
  "wallet_security_enabled": true,
  "wallet_security_poll_interval": 300,
  "wallet_security_alert_level": "medium"
}
```

**Options**:
- `wallet_security_enabled` - Enable/disable monitoring (default: true)
- `wallet_security_poll_interval` - Seconds between checks (default: 300 = 5 minutes)
- `wallet_security_alert_level` - Minimum severity to show: "low", "medium", "high", "critical" (default: "medium")

## Advanced Usage

### Automated Monitoring Script

Create `monitor_wallets.sh`:
```bash
#!/bin/bash
set -e

# Check wallet security
OUTPUT=$(./celeste skill wallet_security --operation check_wallet_security)

# Parse result
ALERTS=$(echo "$OUTPUT" | jq -r '.alerts_found')

if [ "$ALERTS" -gt 0 ]; then
  echo "⚠️  Security Alert: $ALERTS threat(s) detected!"
  echo "$OUTPUT" | jq '.alerts'

  # Send notification (example with curl to Discord webhook)
  curl -X POST "$DISCORD_WEBHOOK_URL" \
    -H "Content-Type: application/json" \
    -d "{\"content\": \"🚨 Wallet Security Alert: $ALERTS threat(s) detected!\"}"
fi
```

### Integration with External Services

**Telegram Bot**:
```bash
# Send alert to Telegram
TELEGRAM_BOT_TOKEN="your_bot_token"
TELEGRAM_CHAT_ID="your_chat_id"

ALERTS=$(./celeste skill wallet_security --operation check_wallet_security | jq '.alerts_found')

if [ "$ALERTS" -gt 0 ]; then
  curl -X POST "https://api.telegram.org/bot$TELEGRAM_BOT_TOKEN/sendMessage" \
    -d "chat_id=$TELEGRAM_CHAT_ID" \
    -d "text=🚨 Wallet Security Alert: $ALERTS threat(s) detected!"
fi
```

## Background Monitoring Daemon

The wallet monitoring daemon runs in the background and automatically checks wallets at configured intervals without requiring manual cron jobs.

### Start the Daemon

```bash
./celeste wallet-monitor start
```

**Output**:
```
✓ Wallet monitoring daemon started (PID: 12345)
  Poll interval: 5m0s
  Check status: celeste wallet-monitor status
  Stop daemon: celeste wallet-monitor stop
```

The daemon will:
- Check all monitored wallets every 5 minutes (configurable via `wallet_security_poll_interval` in config)
- Log alerts to stdout
- Persist alerts to `~/.celeste/wallet_alerts.json`
- Run in the background until stopped

### Stop the Daemon

```bash
./celeste wallet-monitor stop
```

**Output**:
```
✓ Wallet monitoring daemon stopped (PID: 12345)
```

### Check Daemon Status

```bash
./celeste wallet-monitor status
```

**Output**:
```
Wallet monitoring daemon: running (PID: 12345, interval: 5m0s)
```

Or if not running:
```
Wallet monitoring daemon: stopped
```

### Configure Poll Interval

Edit `~/.celeste/config.json` or `~/.celeste/skills.json`:

```json
{
  "wallet_security_poll_interval": 300
}
```

**Values**:
- `300` - 5 minutes (default)
- `60` - 1 minute (active monitoring)
- `900` - 15 minutes (conservative)
- `1800` - 30 minutes (cold storage)

### Daemon Logs

The daemon logs all activity with timestamps:

```
[2025-12-18T10:35:00Z] ✓ No threats detected
[2025-12-18T10:40:00Z] ⚠️  2 security alert(s) detected!
   Run: celeste skill wallet_security --operation get_security_alerts
```

### Daemon Management Best Practices

1. **Auto-Start on Boot**: Add to your system's startup scripts
   ```bash
   # Add to ~/.bashrc or systemd service
   celeste wallet-monitor start
   ```

2. **Monitor Logs**: Redirect stdout to log file
   ```bash
   celeste wallet-monitor start > ~/wallet_monitor.log 2>&1
   ```

3. **Check Status Regularly**: Verify daemon is running
   ```bash
   celeste wallet-monitor status
   ```

4. **Restart After Config Changes**: Stop and start to apply new settings
   ```bash
   celeste wallet-monitor stop
   celeste wallet-monitor start
   ```

## Token Approval Monitoring

Token approval monitoring detects dangerous ERC20 token approvals by monitoring `Approval` events via `eth_getLogs`.

**Detection**:
- **Unlimited Approvals**: Value = 2^256 - 1 (max uint256)
  - Severity: HIGH
  - Risk: Contract can drain all tokens
- **High-Value Approvals**: Value > 1 million tokens
  - Severity: MEDIUM
  - Risk: Large approval to unknown contract

**Example Alert**:
```
🚨 HIGH - Unlimited token approval granted to 0x9abc... for contract 0x1234...
   Wallet: 0xYour...Wallet
   Token: 0xA0b8...6969 (USDC)
   Approved Amount: 115792089237316195423570985008687907853269984665640564039457584007913129639935
   Tx: 0x789...ghi
   Block: 24042520
   Alert ID: alert_1702900300_m3n4o5p6
```

**What to Do**:
1. Verify you authorized this approval (DEX swap, lending protocol, etc.)
2. If unauthorized: Immediately revoke using [revoke.cash](https://revoke.cash)
3. Check contract address on Etherscan for legitimacy
4. For legitimate protocols: Acknowledge alert to clear it

## Future Enhancements (v1.5.0)

Planned features for future releases:

- **Desktop Notifications**: Native OS notifications for critical alerts

- **Customizable Thresholds**: Set per-wallet limits for large transfer detection

- **Contract Whitelisting**: Mark trusted NFT contracts to reduce false positives

- **Multi-Wallet Dashboard**: Visual overview of all monitored wallets

- **ENS Name Integration**: Display ENS names instead of addresses

- **Historical Analytics**: Threat pattern analysis over time

- **Smart Alert Grouping**: Batch related alerts together

- **Webhook Support**: POST alerts to custom endpoints

## Security Considerations

- **API Key Security**: Never commit API keys to version control. Use environment variables.

- **Alert History**: The `wallet_alerts.json` file contains your transaction history. Keep it secure.

- **Private Key Safety**: This tool only monitors public blockchain data. Your private keys are never exposed.

- **Network Privacy**: Alchemy sees your API requests. Use a VPN for additional privacy.

- **Rate Limiting**: Free tier Alchemy accounts have rate limits. Space out checks appropriately.

## Further Reading

- [Alchemy API Documentation](https://docs.alchemy.com)
- [Ethereum Address Checksumming (EIP-55)](https://eips.ethereum.org/EIPS/eip-55)
- [Common Crypto Scams](https://support.mycrypto.com/staying-safe/common-scams)
- [Wallet Security Best Practices](https://ethereum.org/en/security/)
- [Token Approval Risks](https://kalis.me/unlimited-erc20-allowances/)

## Support

For issues or questions:
- GitHub Issues: [https://github.com/whykusanagi/celesteCLI/issues](https://github.com/whykusanagi/celesteCLI/issues)
- Documentation: [https://github.com/whykusanagi/celesteCLI/blob/main/README.md](https://github.com/whykusanagi/celesteCLI/blob/main/README.md)
