// Package builtin provides blockchain monitoring handler implementation
package builtin

import (
	"context"
	"fmt"
	"math/big"
	"net/http"
	"time"
)

// blockmonHandler handles blockchain monitoring skill execution
func blockmonHandler(args map[string]any, configLoader ConfigLoader) (any, error) {
	// Get configuration
	config, err := configLoader.GetBlockmonConfig()
	if err != nil {
		return formatErrorResponse(
			"config_error",
			"Alchemy API key is required for blockchain monitoring",
			"Configure Alchemy API key (same key used for both Alchemy and monitoring)",
			map[string]any{
				"skill":          "blockmon",
				"config_command": "Set CELESTE_ALCHEMY_API_KEY=<your_key>",
			},
		), nil
	}

	// Get operation
	operation, ok := args["operation"].(string)
	if !ok || operation == "" {
		return formatErrorResponse(
			"validation_error",
			"Operation is required",
			"Specify a monitoring operation (get_latest_block, watch_address, etc.)",
			map[string]any{
				"skill": "blockmon",
				"field": "operation",
			},
		), nil
	}

	// Get network (use default if not provided)
	network, ok := args["network"].(string)
	if !ok || network == "" {
		network = config.DefaultNetwork
	}

	// Validate network
	if err := ValidateAlchemyNetwork(network); err != nil {
		return formatErrorResponse(
			"validation_error",
			err.Error(),
			"Use one of: eth-mainnet, polygon-mainnet, arbitrum-mainnet, optimism-mainnet, base-mainnet",
			map[string]any{
				"skill":   "blockmon",
				"network": network,
			},
		), nil
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(config.PollIntervalSeconds) * time.Second,
	}

	// Build Alchemy config for reusing Alchemy functions
	alchemyConfig := AlchemyConfig{
		APIKey:         config.AlchemyAPIKey,
		DefaultNetwork: network,
		TimeoutSeconds: config.PollIntervalSeconds,
	}

	// Route to appropriate handler
	ctx := context.Background()

	switch operation {
	case "get_latest_block":
		return handleGetLatestBlock(ctx, client, alchemyConfig, network)
	case "watch_address":
		return handleWatchAddress(ctx, client, alchemyConfig, network, args)
	case "get_block_by_number":
		return handleGetBlockByNumber(ctx, client, alchemyConfig, network, args)
	default:
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Unknown operation: %s", operation),
			"Valid operations: get_latest_block, watch_address, get_block_by_number",
			map[string]any{
				"skill":     "blockmon",
				"operation": operation,
			},
		), nil
	}
}

// handleGetLatestBlock gets the latest block information
func handleGetLatestBlock(ctx context.Context, client *http.Client, config AlchemyConfig, network string) (any, error) {
	// Get latest block number
	blockNumResult, err := alchemyRequest(ctx, client, config, network, "eth_blockNumber", []any{})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get block number: %v", err),
			"",
			map[string]any{
				"skill":   "blockmon",
				"network": network,
			},
		), nil
	}

	blockNumberHex := blockNumResult["result"].(string)

	// Get full block details
	blockResult, err := alchemyRequest(ctx, client, config, network, "eth_getBlockByNumber", []any{blockNumberHex, true})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get block details: %v", err),
			"",
			map[string]any{
				"skill":   "blockmon",
				"network": network,
			},
		), nil
	}

	blockData, ok := blockResult["result"].(map[string]any)
	if !ok {
		return formatErrorResponse(
			"api_error",
			"Invalid block data format",
			"",
			map[string]any{
				"skill": "blockmon",
			},
		), nil
	}

	// Parse block number
	blockNumber := new(big.Int)
	blockNumber.SetString(blockNumberHex[2:], 16)

	// Get transaction count
	txCount := 0
	if transactions, ok := blockData["transactions"].([]any); ok {
		txCount = len(transactions)
	}

	return map[string]any{
		"success":           true,
		"network":           network,
		"block_number":      blockNumber.String(),
		"block_hex":         blockNumberHex,
		"block_hash":        blockData["hash"],
		"timestamp":         blockData["timestamp"],
		"transaction_count": txCount,
		"miner":             blockData["miner"],
		"gas_used":          blockData["gasUsed"],
		"gas_limit":         blockData["gasLimit"],
		"message":           fmt.Sprintf("Latest block: #%s with %d transactions", blockNumber.String(), txCount),
	}, nil
}

