// Package builtin provides wallet security monitoring handler implementation
package builtin

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
	"strings"
	"time"
)

// WalletSecurityConfig holds wallet security configuration
type WalletSecurityConfig struct {
	MonitoredWallets []MonitoredWallet `json:"monitored_wallets"`
	LastCheckedBlock string            `json:"last_checked_block"`
	// LastCheckedBlocks is the scan checkpoint per network (the last block
	// fully scanned for every wallet on it).
	LastCheckedBlocks   map[string]string `json:"last_checked_blocks,omitempty"`
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
	ID             string         `json:"id"`             // alert_<timestamp>_<random>
	WalletAddress  string         `json:"wallet_address"` // Affected wallet
	AlertType      string         `json:"alert_type"`     // dust_attack, nft_scam, dangerous_approval, large_transfer
	Severity       string         `json:"severity"`       // low, medium, high, critical
	TxHash         string         `json:"tx_hash"`        // Transaction hash
	BlockNumber    string         `json:"block_number"`
	Description    string         `json:"description"` // Human-readable description
	Details        map[string]any `json:"details"`     // Type-specific details
	DetectedAt     time.Time      `json:"detected_at"`
	Acknowledged   bool           `json:"acknowledged"`
	AcknowledgedAt *time.Time     `json:"acknowledged_at,omitempty"`
}

// AlertsLog stores all security alerts
type AlertsLog struct {
	Alerts []SecurityAlert `json:"alerts"`
}

// AssetTransfer represents a blockchain asset transfer
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

// maxAssetTransferPages bounds the alchemy_getAssetTransfers continuation
// pages read per direction in one scan. A scan that needs more fails, so the
// checkpoint stays put and the range is scanned again.
const maxAssetTransferPages = 50

// walletSecurityClient is the HTTP client a scan uses; tests swap it.
var walletSecurityClient = func() *http.Client { return &http.Client{Timeout: 30 * time.Second} }

// Storage path helpers
func getWalletSecurityPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "wallet_security.json")
}

func getWalletAlertsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".celeste", "wallet_alerts.json")
}

// WalletSecurityHandlerFunc is the exported entry point for wallet security operations.
// It is used by the monitor daemon to perform periodic wallet checks.
func WalletSecurityHandlerFunc(args map[string]any, configLoader ConfigLoader) (any, error) {
	return walletSecurityHandler(args, configLoader)
}

