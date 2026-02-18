// Package skills provides Alchemy blockchain API skill implementation
package skills

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"
)

// AlchemySkill returns the Alchemy skill definition
func AlchemySkill() Skill {
	return Skill{
		Name:        "alchemy",
		Description: "Blockchain data and analytics via Alchemy API: wallet tracing, token prices, NFT data, transaction monitoring across Ethereum and L2s (Arbitrum, Optimism, Polygon, Base)",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{
						"get_balance", "get_token_balances", "get_transaction_history",
						"get_token_price", "get_token_metadata",
						"get_nfts_by_owner", "get_nft_metadata",
						"get_gas_price", "get_transaction_receipt", "get_block_number",
					},
					"description": "Alchemy API operation to perform",
				},
				"network": map[string]interface{}{
					"type":        "string",
					"description": "Blockchain network (eth-mainnet, polygon-mainnet, arbitrum-mainnet, optimism-mainnet, base-mainnet)",
				},
				"address": map[string]interface{}{
					"type":        "string",
					"description": "Ethereum address (for wallet and NFT operations)",
				},
				"token_address": map[string]interface{}{
					"type":        "string",
					"description": "Token contract address",
				},
				"tx_hash": map[string]interface{}{
					"type":        "string",
					"description": "Transaction hash (for transaction operations)",
				},
				"block_number": map[string]interface{}{
					"type":        "string",
					"description": "Block number (latest, earliest, or hex number)",
				},
			},
			"required": []string{"operation"},
		},
	}
}

// AlchemyHandler handles Alchemy skill execution
func AlchemyHandler(args map[string]interface{}, configLoader ConfigLoader) (interface{}, error) {
	// Get configuration
	config, err := configLoader.GetAlchemyConfig()
	if err != nil {
		return formatErrorResponse(
			"config_error",
			"Alchemy API key is required",
			"Configure Alchemy by setting CELESTE_ALCHEMY_API_KEY environment variable or adding to skills.json",
			map[string]interface{}{
				"skill":          "alchemy",
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
			"Specify an Alchemy operation (get_balance, get_nfts_by_owner, etc.)",
			map[string]interface{}{
				"skill": "alchemy",
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
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(config.TimeoutSeconds) * time.Second,
	}

	// Route to appropriate handler
	ctx := context.Background()

	switch operation {
	case "get_balance":
		return handleGetBalance(ctx, client, config, network, args)
	case "get_token_balances":
		return handleGetTokenBalances(ctx, client, config, network, args)
	case "get_transaction_history":
		return handleGetTransactionHistory(ctx, client, config, network, args)
	case "get_token_metadata":
		return handleGetTokenMetadata(ctx, client, config, network, args)
	case "get_nfts_by_owner":
		return handleGetNFTsByOwner(ctx, client, config, network, args)
	case "get_nft_metadata":
		return handleGetNFTMetadata(ctx, client, config, network, args)
	case "get_gas_price":
		return handleGetGasPrice(ctx, client, config, network)
	case "get_transaction_receipt":
		return handleGetTransactionReceipt(ctx, client, config, network, args)
	case "get_block_number":
		return handleGetBlockNumber(ctx, client, config, network)
	default:
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Unknown operation: %s", operation),
			"Check the operation name",
			map[string]interface{}{
				"skill":     "alchemy",
				"operation": operation,
			},
		), nil
	}
}

// alchemyRequest makes a JSON-RPC request to Alchemy
func alchemyRequest(ctx context.Context, client *http.Client, config AlchemyConfig, network, method string, params []interface{}) (map[string]interface{}, error) {
	url := BuildAlchemyURL(network, config.APIKey)

	// Build JSON-RPC request
	reqBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
		"params":  params,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse JSON-RPC response
	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Check for JSON-RPC error
	if errObj, ok := result["error"].(map[string]interface{}); ok {
		return nil, fmt.Errorf("RPC error: %v", errObj["message"])
	}

	return result, nil
}

// handleGetBalance gets ETH balance for an address
func handleGetBalance(ctx context.Context, client *http.Client, config AlchemyConfig, network string, args map[string]interface{}) (interface{}, error) {
	// Get and validate address
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return formatErrorResponse(
			"validation_error",
			"Address is required",
			"Provide an Ethereum address",
			map[string]interface{}{
				"skill":     "alchemy",
				"operation": "get_balance",
			},
		), nil
	}

	normalizedAddr, err := NormalizeAddress(address)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			err.Error(),
			"Provide a valid Ethereum address",
			map[string]interface{}{
				"skill":   "alchemy",
				"address": address,
			},
		), nil
	}

	// Get block parameter (default to "latest")
	blockParam := "latest"
	if block, ok := args["block_number"].(string); ok && block != "" {
		blockParam = block
	}

	// Make RPC call
	result, err := alchemyRequest(ctx, client, config, network, "eth_getBalance", []interface{}{normalizedAddr, blockParam})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get balance: %v", err),
			"",
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	// Parse balance (hex string)
	balanceHex, ok := result["result"].(string)
	if !ok {
		return formatErrorResponse(
			"api_error",
			"Invalid response format",
			"",
			map[string]interface{}{
				"skill": "alchemy",
			},
		), nil
	}

	// Convert hex to big.Int
	balance := new(big.Int)
	balance.SetString(balanceHex[2:], 16) // Remove "0x" prefix

	// Convert to Ether
	etherBalance := WeiToEther(balance)

	return map[string]interface{}{
		"success":     true,
		"address":     normalizedAddr,
		"balance_wei": balance.String(),
		"balance_eth": etherBalance,
		"network":     network,
		"block":       blockParam,
		"message":     fmt.Sprintf("Balance: %s ETH", etherBalance),
	}, nil
}

