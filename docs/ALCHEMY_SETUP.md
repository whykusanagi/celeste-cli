# Alchemy API Setup Guide

This guide explains how to configure and use the Alchemy skill in Celeste CLI for comprehensive blockchain data access across Ethereum and Layer 2 networks.

## Overview

The Alchemy skill provides:
- **Wallet Operations**: Get ETH balances, token balances, transaction history
- **Token Data**: Real-time token metadata and information
- **NFT APIs**: Query NFTs by owner, get metadata, explore collections
- **Transaction Monitoring**: Gas prices, transaction receipts, block information
- **Multi-Network Support**: Ethereum, Arbitrum, Optimism, Polygon, Base (mainnet + testnets)

## Getting Started

### 1. Create an Alchemy Account

1. Sign up at [alchemy.com](https://www.alchemy.com)
2. Create a new app in the dashboard
3. Select your target network (e.g., Ethereum Mainnet)
4. Copy your **API Key** from the app dashboard

### 2. Configure Celeste CLI

Edit `~/.celeste/config.json` and add:
```json
{
  "alchemy_api_key": "your_api_key_here",
  "alchemy_default_network": "eth-mainnet",
  "alchemy_timeout_seconds": 10
}
```

**Note**: API keys are stored securely in the config file. Never use environment variables as they can be accessed by any process on your system.

## Supported Networks

### Mainnet Networks
- **eth-mainnet** - Ethereum Mainnet
- **polygon-mainnet** - Polygon (Matic) Mainnet
- **arbitrum-mainnet** - Arbitrum One
- **optimism-mainnet** - Optimism Mainnet
- **base-mainnet** - Base Mainnet

### Testnet Networks
- **eth-sepolia** - Ethereum Sepolia Testnet
- **polygon-amoy** - Polygon Amoy Testnet
- **arbitrum-sepolia** - Arbitrum Sepolia
- **optimism-sepolia** - Optimism Sepolia
- **base-sepolia** - Base Sepolia

## Operations

### 1. Get Balance

Get ETH balance for an address.

```bash
celeste skill alchemy \
  --operation get_balance \
  --address 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
  "network": "eth-mainnet",
  "balance_wei": "1234567890000000000",
  "balance_eth": "1.23456789",
  "message": "Balance: 1.23456789 ETH"
}
```

### 2. Get Token Balances

Get all token balances for an address.

```bash
celeste skill alchemy \
  --operation get_token_balances \
  --address 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
  "network": "eth-mainnet",
  "token_count": 5,
  "tokens": [
    {
      "contract_address": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
      "token_balance": "1000000000",
      "name": "USD Coin",
      "symbol": "USDC"
    }
  ],
  "message": "Found 5 tokens"
}
```

### 3. Get Transaction History

Get recent transactions for an address.

```bash
celeste skill alchemy \
  --operation get_transaction_history \
  --address 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "address": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
  "network": "eth-mainnet",
  "transaction_count": 50,
  "transactions": [
    {
      "hash": "0xabc...",
      "from": "0x...",
      "to": "0x...",
      "value": "1000000000000000000",
      "block_number": 18500000
    }
  ],
  "message": "Found 50 transactions"
}
```

### 4. Get Token Metadata

Get detailed information about a token contract.

```bash
celeste skill alchemy \
  --operation get_token_metadata \
  --token_address 0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48 \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "token_address": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
  "network": "eth-mainnet",
  "name": "USD Coin",
  "symbol": "USDC",
  "decimals": 6,
  "logo": "https://...",
  "message": "Token: USD Coin (USDC)"
}
```

### 5. Get NFTs by Owner

Query all NFTs owned by an address.

```bash
celeste skill alchemy \
  --operation get_nfts_by_owner \
  --address 0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045 \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "owner": "0xd8dA6BF26964aF9D7eEd9e03E53415D37aA96045",
  "network": "eth-mainnet",
  "total_count": 10,
  "nfts": [
    {
      "contract_address": "0x...",
      "token_id": "1234",
      "title": "Cool NFT #1234",
      "description": "...",
      "image_url": "ipfs://..."
    }
  ],
  "message": "Found 10 NFTs"
}
```

### 6. Get NFT Metadata

Get metadata for a specific NFT.

```bash
celeste skill alchemy \
  --operation get_nft_metadata \
  --token_address 0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D \
  --token_id 1 \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "contract_address": "0xBC4CA0EdA7647A8aB7C2061c2E118A18a936f13D",
  "token_id": "1",
  "network": "eth-mainnet",
  "title": "Bored Ape #1",
  "description": "...",
  "image_url": "ipfs://QmRRPWG96cmgTn2qSzjwr2qvfNEuhunv6FNeMFGa9bx6mQ",
  "attributes": [...],
  "message": "NFT: Bored Ape #1"
}
```

### 7. Get Gas Price

Get current gas price for a network.

```bash
celeste skill alchemy \
  --operation get_gas_price \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "network": "eth-mainnet",
  "gas_price_wei": "20000000000",
  "gas_price_gwei": "20",
  "message": "Current gas price: 20 Gwei"
}
```

### 8. Get Transaction Receipt

Get receipt for a completed transaction.

```bash
celeste skill alchemy \
  --operation get_transaction_receipt \
  --tx_hash 0xabc123... \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "tx_hash": "0xabc123...",
  "network": "eth-mainnet",
  "status": "success",
  "block_number": 18500000,
  "gas_used": "21000",
  "from": "0x...",
  "to": "0x...",
  "message": "Transaction successful at block 18500000"
}
```

### 9. Get Block Number

Get the current block number for a network.

```bash
celeste skill alchemy \
  --operation get_block_number \
  --network eth-mainnet

# Returns:
{
  "success": true,
  "network": "eth-mainnet",
  "block_number": "18500000",
  "block_hex": "0x11A09A0",
  "message": "Current block: 18500000"
}
```

## Use Cases

### 1. Wallet Tracking
Monitor wallet balances, token holdings, and transaction history across multiple chains.

### 2. DeFi Analytics
Track token prices, liquidity, and DeFi protocol interactions.

### 3. NFT Portfolio Management
View and analyze NFT collections and metadata.

### 4. Transaction Monitoring
Watch for transaction confirmations and monitor gas prices.

### 5. Multi-Chain Applications
Build applications that work across Ethereum and Layer 2 networks.

## Technical Details

### Rate Limiting
The Alchemy skill includes built-in rate limiting:
- Default: 5 requests per second
- Uses `golang.org/x/time/rate` for token bucket algorithm
- Prevents API throttling errors

### Address Validation
All Ethereum addresses are validated using go-ethereum:
- EIP-55 checksum validation
- Automatic normalization to checksummed format
- Proper hex address format checking

### Wei/Ether Conversion
Accurate conversion using go-ethereum constants:
- 1 Ether = 10^18 Wei
- 1 Gwei = 10^9 Wei
- Big integer math for precision

### Network Endpoints
Each network has a unique Alchemy URL format:
```
https://{network}.g.alchemy.com/v2/{api_key}
```

Examples:
- `https://eth-mainnet.g.alchemy.com/v2/your_api_key`
- `https://polygon-mainnet.g.alchemy.com/v2/your_api_key`

## Error Handling

### Common Errors

**"Alchemy API key is required"**
- Solution: Add `alchemy_api_key` to `~/.celeste/config.json`

**"Invalid Ethereum address"**
- Solution: Verify address format (0x followed by 40 hex characters)

**"Unsupported network"**
- Solution: Check network name against supported networks list

**"API request failed: 429"**
- Solution: Rate limit exceeded, wait before retrying or upgrade plan

**"Transaction not found"**
- Solution: Transaction may not be mined yet or hash is incorrect

## Best Practices

1. **API Key Security**: Never commit API keys to version control
2. **Network Selection**: Use testnets for development and testing
3. **Error Handling**: Implement retry logic for transient failures
4. **Rate Limiting**: Respect API rate limits to avoid throttling
5. **Address Validation**: Always validate addresses before API calls
6. **Caching**: Cache frequently accessed data to reduce API calls

## Cost Considerations

### Alchemy Free Tier
- 300 million compute units per month
- 300 requests per second
- All networks included
- Enhanced APIs included

### Compute Unit Costs
Different operations consume different compute units:
- `eth_getBalance`: 10 CU
- `eth_getBlockByNumber`: 16 CU
- `alchemy_getAssetTransfers`: 150 CU
- `alchemy_getTokenBalances`: 77 CU

Monitor your usage in the Alchemy dashboard.

## Advanced Features

### Asset Transfers
The `get_transaction_history` operation uses Alchemy's enhanced `alchemy_getAssetTransfers` API:
- Tracks external, internal, ERC20, ERC721, ERC1155 transfers
- More comprehensive than standard `eth_getLogs`
- Better performance and accuracy

### NFT API
Alchemy provides enhanced NFT metadata:
- Automatic IPFS resolution
- Cached metadata for faster access
- Normalized format across collections

### WebSocket Support
While not yet implemented in Celeste CLI, Alchemy supports WebSocket subscriptions for:
- New blocks
- Pending transactions
- Contract events

## Troubleshooting

### Slow Response Times
- Check network connectivity
- Verify you're not hitting rate limits
- Consider using a closer geographic endpoint

### Inconsistent Data
- Different nodes may have slight sync delays
- Use `get_block_number` to verify sync status
- Wait for additional confirmations for critical transactions

### Authentication Errors
- Verify API key is correct
- Check that API key has access to the requested network
- Ensure API key hasn't been rate limited or suspended

## Further Reading

- [Alchemy Documentation](https://docs.alchemy.com)
- [Ethereum JSON-RPC API](https://ethereum.org/en/developers/docs/apis/json-rpc/)
- [EIP-55: Mixed-case checksum address encoding](https://eips.ethereum.org/EIPS/eip-55)
- [Alchemy Enhanced APIs](https://docs.alchemy.com/reference/enhanced-apis-overview)