// walletSecurityHandler handles wallet security skill execution
func walletSecurityHandler(args map[string]any, configLoader ConfigLoader) (any, error) {
	// Get operation
	operation, ok := args["operation"].(string)
	if !ok || operation == "" {
		return formatErrorResponse(
			"validation_error",
			"Operation is required",
			"Specify a wallet security operation",
			map[string]any{
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
			map[string]any{
				"skill":     "wallet_security",
				"operation": operation,
			},
		), nil
	}
}

// handleAddMonitoredWallet adds a wallet to the monitoring list
func handleAddMonitoredWallet(args map[string]any) (any, error) {
	// Get and validate address
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return formatErrorResponse(
			"validation_error",
			"Address is required",
			"Provide an Ethereum address to monitor",
			map[string]any{
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
			map[string]any{
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
			map[string]any{
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
				map[string]any{
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
			map[string]any{
				"skill": "wallet_security",
			},
		), nil
	}

	return map[string]any{
		"success": true,
		"message": fmt.Sprintf("Now monitoring wallet: %s (%s) on %s", label, normalizedAddr, network),
		"wallet":  wallet,
	}, nil
}

// handleRemoveMonitoredWallet removes a wallet from the monitoring list
func handleRemoveMonitoredWallet(args map[string]any) (any, error) {
	// Get and validate address
	address, ok := args["address"].(string)
	if !ok || address == "" {
		return formatErrorResponse(
			"validation_error",
			"Address is required",
			"Provide an Ethereum address to remove",
			map[string]any{
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
			map[string]any{
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
			map[string]any{
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
			map[string]any{
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
			map[string]any{
				"skill": "wallet_security",
			},
		), nil
	}

	return map[string]any{
		"success": true,
		"message": fmt.Sprintf("Removed wallet from monitoring: %s", normalizedAddr),
	}, nil
}

// handleListMonitoredWallets lists all monitored wallets
func handleListMonitoredWallets() (any, error) {
	config, err := loadWalletSecurityConfig()
	if err != nil {
		return map[string]any{
			"success": true,
			"wallets": []MonitoredWallet{},
			"count":   0,
			"message": "No wallets configured for monitoring",
		}, nil
	}

	return map[string]any{
		"success": true,
		"wallets": config.MonitoredWallets,
		"count":   len(config.MonitoredWallets),
		"message": fmt.Sprintf("Monitoring %d wallet(s)", len(config.MonitoredWallets)),
	}, nil
}

// handleCheckWalletSecurity checks all monitored wallets for security threats
func handleCheckWalletSecurity(ctx context.Context, configLoader ConfigLoader) (any, error) {
	// Load wallet security config
	wsConfig, err := loadWalletSecurityConfig()
	if err != nil {
		return formatErrorResponse(
			"api_error",
			"No wallets configured for monitoring",
			"Add a wallet first using add_monitored_wallet",
			map[string]any{
				"skill": "wallet_security",
			},
		), nil
	}

	if len(wsConfig.MonitoredWallets) == 0 {
		return map[string]any{
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
			map[string]any{
				"skill":          "wallet_security",
				"config_command": "Set CELESTE_ALCHEMY_API_KEY=<your_key>",
			},
		), nil
	}

	client := walletSecurityClient()

	// Each network has its own block height, so each has its own checkpoint.
	// A network's checkpoint moves only when every wallet on it was scanned.
	networks := []string{}
	byNetwork := map[string][]MonitoredWallet{}
	for _, w := range wsConfig.MonitoredWallets {
		if _, ok := byNetwork[w.Network]; !ok {
			networks = append(networks, w.Network)
		}
		byNetwork[w.Network] = append(byNetwork[w.Network], w)
	}
	if wsConfig.LastCheckedBlocks == nil {
		wsConfig.LastCheckedBlocks = map[string]string{}
	}
	firstNetwork := wsConfig.MonitoredWallets[0].Network
	if wsConfig.LastCheckedBlock != "" {
		// The legacy single checkpoint was taken on the first wallet's network.
		if _, ok := wsConfig.LastCheckedBlocks[firstNetwork]; !ok {
			wsConfig.LastCheckedBlocks[firstNetwork] = wsConfig.LastCheckedBlock
		}
	}

	allAlerts := []SecurityAlert{}
	failed := []map[string]any{}
	currentBlocks := map[string]string{}

	for _, network := range networks {
		wallets := byNetwork[network]
		currentBlock, err := currentBlockNumber(ctx, client, alchemyConfig, network)
		if err != nil {
			for _, w := range wallets {
				failed = append(failed, map[string]any{"wallet": w.Address, "network": network,
					"error": fmt.Sprintf("failed to get current block: %v", err)})
			}
			continue
		}
		currentBlocks[network] = currentBlock

		fromBlock := wsConfig.LastCheckedBlocks[network]
		if fromBlock == "" {
			// First check: look back 100 blocks (~20 minutes on mainnet).
			fromBlock = lookBackBlocks(currentBlock, 100)
		}

		networkOK := true
		for _, wallet := range wallets {
			// The range ends at currentBlock, where the checkpoint moves to.
			alerts, err := checkWalletForThreats(ctx, client, alchemyConfig, wallet, fromBlock, currentBlock)
			if err != nil {
				// Keep checking the other wallets, but the range is not done.
				failed = append(failed, map[string]any{"wallet": wallet.Address, "network": network, "error": err.Error()})
				networkOK = false
				continue
			}
			allAlerts = append(allAlerts, alerts...)
		}
		if networkOK {
			wsConfig.LastCheckedBlocks[network] = currentBlock
		}
	}

	// Save alerts
	if len(allAlerts) > 0 {
		if err := appendAlerts(allAlerts); err != nil {
			return formatErrorResponse(
				"api_error",
				fmt.Sprintf("Failed to save alerts: %v", err),
				"",
				map[string]any{
					"skill": "wallet_security",
				},
			), nil
		}
	}

	// Save the checkpoints that moved. The legacy field mirrors the first
	// network's checkpoint.
	wsConfig.LastCheckedBlock = wsConfig.LastCheckedBlocks[firstNetwork]
	if err := saveWalletSecurityConfig(wsConfig); err != nil {
		return formatErrorResponse(
			"api_error",
			fmt.Sprintf("Failed to update config: %v", err),
			"",
			map[string]any{
				"skill": "wallet_security",
			},
		), nil
	}

	if len(failed) > 0 {
		// A failed scan leaves its network's checkpoint where it was, so
		// the same range is scanned again next time.
		return map[string]any{
			"success":         false,
			"error":           true,
			"error_type":      "scan_incomplete",
			"wallets_checked": len(wsConfig.MonitoredWallets) - len(failed),
			"wallets_failed":  failed,
			"alerts_found":    len(allAlerts),
			"alerts":          allAlerts,
			"current_blocks":  currentBlocks,
			"message": fmt.Sprintf("Scan incomplete: %d of %d wallet(s) could not be checked; their network's checkpoint was not advanced, so that range will be scanned again",
				len(failed), len(wsConfig.MonitoredWallets)),
		}, nil
	}

	return map[string]any{
		"success":         true,
		"wallets_checked": len(wsConfig.MonitoredWallets),
		"alerts_found":    len(allAlerts),
		"alerts":          allAlerts,
		"current_block":   currentBlocks[firstNetwork],
		"current_blocks":  currentBlocks,
		"message":         fmt.Sprintf("Checked %d wallet(s), found %d alert(s)", len(wsConfig.MonitoredWallets), len(allAlerts)),
	}, nil
}

// currentBlockNumber returns a network's latest block as a 0x-hex string.
func currentBlockNumber(ctx context.Context, client *http.Client, config AlchemyConfig, network string) (string, error) {
	res, err := alchemyRequest(ctx, client, config, network, "eth_blockNumber", []any{})
	if err != nil {
		return "", err
	}
	block, _ := res["result"].(string)
	if len(block) < 3 || block[:2] != "0x" {
		return "", fmt.Errorf("unexpected eth_blockNumber result %q", block)
	}
	if _, ok := new(big.Int).SetString(block[2:], 16); !ok {
		return "", fmt.Errorf("unexpected eth_blockNumber result %q", block)
	}
	return block, nil
}

// lookBackBlocks is block minus n (not below 0), as 0x-hex.
func lookBackBlocks(block string, n int64) string {
	num, _ := new(big.Int).SetString(block[2:], 16)
	from := new(big.Int).Sub(num, big.NewInt(n))
	if from.Sign() < 0 {
		from.SetInt64(0)
	}
	return fmt.Sprintf("0x%x", from)
}

// checkWalletForThreats analyzes a wallet for security threats
func checkWalletForThreats(ctx context.Context, client *http.Client, config AlchemyConfig,
	wallet MonitoredWallet, fromBlock, toBlock string) ([]SecurityAlert, error) {

	// Fetch asset transfers (both incoming and outgoing)
	params := map[string]any{
		"fromBlock": fromBlock,
		"toBlock":   toBlock,
		"category":  []string{"external", "internal", "erc20", "erc721", "erc1155"},
	}

	// Both directions, every continuation page of each.
	outgoing, err := fetchAssetTransfers(ctx, client, config, wallet.Network, params, "fromAddress", wallet.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to get outgoing transfers: %w", err)
	}
	incoming, err := fetchAssetTransfers(ctx, client, config, wallet.Network, params, "toAddress", wallet.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to get incoming transfers: %w", err)
	}
	allTransfers := append(outgoing, incoming...)

	// Get current balance for large transfer detection
	balanceResult, _ := alchemyRequest(ctx, client, config, wallet.Network,
		"eth_getBalance", []any{wallet.Address, "latest"})
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
		data, ok := t.(map[string]any)
		if !ok {
			continue
		}
		transfer := parseAssetTransfer(data)

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
		return nil, fmt.Errorf("failed to check token approvals: %w", err)
	}
	alerts = append(alerts, approvalAlerts...)

	return alerts, nil
}

// fetchAssetTransfers reads every page of alchemy_getAssetTransfers for one
// direction (addrField is "fromAddress" or "toAddress"), following pageKey
// up to maxAssetTransferPages. Pagination that does not finish is an error.
func fetchAssetTransfers(ctx context.Context, client *http.Client, config AlchemyConfig,
	network string, base map[string]any, addrField, address string) ([]any, error) {
	var all []any
	pageKey := ""
	for page := 0; page < maxAssetTransferPages; page++ {
		p := make(map[string]any, len(base)+2)
		for k, v := range base {
			p[k] = v
		}
		p[addrField] = address
		if pageKey != "" {
			p["pageKey"] = pageKey
		}
		result, err := alchemyRequest(ctx, client, config, network, "alchemy_getAssetTransfers", []any{p})
		if err != nil {
			return nil, err
		}
		data, ok := result["result"].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected alchemy_getAssetTransfers response")
		}
		if transfers, ok := data["transfers"].([]any); ok {
			all = append(all, transfers...)
		}
		next, _ := data["pageKey"].(string)
		if next == "" {
			return all, nil
		}
		if next == pageKey {
			return nil, fmt.Errorf("alchemy_getAssetTransfers repeated page key")
		}
		pageKey = next
	}
	return nil, fmt.Errorf("more than %d pages of transfers in one scan", maxAssetTransferPages)
}

// checkTokenApprovals fetches and analyzes ERC20 token approvals
func checkTokenApprovals(ctx context.Context, client *http.Client, config AlchemyConfig,
	wallet MonitoredWallet, fromBlock, toBlock string) ([]SecurityAlert, error) {

	// ERC20 Approval event signature: Approval(address indexed owner, address indexed spender, uint256 value)
	approvalEventSig := "0x8c5be1e5ebec7d5bd14f71427d1e84f3dd0314c0f7b2291e5b200ac8c7c3b925"

	// Build eth_getLogs request
	// Topic1 should be the owner address (padded to 32 bytes)
	ownerTopic := "0x" + fmt.Sprintf("%064s", wallet.Address[2:])

	logsParams := map[string]any{
		"fromBlock": fromBlock,
		"toBlock":   toBlock,
		"topics": []any{
			approvalEventSig, // Topic0: event signature
			ownerTopic,       // Topic1: owner (our monitored wallet)
		},
	}

	result, err := alchemyRequest(ctx, client, config, wallet.Network,
		"eth_getLogs", []any{logsParams})
	if err != nil {
		return nil, fmt.Errorf("failed to get approval logs: %w", err)
	}

	// Parse logs
	logs := []any{}
	if resultData, ok := result["result"].([]any); ok {
		logs = resultData
	}

	// Analyze each approval
	alerts := []SecurityAlert{}
	for _, logEntry := range logs {
		logData, ok := logEntry.(map[string]any)
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
func parseApprovalEvent(logData map[string]any, ownerAddr string) ApprovalEvent {
	topics, _ := logData["topics"].([]any)
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
		Details: map[string]any{
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
	if !strings.EqualFold(transfer.To, monitoredAddr) {
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
		Details: map[string]any{
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
	if !strings.EqualFold(transfer.To, monitoredAddr) {
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
		Details: map[string]any{
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
	if !strings.EqualFold(transfer.From, monitoredAddr) {
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
		Details: map[string]any{
			"to_address": transfer.To,
			"amount":     transfer.Value,
			"asset":      transfer.Asset,
			"category":   transfer.Category,
		},
	}
}

// handleGetSecurityAlerts retrieves security alerts
func handleGetSecurityAlerts(args map[string]any) (any, error) {
	alertsLog, err := loadAlertsLog()
	if err != nil {
		return map[string]any{
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

	return map[string]any{
		"success": true,
		"alerts":  filteredAlerts,
		"count":   len(filteredAlerts),
		"message": fmt.Sprintf("Found %d alert(s)", len(filteredAlerts)),
	}, nil
}

// handleAcknowledgeAlert acknowledges an alert
func handleAcknowledgeAlert(args map[string]any) (any, error) {
	alertID, ok := args["alert_id"].(string)
	if !ok || alertID == "" {
		return formatErrorResponse(
			"validation_error",
			"Alert ID is required",
			"Provide an alert ID to acknowledge",
			map[string]any{
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
			map[string]any{
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
			map[string]any{
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
			map[string]any{
				"skill": "wallet_security",
			},
		), nil
	}

	return map[string]any{
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

func parseAssetTransfer(data map[string]any) AssetTransfer {
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
	if rawContract, ok := data["rawContract"].(map[string]any); ok {
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
