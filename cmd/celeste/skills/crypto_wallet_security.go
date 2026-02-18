// Package skills provides wallet security monitoring skill implementation
package skills

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// WalletSecuritySkill returns the wallet security monitoring skill definition
func WalletSecuritySkill() Skill {
	return Skill{
		Name:        "wallet_security",
		Description: "Monitor wallet addresses for security threats: dust attacks, NFT scams, dangerous approvals, large transfers across Ethereum and L2s",
		Parameters: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{
						"add_monitored_wallet",
						"remove_monitored_wallet",
						"list_monitored_wallets",
						"check_wallet_security",
						"get_security_alerts",
						"acknowledge_alert",
					},
					"description": "Wallet security operation to perform",
				},
				"address": map[string]interface{}{
					"type":        "string",
					"description": "Ethereum wallet address to monitor",
				},
				"label": map[string]interface{}{
					"type":        "string",
					"description": "Friendly label for the wallet (e.g., 'Main Wallet', 'Trading Account')",
				},
				"network": map[string]interface{}{
					"type":        "string",
					"description": "Blockchain network (default: eth-mainnet)",
				},
				"alert_id": map[string]interface{}{
					"type":        "string",
					"description": "Alert ID to acknowledge",
				},
				"unacknowledged_only": map[string]interface{}{
					"type":        "boolean",
					"description": "Filter for unacknowledged alerts only",
				},
			},
			"required": []string{"operation"},
		},
	}
}

// WalletSecurityConfig holds wallet security configuration
type WalletSecurityConfig struct {
	MonitoredWallets    []MonitoredWallet `json:"monitored_wallets"`
	LastCheckedBlock    string            `json:"last_checked_block"`
	PollIntervalSeconds int               `json:"poll_interval_seconds"`
}

// MonitoredWallet represents a wallet being monitored
type MonitoredWallet struct {
	Address string    `json:"address"` // EIP-55 checksummed
	Label   string    `json:"label"`
	Network string    `json:"network"`
	AddedAt time.Time `json:"added_at"`
}

// SecurityAlert represents a detected security threat
type SecurityAlert struct {
	ID             string                 `json:"id"`             // alert_<timestamp>_<random>
	WalletAddress  string                 `json:"wallet_address"` // Affected wallet
	AlertType      string                 `json:"alert_type"`     // dust_attack, nft_scam, dangerous_approval, large_transfer
	Severity       string                 `json:"severity"`       // low, medium, high, critical
	TxHash         string                 `json:"tx_hash"`        // Transaction hash
	BlockNumber    string                 `json:"block_number"`
	Description    string                 `json:"description"` // Human-readable description
	Details        map[string]interface{} `json:"details"`     // Type-specific details
	DetectedAt     time.Time              `json:"detected_at"`
	Acknowledged   bool                   `json:"acknowledged"`
	AcknowledgedAt *time.Time             `json:"acknowledged_at,omitempty"`
}

// AlertsLog stores all security alerts
type AlertsLog struct {
	Alerts []SecurityAlert `json:"alerts"`
}

// AssetTransfer represents a blockchain asset transfer (from crypto_blockmon.go pattern)
type AssetTransfer struct {
	Category        string
	BlockNum        string
	From            string
	To              string
	Value           float64
	Asset           string
	Hash            string
	RawContract     struct{ Address string }
	TokenId         string
	ERC721TokenId   string
	ERC1155Metadata []struct {
		TokenId string
		Value   string
	}
}

// Storage path helpers
func getWalletSecurityPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "wallet_security.json")
}

func getWalletAlertsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "wallet_alerts.json")
}