// handleGetTokenBalances gets token balances for an address
func handleGetTokenBalances(ctx context.Context, client *http.Client, config AlchemyConfig, network string, args map[string]interface{}) (interface{}, error) {
	// Get and validate address
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return formatErrorResponse(
			"validation_error",
			"Address is required",
			"Provide an Ethereum address",
			map[string]interface{}{
				"skill":     "alchemy",
				"operation": "get_token_balances",
			},
		), nil
	}

	normalizedAddr, err := NormalizeAddress(address)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			err.Error(),
			"Provide a valid Ethereum address",
			map[string]interface{}{
				"skill":   "alchemy",
				"address": address,
			},
		), nil
	}

	// Make RPC call (using Alchemy's enhanced API)
	result, err := alchemyRequest(ctx, client, config, network, "alchemy_getTokenBalances", []interface{}{normalizedAddr, "erc20"})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get token balances: %v", err),
			"",
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"address": normalizedAddr,
		"network": network,
		"data":    result["result"],
		"message": "Token balances retrieved successfully",
	}, nil
}

// handleGetTransactionHistory gets transaction history for an address
func handleGetTransactionHistory(ctx context.Context, client *http.Client, config AlchemyConfig, network string, args map[string]interface{}) (interface{}, error) {
	// Get and validate address
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return formatErrorResponse(
			"validation_error",
			"Address is required",
			"Provide an Ethereum address",
			map[string]interface{}{
				"skill":     "alchemy",
				"operation": "get_transaction_history",
			},
		), nil
	}

	normalizedAddr, err := NormalizeAddress(address)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			err.Error(),
			"Provide a valid Ethereum address",
			map[string]interface{}{
				"skill":   "alchemy",
				"address": address,
			},
		), nil
	}

	// Build parameters for asset transfers
	params := map[string]interface{}{
		"fromAddress": normalizedAddr,
		"category":    []string{"external", "internal", "erc20", "erc721", "erc1155"},
	}

	// Make RPC call (using Alchemy's asset transfers API)
	result, err := alchemyRequest(ctx, client, config, network, "alchemy_getAssetTransfers", []interface{}{params})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get transaction history: %v", err),
			"",
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"address": normalizedAddr,
		"network": network,
		"data":    result["result"],
		"message": "Transaction history retrieved successfully",
	}, nil
}

// handleGetTokenMetadata gets metadata for a token
func handleGetTokenMetadata(ctx context.Context, client *http.Client, config AlchemyConfig, network string, args map[string]interface{}) (interface{}, error) {
	// Get and validate token address
	tokenAddress, ok := args["token_address"].(string)
	if !ok || tokenAddress == "" {
		return formatErrorResponse(
			"validation_error",
			"Token address is required",
			"Provide a token contract address",
			map[string]interface{}{
				"skill":     "alchemy",
				"operation": "get_token_metadata",
			},
		), nil
	}

	normalizedAddr, err := NormalizeAddress(tokenAddress)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			err.Error(),
			"Provide a valid token contract address",
			map[string]interface{}{
				"skill":         "alchemy",
				"token_address": tokenAddress,
			},
		), nil
	}

	// Make RPC call
	result, err := alchemyRequest(ctx, client, config, network, "alchemy_getTokenMetadata", []interface{}{normalizedAddr})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get token metadata: %v", err),
			"",
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	return map[string]interface{}{
		"success":       true,
		"token_address": normalizedAddr,
		"network":       network,
		"data":          result["result"],
		"message":       "Token metadata retrieved successfully",
	}, nil
}

// handleGetNFTsByOwner gets NFTs owned by an address
func handleGetNFTsByOwner(ctx context.Context, client *http.Client, config AlchemyConfig, network string, args map[string]interface{}) (interface{}, error) {
	// Get and validate address
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return formatErrorResponse(
			"validation_error",
			"Address is required",
			"Provide an Ethereum address",
			map[string]interface{}{
				"skill":     "alchemy",
				"operation": "get_nfts_by_owner",
			},
		), nil
	}

	normalizedAddr, err := NormalizeAddress(address)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			err.Error(),
			"Provide a valid Ethereum address",
			map[string]interface{}{
				"skill":   "alchemy",
				"address": address,
			},
		), nil
	}

	// Make RPC call (using Alchemy's NFT API)
	result, err := alchemyRequest(ctx, client, config, network, "alchemy_getNFTs", []interface{}{normalizedAddr})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get NFTs: %v", err),
			"",
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"address": normalizedAddr,
		"network": network,
		"data":    result["result"],
		"message": "NFTs retrieved successfully",
	}, nil
}