// handleWatchAddress monitors recent transactions for an address
func handleWatchAddress(ctx context.Context, client *http.Client, config AlchemyConfig, network string, args map[string]any) (any, error) {
	// Get and validate address
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return formatErrorResponse(
			"validation_error",
			"Address is required for watch_address operation",
			"Provide an Ethereum address to monitor",
			map[string]any{
				"skill":     "blockmon",
				"operation": "watch_address",
			},
		), nil
	}

	normalizedAddr, err := NormalizeAddress(address)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			err.Error(),
			"Provide a valid Ethereum address",
			map[string]any{
				"skill":   "blockmon",
				"address": address,
			},
		), nil
	}

	// Get blocks history (default 10)
	blocksHistory := 10
	if blocks, ok := args["blocks_history"].(float64); ok {
		blocksHistory = int(blocks)
	}

	// Get current block number
	blockNumResult, err := alchemyRequest(ctx, client, config, network, "eth_blockNumber", []any{})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get block number: %v", err),
			"",
			map[string]any{
				"skill":   "blockmon",
				"network": network,
			},
		), nil
	}

	currentBlockHex := blockNumResult["result"].(string)
	currentBlock := new(big.Int)
	currentBlock.SetString(currentBlockHex[2:], 16)

	// Calculate from block
	fromBlock := new(big.Int).Sub(currentBlock, big.NewInt(int64(blocksHistory)))
	fromBlockHex := fmt.Sprintf("0x%x", fromBlock)

	// Get asset transfers for the address
	params := map[string]any{
		"fromBlock":   fromBlockHex,
		"toBlock":     "latest",
		"fromAddress": normalizedAddr,
		"category":    []string{"external", "internal", "erc20", "erc721", "erc1155"},
	}

	result, err := alchemyRequest(ctx, client, config, network, "alchemy_getAssetTransfers", []any{params})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get transactions: %v", err),
			"",
			map[string]any{
				"skill":   "blockmon",
				"network": network,
			},
		), nil
	}

	transferData, ok := result["result"].(map[string]any)
	if !ok {
		return formatErrorResponse(
			"api_error",
			"Invalid response format",
			"",
			map[string]any{
				"skill": "blockmon",
			},
		), nil
	}

	transfers, _ := transferData["transfers"].([]any)
	txCount := len(transfers)

	return map[string]any{
		"success":           true,
		"address":           normalizedAddr,
		"network":           network,
		"blocks_checked":    blocksHistory,
		"current_block":     currentBlock.String(),
		"from_block":        fromBlock.String(),
		"transaction_count": txCount,
		"transactions":      transfers,
		"message":           fmt.Sprintf("Found %d transactions in last %d blocks", txCount, blocksHistory),
	}, nil
}

// handleGetBlockByNumber gets a specific block by number
func handleGetBlockByNumber(ctx context.Context, client *http.Client, config AlchemyConfig, network string, args map[string]any) (any, error) {
	// Get block number
	blockNumber, ok := args["block_number"].(string)
	if !ok || blockNumber == "" {
		return formatErrorResponse(
			"validation_error",
			"Block number is required",
			"Provide a block number (hex like 0x1234 or decimal like 4660)",
			map[string]any{
				"skill":     "blockmon",
				"operation": "get_block_by_number",
			},
		), nil
	}

	// Convert to hex if decimal
	blockNumberHex := blockNumber
	if blockNumber[0] != '0' || (len(blockNumber) > 1 && blockNumber[1] != 'x') {
		// Decimal number, convert to hex
		bn := new(big.Int)
		bn.SetString(blockNumber, 10)
		blockNumberHex = fmt.Sprintf("0x%x", bn)
	}

	// Get block details
	result, err := alchemyRequest(ctx, client, config, network, "eth_getBlockByNumber", []any{blockNumberHex, true})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get block: %v", err),
			"",
			map[string]any{
				"skill":   "blockmon",
				"network": network,
			},
		), nil
	}

	blockData, ok := result["result"].(map[string]any)
	if !ok || blockData == nil {
		return formatErrorResponse(
			"api_error",
			"Block not found",
			"The block number may not exist yet",
			map[string]any{
				"skill":        "blockmon",
				"block_number": blockNumber,
			},
		), nil
	}

	// Get transaction count
	txCount := 0
	if transactions, ok := blockData["transactions"].([]any); ok {
		txCount = len(transactions)
	}

	return map[string]any{
		"success":           true,
		"network":           network,
		"block_number":      blockNumber,
		"block_hex":         blockNumberHex,
		"block_hash":        blockData["hash"],
		"timestamp":         blockData["timestamp"],
		"transaction_count": txCount,
		"miner":             blockData["miner"],
		"gas_used":          blockData["gasUsed"],
		"gas_limit":         blockData["gasLimit"],
		"data":              blockData,
		"message":           fmt.Sprintf("Block #%s with %d transactions", blockNumber, txCount),
	}, nil
}