// WalletSecurityHandler handles wallet security skill execution
func WalletSecurityHandler(args map[string]interface{}, configLoader ConfigLoader) (interface{}, error) {
	// Get operation
	operation, ok := args["operation"].(string)
	if !ok || operation == "" {
		return formatErrorResponse(
			"validation_error",
			"Operation is required",
			"Specify a wallet security operation",
			map[string]interface{}{
				"skill": "wallet_security",
				"field": "operation",
			},
		), nil
	}

	// Create context
	ctx := context.Background()

	// Route to operation handlers
	switch operation {
	case "add_monitored_wallet":
		return handleAddMonitoredWallet(args)
	case "remove_monitored_wallet":
		return handleRemoveMonitoredWallet(args)
	case "list_monitored_wallets":
		return handleListMonitoredWallets()
	case "check_wallet_security":
		return handleCheckWalletSecurity(ctx, configLoader)
	case "get_security_alerts":
		return handleGetSecurityAlerts(args)
	case "acknowledge_alert":
		return handleAcknowledgeAlert(args)
	default:
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Unknown operation: %s", operation),
			"Check the operation name",
			map[string]interface{}{
				"skill":     "wallet_security",
				"operation": operation,
			},
		), nil
	}
}

// handleAddMonitoredWallet adds a wallet to the monitoring list
func handleAddMonitoredWallet(args map[string]interface{}) (interface{}, error) {
	// Get and validate address
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return formatErrorResponse(
			"validation_error",
			"Address is required",
			"Provide an Ethereum address to monitor",
			map[string]interface{}{
				"skill":     "wallet_security",
				"operation": "add_monitored_wallet",
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
				"skill":   "wallet_security",
				"address": address,
			},
		), nil
	}

	// Get label (optional)
	label, _ := args["label"].(string)
	if label == "" {
		label = fmt.Sprintf("Wallet %s", normalizedAddr[:8])
	}

	// Get network (default: eth-mainnet)
	network, _ := args["network"].(string)
	if network == "" {
		network = "eth-mainnet"
	}

	if err := ValidateAlchemyNetwork(network); err != nil {
		return formatErrorResponse(
			"validation_error",
			err.Error(),
			"Use one of: eth-mainnet, polygon-mainnet, arbitrum-mainnet, optimism-mainnet, base-mainnet",
			map[string]interface{}{
				"skill":   "wallet_security",
				"network": network,
			},
		), nil
	}

	// Load existing config
	config, err := loadWalletSecurityConfig()
	if err != nil {
		config = &WalletSecurityConfig{
			MonitoredWallets:    []MonitoredWallet{},
			PollIntervalSeconds: 300, // 5 minutes
		}
	}

	// Check if already monitoring
	for _, w := range config.MonitoredWallets {
		if w.Address == normalizedAddr && w.Network == network {
			return formatErrorResponse(
				"validation_error",
				fmt.Sprintf("Wallet already monitored: %s on %s", normalizedAddr, network),
				"This wallet is already in the monitoring list",
				map[string]interface{}{
					"skill":   "wallet_security",
					"address": normalizedAddr,
					"network": network,
				},
			), nil
		}
	}

	// Add wallet
	wallet := MonitoredWallet{
		Address: normalizedAddr,
		Label:   label,
		Network: network,
		AddedAt: time.Now(),
	}
	config.MonitoredWallets = append(config.MonitoredWallets, wallet)

	// Save config
	if err := saveWalletSecurityConfig(config); err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to save configuration: %v", err),
			"",
			map[string]interface{}{
				"skill": "wallet_security",
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Now monitoring wallet: %s (%s) on %s", label, normalizedAddr, network),
		"wallet":  wallet,
	}, nil
}

