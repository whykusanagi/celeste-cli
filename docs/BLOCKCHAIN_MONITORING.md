# Blockchain Monitoring Setup Guide

This guide explains how to configure and use the blockchain monitoring skill in Celeste CLI for real-time tracking of addresses, blocks, and network activity.

## Overview

The blockchain monitoring skill provides:
- **Address Watching**: Monitor addresses for new transactions
- **Block Tracking**: Get latest block information with transaction details
- **Historical Queries**: Fetch specific blocks by number
- **Asset Transfer Tracking**: Monitor external, internal, and token transfers (ERC20, ERC721, ERC1155)
- **Multi-Network Support**: Works across all Alchemy-supported networks

## Prerequisites

The blockchain monitoring skill uses the same Alchemy API as the Alchemy skill.

### Setup Alchemy API Key

**Method 1: Environment Variable**
```bash
# Add to ~/.celeste/config.json:
# "alchemy_api_key": "your_api_key_here"
```

**Method 2: skills.json Configuration**
Edit `~/.celeste/skills.json`:
```json
{
  "blockmon_alchemy_api_key": "your_api_key_here",
  "blockmon_default_network": "eth-mainnet",
  "blockmon_poll_interval_seconds": 15,
  "blockmon_webhook_url": ""
}
```

**Note**: If `blockmon_alchemy_api_key` is not set, it falls back to `alchemy_api_key`.

## Supported Networks

Same networks as Alchemy skill:
- **Mainnet**: eth-mainnet, polygon-mainnet, arbitrum-mainnet, optimism-mainnet, base-mainnet
- **Testnet**: eth-sepolia, polygon-amoy, arbitrum-sepolia, optimism-sepolia, base-sepolia

## Operations

### 1. Get Latest Block

Get information about the most recent block.

```bash
celeste skill blockmon \
  --operation get_latest_block \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "network": "eth-mainnet",
  "block_number": "18500000",
  "block_hex": "0x11A09A0",
  "block_hash": "0xabc123...",
  "timestamp": "0x656a1b2c",
  "transaction_count": 150,
  "miner": "0x...",
  "gas_used": "0x...",
  "gas_limit": "0x...",
  "message": "Latest block: #18500000 with 150 transactions"
}
```

**Use Cases**:
- Monitor network activity
- Check blockchain sync status
- Track block production rate
- Analyze gas usage trends

### 2. Watch Address

Monitor an address for recent transactions.

```bash
celeste skill blockmon \
  --operation watch_address \
  --address 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 \
  --network eth-mainnet \
  --blocks_history 10

# Returns:
{
  "success": true,
  "address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
  "network": "eth-mainnet",
  "blocks_checked": 10,
  "current_block": "18500000",
  "from_block": "18499990",
  "transaction_count": 3,
  "transactions": [
    {
      "from": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
      "to": "0x123...",
      "value": "1.5",
      "asset": "ETH",
      "category": "external",
      "blockNum": "0x11A0999",
      "hash": "0xdef456..."
    }
  ],
  "message": "Found 3 transactions in last 10 blocks"
}
```

**Parameters**:
- `address` (required): Ethereum address to watch
- `blocks_history` (optional): Number of past blocks to check (default: 10)

**Transaction Categories**:
- **external**: Regular ETH transfers
- **internal**: Internal transactions (contract calls)
- **erc20**: ERC20 token transfers
- **erc721**: NFT transfers
- **erc1155**: Multi-token standard transfers

**Use Cases**:
- Track wallet activity
- Monitor contract interactions
- Detect incoming payments
- Alert on specific transaction types

### 3. Get Block by Number

Fetch details for a specific block.

```bash
celeste skill blockmon \
  --operation get_block_by_number \
  --block_number 18500000 \
  --network eth-mainnet

# Or use hex format:
celeste skill blockmon \
  --operation get_block_by_number \
  --block_number 0x11A09A0 \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "network": "eth-mainnet",
  "block_number": "18500000",
  "block_hex": "0x11A09A0",
  "block_hash": "0xabc123...",
  "timestamp": "0x656a1b2c",
  "transaction_count": 150,
  "miner": "0x...",
  "gas_used": "0x...",
  "gas_limit": "0x...",
  "data": { /* full block data */ },
  "message": "Block #18500000 with 150 transactions"
}
```