// handleGetNFTMetadata gets metadata for a specific NFT
func handleGetNFTMetadata(ctx context.Context, client *http.Client, config AlchemyConfig, network string, args map[string]interface{}) (interface{}, error) {
	// Get and validate contract address
	contractAddress, ok := args["token_address"].(string)
	if !ok || contractAddress == "" {
		return formatErrorResponse(
			"validation_error",
			"Token address (NFT contract) is required",
			"Provide an NFT contract address",
			map[string]interface{}{
				"skill":     "alchemy",
				"operation": "get_nft_metadata",
			},
		), nil
	}

	normalizedAddr, err := NormalizeAddress(contractAddress)
	if err != nil {
		return formatErrorResponse(
			"validation_error",
			err.Error(),
			"Provide a valid NFT contract address",
			map[string]interface{}{
				"skill":         "alchemy",
				"token_address": contractAddress,
			},
		), nil
	}

	// Get token ID (as string)
	tokenID, _ := args["token_id"].(string)
	if tokenID == "" {
		tokenID = "1" // Default to token ID 1
	}

	// Make RPC call
	result, err := alchemyRequest(ctx, client, config, network, "alchemy_getNFTMetadata", []interface{}{normalizedAddr, tokenID})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get NFT metadata: %v", err),
			"",
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	return map[string]interface{}{
		"success":  true,
		"contract": normalizedAddr,
		"token_id": tokenID,
		"network":  network,
		"data":     result["result"],
		"message":  "NFT metadata retrieved successfully",
	}, nil
}

// handleGetGasPrice gets current gas price
func handleGetGasPrice(ctx context.Context, client *http.Client, config AlchemyConfig, network string) (interface{}, error) {
	// Make RPC call
	result, err := alchemyRequest(ctx, client, config, network, "eth_gasPrice", []interface{}{})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get gas price: %v", err),
			"",
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	// Parse gas price (hex string)
	gasPriceHex, ok := result["result"].(string)
	if !ok {
		return formatErrorResponse(
			"api_error",
			"Invalid response format",
			"",
			map[string]interface{}{
				"skill": "alchemy",
			},
		), nil
	}

	// Convert hex to big.Int
	gasPrice := new(big.Int)
	gasPrice.SetString(gasPriceHex[2:], 16) // Remove "0x" prefix

	// Convert to Gwei
	gweiPrice := WeiToGwei(gasPrice)

	return map[string]interface{}{
		"success":        true,
		"network":        network,
		"gas_price_wei":  gasPrice.String(),
		"gas_price_gwei": gweiPrice,
		"message":        fmt.Sprintf("Current gas price: %d Gwei", gweiPrice),
	}, nil
}

// handleGetTransactionReceipt gets receipt for a transaction
func handleGetTransactionReceipt(ctx context.Context, client *http.Client, config AlchemyConfig, network string, args map[string]interface{}) (interface{}, error) {
	// Get transaction hash
	txHash, ok := args["tx_hash"].(string)
	if !ok || txHash == "" {
		return formatErrorResponse(
			"validation_error",
			"Transaction hash is required",
			"Provide a transaction hash",
			map[string]interface{}{
				"skill":     "alchemy",
				"operation": "get_transaction_receipt",
			},
		), nil
	}

	// Make RPC call
	result, err := alchemyRequest(ctx, client, config, network, "eth_getTransactionReceipt", []interface{}{txHash})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get transaction receipt: %v", err),
			"",
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	return map[string]interface{}{
		"success":          true,
		"transaction_hash": txHash,
		"network":          network,
		"data":             result["result"],
		"message":          "Transaction receipt retrieved successfully",
	}, nil
}

// handleGetBlockNumber gets current block number
func handleGetBlockNumber(ctx context.Context, client *http.Client, config AlchemyConfig, network string) (interface{}, error) {
	// Make RPC call
	result, err := alchemyRequest(ctx, client, config, network, "eth_blockNumber", []interface{}{})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get block number: %v", err),
			"",
			map[string]interface{}{
				"skill":   "alchemy",
				"network": network,
			},
		), nil
	}

	// Parse block number (hex string)
	blockNumberHex, ok := result["result"].(string)
	if !ok {
		return formatErrorResponse(
			"api_error",
			"Invalid response format",
			"",
			map[string]interface{}{
				"skill": "alchemy",
			},
		), nil
	}

	// Convert hex to int
	blockNumber := new(big.Int)
	blockNumber.SetString(blockNumberHex[2:], 16) // Remove "0x" prefix

	return map[string]interface{}{
		"success":      true,
		"network":      network,
		"block_number": blockNumber.String(),
		"block_hex":    blockNumberHex,
		"message":      fmt.Sprintf("Current block: %s", blockNumber.String()),
	}, nil
}