// handleRemoveMonitoredWallet removes a wallet from the monitoring list
func handleRemoveMonitoredWallet(args map[string]interface{}) (interface{}, error) {
	// Get and validate address
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return formatErrorResponse(
			"validation_error",
			"Address is required",
			"Provide an Ethereum address to remove",
			map[string]interface{}{
				"skill":     "wallet_security",
				"operation": "remove_monitored_wallet",
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
				"skill":   "wallet_security",
				"address": address,
			},
		), nil
	}

	// Get network (optional, if not provided remove from all networks)
	network, _ := args["network"].(string)

	// Load existing config
	config, err := loadWalletSecurityConfig()
	if err != nil {
		return formatErrorResponse(
			"api_error",
			"No wallets configured for monitoring",
			"Add a wallet first using add_monitored_wallet",
			map[string]interface{}{
				"skill": "wallet_security",
			},
		), nil
	}

	// Remove wallet(s)
	found := false
	newWallets := []MonitoredWallet{}
	for _, w := range config.MonitoredWallets {
		if w.Address == normalizedAddr && (network == "" || w.Network == network) {
			found = true
			continue // Skip this wallet
		}
		newWallets = append(newWallets, w)
	}

	if !found {
		return formatErrorResponse(
			"validation_error",
			"Wallet not found in monitoring list",
			"Check the address and network",
			map[string]interface{}{
				"skill":   "wallet_security",
				"address": normalizedAddr,
				"network": network,
			},
		), nil
	}

	config.MonitoredWallets = newWallets

	// Save config
	if err := saveWalletSecurityConfig(config); err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to save configuration: %v", err),
			"",
			map[string]interface{}{
				"skill": "wallet_security",
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Removed wallet from monitoring: %s", normalizedAddr),
	}, nil
}

// handleListMonitoredWallets lists all monitored wallets
func handleListMonitoredWallets() (interface{}, error) {
	config, err := loadWalletSecurityConfig()
	if err != nil {
		return map[string]interface{}{
			"success": true,
			"wallets": []MonitoredWallet{},
			"count":   0,
			"message": "No wallets configured for monitoring",
		}, nil
	}

	return map[string]interface{}{
		"success": true,
		"wallets": config.MonitoredWallets,
		"count":   len(config.MonitoredWallets),
		"message": fmt.Sprintf("Monitoring %d wallet(s)", len(config.MonitoredWallets)),
	}, nil
}

// handleCheckWalletSecurity checks all monitored wallets for security threats
func handleCheckWalletSecurity(ctx context.Context, configLoader ConfigLoader) (interface{}, error) {
	// Load wallet security config
	wsConfig, err := loadWalletSecurityConfig()
	if err != nil {
		return formatErrorResponse(
			"api_error",
			"No wallets configured for monitoring",
			"Add a wallet first using add_monitored_wallet",
			map[string]interface{}{
				"skill": "wallet_security",
			},
		), nil
	}

	if len(wsConfig.MonitoredWallets) == 0 {
		return map[string]interface{}{
			"success": true,
			"message": "No wallets to monitor",
		}, nil
	}

	// Load Alchemy config
	alchemyConfig, err := configLoader.GetAlchemyConfig()
	if err != nil {
		return formatErrorResponse(
			"config_error",
			"Alchemy API key is required for wallet security monitoring",
			"Configure Alchemy API key",
			map[string]interface{}{
				"skill":          "wallet_security",
				"config_command": "Set CELESTE_ALCHEMY_API_KEY=<your_key>",
			},
		), nil
	}

	// Create HTTP client
	client := &http.Client{Timeout: 30 * time.Second}

	// Get current block
	blockNumResult, err := alchemyRequest(ctx, client, alchemyConfig,
		wsConfig.MonitoredWallets[0].Network,
		"eth_blockNumber", []interface{}{})
	if err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to get current block: %v", err),
			"",
			map[string]interface{}{
				"skill": "wallet_security",
			},
		), nil
	}

	currentBlock := blockNumResult["result"].(string)

	// Determine block range to check
	fromBlock := wsConfig.LastCheckedBlock
	if fromBlock == "" {
		// First check: look back 100 blocks (~20 minutes)
		currentBlockNum := new(big.Int)
		currentBlockNum.SetString(currentBlock[2:], 16)
		fromBlockNum := new(big.Int).Sub(currentBlockNum, big.NewInt(100))
		fromBlock = fmt.Sprintf("0x%x", fromBlockNum)
	}

	// Check each wallet
	allAlerts := []SecurityAlert{}

	for _, wallet := range wsConfig.MonitoredWallets {
		alerts, err := checkWalletForThreats(ctx, client, alchemyConfig, wallet, fromBlock, "latest")
		if err != nil {
			// Log error but continue checking other wallets
			fmt.Printf("Warning: Error checking wallet %s: %v\n", wallet.Address, err)
			continue
		}
		allAlerts = append(allAlerts, alerts...)
	}

	// Save alerts
	if len(allAlerts) > 0 {
		if err := appendAlerts(allAlerts); err != nil {
			return formatErrorResponse(
				"api_error",
				fmt.Sprintf("Failed to save alerts: %v", err),
				"",
				map[string]interface{}{
					"skill": "wallet_security",
				},
			), nil
		}
	}

	// Update last checked block
	wsConfig.LastCheckedBlock = currentBlock
	if err := saveWalletSecurityConfig(wsConfig); err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to update config: %v", err),
			"",
			map[string]interface{}{
				"skill": "wallet_security",
			},
		), nil
	}

	return map[string]interface{}{
		"success":         true,
		"wallets_checked": len(wsConfig.MonitoredWallets),
		"alerts_found":    len(allAlerts),
		"alerts":          allAlerts,
		"current_block":   currentBlock,
		"message":         fmt.Sprintf("Checked %d wallet(s), found %d alert(s)", len(wsConfig.MonitoredWallets), len(allAlerts)),
	}, nil
}