**Use Cases**:
- Analyze historical blocks
- Verify transaction inclusion
- Study block timestamps
- Research miner behavior

## Advanced Usage

### Continuous Monitoring Pattern

For continuous monitoring, you can poll the watch_address operation:

```bash
# Poll every 15 seconds (configurable via blockmon_poll_interval_seconds)
while true; do
  celeste skill blockmon \
    --operation watch_address \
    --address 0xYourAddress \
    --network eth-mainnet \
    --blocks_history 1
  sleep 15
done
```

### Multi-Address Monitoring

Monitor multiple addresses by calling the skill multiple times:

```bash
# Create a monitoring script
addresses=(
  "0xAddress1"
  "0xAddress2"
  "0xAddress3"
)

for addr in "${addresses[@]}"; do
  celeste skill blockmon \
    --operation watch_address \
    --address $addr \
    --network eth-mainnet
done
```

### Cross-Chain Monitoring

Track the same address across multiple networks:

```bash
networks=("eth-mainnet" "polygon-mainnet" "arbitrum-mainnet")

for network in "${networks[@]}"; do
  celeste skill blockmon \
    --operation watch_address \
    --address 0xYourAddress \
    --network $network
done
```

## Configuration Options

### Poll Interval
Control how frequently to check for new data:

```json
{
  "blockmon_poll_interval_seconds": 15
}
```

Recommended values:
- **High-frequency**: 5-10 seconds (more API calls)
- **Standard**: 15 seconds (balanced)
- **Low-frequency**: 30-60 seconds (fewer API calls)

### Blocks History
Default number of blocks to scan when watching addresses:

```bash
# Check last 100 blocks for transactions
celeste skill blockmon \
  --operation watch_address \
  --address 0xAddress \
  --blocks_history 100
```

Trade-offs:
- **Small (1-10 blocks)**: Fast queries, recent activity only
- **Medium (10-50 blocks)**: Good balance
- **Large (50+ blocks)**: Comprehensive history, slower queries

## Use Cases

### 1. Payment Notification System
Monitor your wallet for incoming payments:

```bash
# Watch for incoming ETH or token transfers
celeste skill blockmon \
  --operation watch_address \
  --address 0xYourMerchantAddress \
  --network eth-mainnet \
  --blocks_history 5
```

### 2. Smart Contract Monitoring
Track interactions with your smart contract:

```bash
# Monitor contract address for calls
celeste skill blockmon \
  --operation watch_address \
  --address 0xYourContractAddress \
  --network eth-mainnet
```

### 3. Whale Watching
Monitor large holders for activity:

```bash
# Track whale addresses
celeste skill blockmon \
  --operation watch_address \
  --address 0xWhaleAddress \
  --network eth-mainnet \
  --blocks_history 20
```

### 4. Network Health Monitoring
Track block production and network metrics:

```bash
# Check latest block every 15 seconds
watch -n 15 'celeste skill blockmon --operation get_latest_block --network eth-mainnet'
```

### 5. Transaction Confirmation Tracking
Verify transaction inclusion in a block:

```bash
# Get block containing your transaction
celeste skill blockmon \
  --operation get_block_by_number \
  --block_number 18500000 \
  --network eth-mainnet
```

## Integration Patterns

### Webhook Integration (Future)
While not yet implemented, the `blockmon_webhook_url` configuration field is reserved for future webhook support:

```json
{
  "blockmon_webhook_url": "https://your-app.com/webhook"
}
```

This will enable push-based notifications instead of polling.

### Database Logging
Store monitoring results in a database:

```bash
celeste skill blockmon --operation watch_address --address 0x... | \
  jq '.transactions[]' | \
  while read tx; do
    # Insert into database
    mysql -e "INSERT INTO transactions VALUES (...);"
  done
```