// checkWalletForThreats analyzes a wallet for security threats
func checkWalletForThreats(ctx context.Context, client *http.Client, config AlchemyConfig,
	wallet MonitoredWallet, fromBlock, toBlock string) ([]SecurityAlert, error) {

	// Fetch asset transfers (both incoming and outgoing)
	params := map[string]interface{}{
		"fromBlock": fromBlock,
		"toBlock":   toBlock,
		"category":  []string{"external", "internal", "erc20", "erc721", "erc1155"},
	}

	// We need both directions, so we'll make two calls
	// First: outgoing transfers
	paramsOutgoing := make(map[string]interface{})
	for k, v := range params {
		paramsOutgoing[k] = v
	}
	paramsOutgoing["fromAddress"] = wallet.Address

	resultOutgoing, err := alchemyRequest(ctx, client, config, wallet.Network,
		"alchemy_getAssetTransfers", []interface{}{paramsOutgoing})
	if err != nil {
		return nil, fmt.Errorf("failed to get outgoing transfers: %w", err)
	}

	// Second: incoming transfers
	paramsIncoming := make(map[string]interface{})
	for k, v := range params {
		paramsIncoming[k] = v
	}
	paramsIncoming["toAddress"] = wallet.Address

	resultIncoming, err := alchemyRequest(ctx, client, config, wallet.Network,
		"alchemy_getAssetTransfers", []interface{}{paramsIncoming})
	if err != nil {
		return nil, fmt.Errorf("failed to get incoming transfers: %w", err)
	}

	// Combine transfers
	allTransfers := []interface{}{}
	if outgoingData, ok := resultOutgoing["result"].(map[string]interface{}); ok {
		if transfers, ok := outgoingData["transfers"].([]interface{}); ok {
			allTransfers = append(allTransfers, transfers...)
		}
	}
	if incomingData, ok := resultIncoming["result"].(map[string]interface{}); ok {
		if transfers, ok := incomingData["transfers"].([]interface{}); ok {
			allTransfers = append(allTransfers, transfers...)
		}
	}

	// Get current balance for large transfer detection
	balanceResult, _ := alchemyRequest(ctx, client, config, wallet.Network,
		"eth_getBalance", []interface{}{wallet.Address, "latest"})
	balanceETH := 0.0
	if balanceResult != nil {
		if resultData, ok := balanceResult["result"].(string); ok {
			weiBalance := new(big.Int)
			weiBalance.SetString(resultData[2:], 16)
			balanceETHStr := WeiToEther(weiBalance)
			balanceETH, _ = strconv.ParseFloat(balanceETHStr, 64)
		}
	}

	// Analyze each transfer for threats
	alerts := []SecurityAlert{}

	for _, t := range allTransfers {
		transfer := parseAssetTransfer(t.(map[string]interface{}))

		// Run detection algorithms
		if alert := detectDustAttack(transfer, wallet.Address); alert != nil {
			alert.WalletAddress = wallet.Address
			alert.TxHash = transfer.Hash
			alert.BlockNumber = transfer.BlockNum
			alert.ID = generateAlertID()
			alert.DetectedAt = time.Now()
			alerts = append(alerts, *alert)
		}

		if alert := detectNFTScam(transfer, wallet.Address); alert != nil {
			alert.WalletAddress = wallet.Address
			alert.TxHash = transfer.Hash
			alert.BlockNumber = transfer.BlockNum
			alert.ID = generateAlertID()
			alert.DetectedAt = time.Now()
			alerts = append(alerts, *alert)
		}

		if alert := detectLargeTransfer(transfer, wallet.Address, balanceETH); alert != nil {
			alert.WalletAddress = wallet.Address
			alert.TxHash = transfer.Hash
			alert.BlockNumber = transfer.BlockNum
			alert.ID = generateAlertID()
			alert.DetectedAt = time.Now()
			alerts = append(alerts, *alert)
		}
	}

	// Fetch and analyze token approvals
	approvalAlerts, err := checkTokenApprovals(ctx, client, config, wallet, fromBlock, toBlock)
	if err != nil {
		// Log warning but don't fail - approvals are optional enhancement
		fmt.Printf("Warning: Failed to check token approvals for %s: %v\n", wallet.Address, err)
	} else {
		alerts = append(alerts, approvalAlerts...)
	}

	return alerts, nil
}

// checkTokenApprovals fetches and analyzes ERC20 token approvals
func checkTokenApprovals(ctx context.Context, client *http.Client, config AlchemyConfig,
	wallet MonitoredWallet, fromBlock, toBlock string) ([]SecurityAlert, error) {

	// ERC20 Approval event signature: Approval(address indexed owner, address indexed spender, uint256 value)
	approvalEventSig := "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"

	// Build eth_getLogs request
	// Topic1 should be the owner address (padded to 32 bytes)
	ownerTopic := "0x" + fmt.Sprintf("%064s", wallet.Address[2:])

	logsParams := map[string]interface{}{
		"fromBlock": fromBlock,
		"toBlock":   toBlock,
		"topics": []interface{}{
			approvalEventSig, // Topic0: event signature
			ownerTopic,       // Topic1: owner (our monitored wallet)
		},
	}

	result, err := alchemyRequest(ctx, client, config, wallet.Network,
		"eth_getLogs", []interface{}{logsParams})
	if err != nil {
		return nil, fmt.Errorf("failed to get approval logs: %w", err)
	}

	// Parse logs
	logs := []interface{}{}
	if resultData, ok := result["result"].([]interface{}); ok {
		logs = resultData
	}

	// Analyze each approval
	alerts := []SecurityAlert{}
	for _, logEntry := range logs {
		logData, ok := logEntry.(map[string]interface{})
		if !ok {
			continue
		}

		approval := parseApprovalEvent(logData, wallet.Address)
		if alert := detectDangerousApproval(approval); alert != nil {
			alert.WalletAddress = wallet.Address
			alert.TxHash = approval.TxHash
			alert.BlockNumber = approval.BlockNumber
			alert.ID = generateAlertID()
			alert.DetectedAt = time.Now()
			alerts = append(alerts, *alert)
		}
	}

	return alerts, nil
}

// ApprovalEvent represents an ERC20 token approval
type ApprovalEvent struct {
	Owner         string   // Wallet that granted approval
	Spender       string   // Contract/address that can spend
	Value         *big.Int // Approved amount
	TokenContract string   // ERC20 contract address
	TxHash        string
	BlockNumber   string
	IsUnlimited   bool // True if value == max uint256
}