### Alert Systems
Trigger alerts based on activity:

```bash
result=$(celeste skill blockmon --operation watch_address --address 0x...)
tx_count=$(echo $result | jq '.transaction_count')

if [ $tx_count -gt 0 ]; then
  # Send alert via email, Slack, etc.
  echo "New transactions detected!" | mail -s "Alert" you@email.com
fi
```

## Technical Details

### Asset Transfer API
The `watch_address` operation uses Alchemy's enhanced `alchemy_getAssetTransfers` API:

**Benefits**:
- Single API call for all transfer types
- Normalized data format
- Better performance than event logs
- Includes internal transactions

**Categories Tracked**:
```json
["external", "internal", "erc20", "erc721", "erc1155"]
```

### Block Number Formats
Supports both decimal and hex formats:
- Decimal: `18500000`
- Hex: `0x11A09A0`

Automatic conversion ensures compatibility with Ethereum JSON-RPC.

### Address Normalization
All addresses are normalized to EIP-55 checksummed format:
- Input: `0xd8da6bf26964af9d7eed9e03e53415d37aa96045`
- Output: `0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045`

## Error Handling

### Common Errors

**"Alchemy API key is required"**
- Solution: Configure `alchemy_api_key` in `~/.celeste/config.json`

**"Address is required"**
- Solution: Provide `--address` parameter for watch_address operation

**"Invalid Ethereum address"**
- Solution: Verify address format (0x + 40 hex characters)

**"Block not found"**
- Solution: Block number may not exist yet or is too far in the future

**"Unsupported network"**
- Solution: Check network name against supported networks list

## Best Practices

1. **Polling Frequency**: Balance between latency and API usage
2. **Block History**: Keep `blocks_history` reasonable (1-20 blocks)
3. **Error Handling**: Implement retry logic for transient failures
4. **Rate Limiting**: Monitor API usage to stay within limits
5. **Data Storage**: Store historical data to reduce redundant API calls
6. **Network Selection**: Use appropriate network (mainnet vs testnet)

## Performance Considerations

### API Call Costs
Different operations have different compute unit costs:

- `get_latest_block`: ~20 CU per call
- `watch_address`: ~150 CU per call (depends on blocks_history)
- `get_block_by_number`: ~16 CU per call

### Optimization Tips

1. **Batch Addresses**: Monitor multiple addresses in a single script
2. **Adjust Poll Interval**: Longer intervals = fewer API calls
3. **Limit Block History**: Only check recent blocks for most use cases
4. **Cache Block Data**: Store block information to avoid redundant queries
5. **Use Webhooks**: When available, webhooks are more efficient than polling

## Security Considerations

- **API Key Security**: Never expose API keys in logs or public repositories
- **Address Validation**: Always validate addresses before monitoring
- **Rate Limiting**: Implement backoff strategies to avoid throttling
- **Data Privacy**: Be aware that blockchain data is public
- **Webhook Security**: Use HTTPS and verify webhook signatures (when implemented)

## Troubleshooting

### No Transactions Found
- Verify the address is correct
- Increase `blocks_history` to scan more blocks
- Check that the address has recent activity on the specified network

### Slow Performance
- Reduce `blocks_history` parameter
- Check network connectivity
- Verify you're not hitting rate limits

### Inconsistent Results
- Different nodes may have slight sync delays
- Wait for additional block confirmations for critical data

## Future Enhancements

Planned features for future versions:
- WebSocket subscriptions for real-time updates
- Webhook support for push notifications
- Contract event filtering by topics
- Mempool transaction monitoring
- Custom alert rules and filters

## Further Reading

- [Alchemy Asset Transfers API](https://docs.alchemy.com/reference/alchemy-getassettransfers)
- [Ethereum Block Structure](https://ethereum.org/en/developers/docs/blocks/)
- [EIP-55: Checksummed Addresses](https://eips.ethereum.org/EIPS/eip-55)
- [Alchemy Enhanced APIs](https://docs.alchemy.com/reference/enhanced-apis-overview)