// parseApprovalEvent parses eth_getLogs approval event
func parseApprovalEvent(logData map[string]interface{}, ownerAddr string) ApprovalEvent {
	topics, _ := logData["topics"].([]interface{})
	data, _ := logData["data"].(string)

	event := ApprovalEvent{
		Owner:         ownerAddr,
		TxHash:        logData["transactionHash"].(string),
		BlockNumber:   logData["blockNumber"].(string),
		TokenContract: logData["address"].(string),
	}

	// Topic2 is spender address (indexed, padded to 32 bytes)
	if len(topics) > 2 {
		spenderTopic := topics[2].(string)
		event.Spender = "0x" + spenderTopic[len(spenderTopic)-40:]
	}

	// Data contains the approval value (uint256)
	if data != "" && len(data) > 2 {
		value := new(big.Int)
		value.SetString(data[2:], 16)
		event.Value = value

		// Check if unlimited approval (2^256 - 1)
		maxUint256 := new(big.Int)
		maxUint256.SetString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)
		event.IsUnlimited = value.Cmp(maxUint256) == 0
	}

	return event
}

// Detection algorithms

// detectDangerousApproval detects unlimited or high-value token approvals
func detectDangerousApproval(approval ApprovalEvent) *SecurityAlert {
	if approval.Value == nil || approval.Value.Cmp(big.NewInt(0)) == 0 {
		return nil // Zero approval (revocation) is safe
	}

	var severity string
	var description string

	if approval.IsUnlimited {
		// Unlimited approval is highest risk
		severity = "high"
		description = fmt.Sprintf("Unlimited token approval granted to %s for contract %s",
			approval.Spender, approval.TokenContract)
	} else {
		// High value approval (heuristic: > 1e18 which is 1 token with 18 decimals)
		threshold := new(big.Int)
		threshold.SetString("1000000000000000000", 10) // 1e18
		threshold.Mul(threshold, big.NewInt(1000000))  // 1 million tokens

		if approval.Value.Cmp(threshold) > 0 {
			severity = "medium"
			valueStr := approval.Value.String()
			description = fmt.Sprintf("High-value token approval (%s) granted to %s for contract %s",
				valueStr, approval.Spender, approval.TokenContract)
		} else {
			// Normal approval amount - not suspicious
			return nil
		}
	}

	return &SecurityAlert{
		AlertType:   "dangerous_approval",
		Severity:    severity,
		Description: description,
		Details: map[string]interface{}{
			"spender_address": approval.Spender,
			"token_contract":  approval.TokenContract,
			"approved_amount": approval.Value.String(),
			"is_unlimited":    approval.IsUnlimited,
		},
	}
}

// detectDustAttack detects tiny value transfers (potential address poisoning)
func detectDustAttack(transfer AssetTransfer, monitoredAddr string) *SecurityAlert {
	// Dust attack: incoming transfer with very small value
	if transfer.To != monitoredAddr {
		return nil // Not incoming
	}

	// Check value threshold
	isDust := false

	if transfer.Category == "external" || transfer.Category == "internal" {
		// ETH transfer < 0.001 ETH
		if transfer.Value < 0.001 {
			isDust = true
		}
	} else if transfer.Category == "erc20" {
		// Token transfer < 1 token (heuristic)
		if transfer.Value < 1.0 {
			isDust = true
		}
	}

	if !isDust {
		return nil
	}

	return &SecurityAlert{
		AlertType:   "dust_attack",
		Severity:    "low",
		Description: fmt.Sprintf("Potential dust attack: Received tiny amount (%f %s) from %s", transfer.Value, transfer.Asset, transfer.From),
		Details: map[string]interface{}{
			"from_address": transfer.From,
			"amount":       transfer.Value,
			"asset":        transfer.Asset,
			"category":     transfer.Category,
		},
	}
}

// detectNFTScam detects unsolicited NFT transfers
func detectNFTScam(transfer AssetTransfer, monitoredAddr string) *SecurityAlert {
	// NFT scam: incoming NFT from unknown address
	if transfer.To != monitoredAddr {
		return nil
	}

	if transfer.Category != "erc721" && transfer.Category != "erc1155" {
		return nil
	}

	// For MVP, flag all unsolicited NFTs
	contractAddr := transfer.RawContract.Address

	return &SecurityAlert{
		AlertType:   "nft_scam",
		Severity:    "medium",
		Description: fmt.Sprintf("Unsolicited NFT received from contract %s (potential scam)", contractAddr),
		Details: map[string]interface{}{
			"contract_address": contractAddr,
			"token_id":         transfer.TokenId,
			"from_address":     transfer.From,
			"category":         transfer.Category,
		},
	}
}

// detectLargeTransfer detects significant outgoing transfers
func detectLargeTransfer(transfer AssetTransfer, monitoredAddr string, balanceETH float64) *SecurityAlert {
	// Large transfer: outgoing transfer exceeding threshold
	if transfer.From != monitoredAddr {
		return nil
	}

	isLarge := false
	severity := "medium"

	if transfer.Category == "external" || transfer.Category == "internal" {
		// ETH transfer
		ethValue := transfer.Value

		// Heuristic: > 1 ETH or > 10% of balance
		if ethValue > 1.0 {
			isLarge = true
		}
		if balanceETH > 0 && ethValue > balanceETH*0.1 {
			isLarge = true
			severity = "high"
		}
		if balanceETH > 0 && ethValue > balanceETH*0.5 {
			severity = "critical"
		}
	} else if transfer.Category == "erc20" {
		// Token transfer - heuristic: > 1000 tokens
		if transfer.Value > 1000.0 {
			isLarge = true
		}
	}

	if !isLarge {
		return nil
	}

	return &SecurityAlert{
		AlertType:   "large_transfer",
		Severity:    severity,
		Description: fmt.Sprintf("Large outgoing transfer: %f %s sent to %s", transfer.Value, transfer.Asset, transfer.To),
		Details: map[string]interface{}{
			"to_address": transfer.To,
			"amount":     transfer.Value,
			"asset":      transfer.Asset,
			"category":   transfer.Category,
		},
	}
}

// handleGetSecurityAlerts retrieves security alerts
func handleGetSecurityAlerts(args map[string]interface{}) (interface{}, error) {
	alertsLog, err := loadAlertsLog()
	if err != nil {
		return map[string]interface{}{
			"success": true,
			"alerts":  []SecurityAlert{},
			"count":   0,
			"message": "No alerts found",
		}, nil
	}

	// Check if filtering for unacknowledged only
	unacknowledgedOnly, _ := args["unacknowledged_only"].(bool)

	// Filter alerts
	filteredAlerts := []SecurityAlert{}
	for _, alert := range alertsLog.Alerts {
		if unacknowledgedOnly && alert.Acknowledged {
			continue
		}
		filteredAlerts = append(filteredAlerts, alert)
	}

	// Sort by detected_at descending (most recent first)
	sort.Slice(filteredAlerts, func(i, j int) bool {
		return filteredAlerts[i].DetectedAt.After(filteredAlerts[j].DetectedAt)
	})

	return map[string]interface{}{
		"success": true,
		"alerts":  filteredAlerts,
		"count":   len(filteredAlerts),
		"message": fmt.Sprintf("Found %d alert(s)", len(filteredAlerts)),
	}, nil
}

// handleAcknowledgeAlert acknowledges an alert
func handleAcknowledgeAlert(args map[string]interface{}) (interface{}, error) {
	alertID, ok := args["alert_id"].(string)
	if !ok || alertID == "" {
		return formatErrorResponse(
			"validation_error",
			"Alert ID is required",
			"Provide an alert ID to acknowledge",
			map[string]interface{}{
				"skill":     "wallet_security",
				"operation": "acknowledge_alert",
			},
		), nil
	}

	alertsLog, err := loadAlertsLog()
	if err != nil {
		return formatErrorResponse(
			"api_error",
			"No alerts found",
			"",
			map[string]interface{}{
				"skill": "wallet_security",
			},
		), nil
	}

	found := false
	for i, alert := range alertsLog.Alerts {
		if alert.ID == alertID {
			now := time.Now()
			alertsLog.Alerts[i].Acknowledged = true
			alertsLog.Alerts[i].AcknowledgedAt = &now
			found = true
			break
		}
	}

	if !found {
		return formatErrorResponse(
			"validation_error",
			fmt.Sprintf("Alert not found: %s", alertID),
			"Check the alert ID",
			map[string]interface{}{
				"skill":    "wallet_security",
				"alert_id": alertID,
			},
		), nil
	}

	if err := saveAlertsLog(alertsLog); err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to save alerts: %v", err),
			"",
			map[string]interface{}{
				"skill": "wallet_security",
			},
		), nil
	}

	return map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Alert %s acknowledged", alertID),
	}, nil
}

// Storage functions

func loadWalletSecurityConfig() (*WalletSecurityConfig, error) {
	path := getWalletSecurityPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var config WalletSecurityConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

func saveWalletSecurityConfig(config *WalletSecurityConfig) error {
	path := getWalletSecurityPath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func loadAlertsLog() (*AlertsLog, error) {
	path := getWalletAlertsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var log AlertsLog
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, err
	}

	return &log, nil
}

func saveAlertsLog(log *AlertsLog) error {
	path := getWalletAlertsPath()

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0644)
}

func appendAlerts(newAlerts []SecurityAlert) error {
	log, err := loadAlertsLog()
	if err != nil {
		// Create new log if doesn't exist
		log = &AlertsLog{
			Alerts: []SecurityAlert{},
		}
	}

	log.Alerts = append(log.Alerts, newAlerts...)

	return saveAlertsLog(log)
}

// Utility functions

func generateAlertID() string {
	timestamp := time.Now().Unix()
	randomBytes := make([]byte, 4)
	_, _ = rand.Read(randomBytes) // crypto/rand.Read always returns nil error
	randomHex := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("alert_%d_%s", timestamp, randomHex)
}

func parseAssetTransfer(data map[string]interface{}) AssetTransfer {
	transfer := AssetTransfer{}

	if category, ok := data["category"].(string); ok {
		transfer.Category = category
	}
	if blockNum, ok := data["blockNum"].(string); ok {
		transfer.BlockNum = blockNum
	}
	if from, ok := data["from"].(string); ok {
		transfer.From = from
	}
	if to, ok := data["to"].(string); ok {
		transfer.To = to
	}
	if value, ok := data["value"].(float64); ok {
		transfer.Value = value
	}
	if asset, ok := data["asset"].(string); ok {
		transfer.Asset = asset
	}
	if hash, ok := data["hash"].(string); ok {
		transfer.Hash = hash
	}
	if rawContract, ok := data["rawContract"].(map[string]interface{}); ok {
		if address, ok := rawContract["address"].(string); ok {
			transfer.RawContract.Address = address
		}
	}
	if tokenId, ok := data["tokenId"].(string); ok {
		transfer.TokenId = tokenId
	}
	if erc721TokenId, ok := data["erc721TokenId"].(string); ok {
		transfer.ERC721TokenId = erc721TokenId
	}

	// Handle value conversion if it's a hex string
	if valueHex, ok := data["value"].(string); ok && valueHex != "" {
		// Parse hex value
		valueBig := new(big.Int)
		if len(valueHex) > 2 && valueHex[:2] == "0x" {
			valueBig.SetString(valueHex[2:], 16)
		} else {
			valueBig.SetString(valueHex, 10)
		}

		// Convert to float (simplified - assumes 18 decimals for ETH)
		valueFloat := new(big.Float).SetInt(valueBig)
		divisor := new(big.Float).SetFloat64(math.Pow10(18))
		valueFloat.Quo(valueFloat, divisor)
		transfer.Value, _ = valueFloat.Float64()
	}

	return transfer
}
