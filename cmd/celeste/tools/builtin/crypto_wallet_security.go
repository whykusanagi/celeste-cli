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

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
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
	// Key identifies the on-chain event (network, wallet, type, tx and the
	// transfer or log within it); an event is stored once.
	Key string `json:"key,omitempty"`
}

// maxStoredAlerts bounds the alerts log; the oldest are dropped first.
const maxStoredAlerts = 1000

// maxReturnedAlerts bounds what get_security_alerts returns (newest first).
const maxReturnedAlerts = 100

// alertKey is a's dedup key; alerts saved before keys existed fall back to
// wallet, type, tx and block.
func alertKey(a SecurityAlert) string {
	if a.Key != "" {
		return a.Key
	}
	return strings.Join([]string{"legacy", strings.ToLower(a.WalletAddress), a.AlertType, strings.ToLower(a.TxHash), a.BlockNumber}, "|")
}

// eventKey builds a SecurityAlert.Key.
func eventKey(network, wallet, alertType, txHash, sub string) string {
	return strings.Join([]string{network, strings.ToLower(wallet), alertType, strings.ToLower(txHash), sub}, "|")
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
	UniqueID        string // Alchemy's per-transfer id within the tx
	RawContract     struct{ Address string }
	TokenId         string
	ERC721TokenId   string
	ERC1155Metadata []struct {
		TokenId string
		Value   string
	}
}

// maxAssetTransferPages bounds the alchemy_getAssetTransfers continuation
// pages read per direction in one scan. A range that needs more is scanned
// up to the last complete block, and the next scan continues from there.
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
	partial := map[string]string{} // network -> block scanned to, short of current

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
		// The checkpoint moves to the last block every wallet on the
		// network was fully scanned to: currentBlock, or less for a wallet
		// with more transfers than one scan reads.
		checkpoint, _ := parseHexBlock(currentBlock)
		for _, wallet := range wallets {
			alerts, scannedTo, err := checkWalletForThreats(ctx, client, alchemyConfig, wallet, fromBlock, currentBlock)
			if err != nil {
				// Keep checking the other wallets, but the range is not done.
				failed = append(failed, map[string]any{"wallet": wallet.Address, "network": network, "error": err.Error()})
				networkOK = false
				continue
			}
			allAlerts = append(allAlerts, alerts...)
			if b, ok := parseHexBlock(scannedTo); ok && b.Cmp(checkpoint) < 0 {
				checkpoint = b
			}
		}
		if networkOK {
			wsConfig.LastCheckedBlocks[network] = fmt.Sprintf("0x%x", checkpoint)
			if hex := fmt.Sprintf("0x%x", checkpoint); hex != currentBlocks[network] {
				partial[network] = hex
			}
		}
	}

	// Save alerts
	if len(allAlerts) > 0 {
		added, err := appendAlerts(allAlerts)
		if err != nil {
			return formatErrorResponse(
				"api_error",
				fmt.Sprintf("Failed to save alerts: %v", err),
				"",
				map[string]any{
					"skill": "wallet_security",
				},
			), nil
		}
		// Report only events not seen before (a rescanned range repeats).
		allAlerts = added
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

	message := fmt.Sprintf("Checked %d wallet(s), found %d alert(s)", len(wsConfig.MonitoredWallets), len(allAlerts))
	if len(partial) > 0 {
		message += "; some networks had more transfers than one scan reads and were scanned part of the way, the next scan continues from there"
	}
	out := map[string]any{
		"success":         true,
		"wallets_checked": len(wsConfig.MonitoredWallets),
		"alerts_found":    len(allAlerts),
		"alerts":          allAlerts,
		"current_block":   currentBlocks[firstNetwork],
		"current_blocks":  currentBlocks,
		"message":         message,
	}
	if len(partial) > 0 {
		out["scanned_to"] = partial
	}
	return out, nil
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

// checkWalletForThreats analyzes a wallet for security threats in the
// blocks fromBlock..toBlock (0x-hex, inclusive). It returns the alerts and
// the last block it fully scanned: toBlock, or earlier when the range holds
// more transfers than maxAssetTransferPages pages, in which case the rest
// is left for the next scan.
func checkWalletForThreats(ctx context.Context, client *http.Client, config AlchemyConfig,
	wallet MonitoredWallet, fromBlock, toBlock string) ([]SecurityAlert, string, error) {
	from, okFrom := parseHexBlock(fromBlock)
	to, okTo := parseHexBlock(toBlock)
	if !okFrom || !okTo {
		return nil, "", fmt.Errorf("invalid block range %q..%q", fromBlock, toBlock)
	}

	// Fetch asset transfers (both incoming and outgoing), oldest first
	params := map[string]any{
		"fromBlock": fromBlock,
		"toBlock":   toBlock,
		"order":     "asc",
		"category":  []string{"external", "internal", "erc20", "erc721", "erc1155"},
	}

	// Both directions, every continuation page of each.
	outgoing, outDone, err := fetchAssetTransfers(ctx, client, config, wallet.Network, params, "fromAddress", wallet.Address)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get outgoing transfers: %w", err)
	}
	incoming, inDone, err := fetchAssetTransfers(ctx, client, config, wallet.Network, params, "toAddress", wallet.Address)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get incoming transfers: %w", err)
	}

	// A direction cut off at the page cap was read up to some block; the
	// block before the last one seen is complete. The scan ends there.
	scannedTo := new(big.Int).Set(to)
	for _, cut := range []struct {
		done bool
		list []any
	}{{outDone, outgoing}, {inDone, incoming}} {
		if cut.done {
			continue
		}
		last, ok := lastTransferBlock(cut.list)
		if !ok {
			return nil, "", fmt.Errorf("more than %d pages of transfers without block numbers", maxAssetTransferPages)
		}
		if bound := new(big.Int).Sub(last, big.NewInt(1)); bound.Cmp(scannedTo) < 0 {
			scannedTo = bound
		}
	}
	if scannedTo.Cmp(from) < 0 {
		return nil, "", fmt.Errorf("more than %d pages of transfers in block %s", maxAssetTransferPages, fromBlock)
	}
	allTransfers := make([]any, 0, len(outgoing)+len(incoming))
	for _, t := range append(outgoing, incoming...) {
		if m, ok := t.(map[string]any); ok {
			if b, ok := parseHexBlock(stringField(m, "blockNum")); ok && b.Cmp(scannedTo) > 0 {
				continue // past the scanned range: read again next scan
			}
		}
		allTransfers = append(allTransfers, t)
	}
	toBlock = fmt.Sprintf("0x%x", scannedTo)

	// Get current balance for large transfer detection
	// (it decides large-transfer detection, so a failure fails the scan).
	balanceResult, err := alchemyRequest(ctx, client, config, wallet.Network,
		"eth_getBalance", []any{wallet.Address, "latest"})
	if err != nil {
		return nil, "", fmt.Errorf("failed to get balance: %w", err)
	}
	resultData, _ := balanceResult["result"].(string)
	weiBalance, ok := new(big.Int), len(resultData) > 2 && resultData[:2] == "0x"
	if ok {
		_, ok = weiBalance.SetString(resultData[2:], 16)
	}
	if !ok {
		return nil, "", fmt.Errorf("unexpected eth_getBalance result %q", resultData)
	}
	balanceETH, _ := strconv.ParseFloat(WeiToEther(weiBalance), 64)

	// Analyze each transfer for threats
	alerts := []SecurityAlert{}

	for _, t := range allTransfers {
		data, ok := t.(map[string]any)
		if !ok {
			continue
		}
		transfer := parseAssetTransfer(data)
		transferSub := transfer.UniqueID
		if transferSub == "" {
			transferSub = strings.Join([]string{transfer.Category, strings.ToLower(transfer.From), strings.ToLower(transfer.To),
				transfer.Asset, strings.ToLower(transfer.RawContract.Address), transfer.TokenId, strconv.FormatFloat(transfer.Value, 'g', -1, 64)}, "/")
		}

		// Run detection algorithms
		if alert := detectDustAttack(transfer, wallet.Address); alert != nil {
			alert.WalletAddress = wallet.Address
			alert.TxHash = transfer.Hash
			alert.BlockNumber = transfer.BlockNum
			alert.ID = generateAlertID()
			alert.Key = eventKey(wallet.Network, wallet.Address, alert.AlertType, transfer.Hash, transferSub)
			alert.DetectedAt = time.Now()
			alerts = append(alerts, *alert)
		}

		if alert := detectNFTScam(transfer, wallet.Address); alert != nil {
			alert.WalletAddress = wallet.Address
			alert.TxHash = transfer.Hash
			alert.BlockNumber = transfer.BlockNum
			alert.ID = generateAlertID()
			alert.Key = eventKey(wallet.Network, wallet.Address, alert.AlertType, transfer.Hash, transferSub)
			alert.DetectedAt = time.Now()
			alerts = append(alerts, *alert)
		}

		if alert := detectLargeTransfer(transfer, wallet.Address, balanceETH); alert != nil {
			alert.WalletAddress = wallet.Address
			alert.TxHash = transfer.Hash
			alert.BlockNumber = transfer.BlockNum
			alert.ID = generateAlertID()
			alert.Key = eventKey(wallet.Network, wallet.Address, alert.AlertType, transfer.Hash, transferSub)
			alert.DetectedAt = time.Now()
			alerts = append(alerts, *alert)
		}
	}

	// Fetch and analyze token approvals
	approvalAlerts, err := checkTokenApprovals(ctx, client, config, wallet, fromBlock, toBlock)
	if err != nil {
		return nil, "", fmt.Errorf("failed to check token approvals: %w", err)
	}
	alerts = append(alerts, approvalAlerts...)

	return alerts, toBlock, nil
}

// parseHexBlock parses a 0x-hex block number.
func parseHexBlock(s string) (*big.Int, bool) {
	if len(s) < 3 || (s[:2] != "0x" && s[:2] != "0X") {
		return nil, false
	}
	n, ok := new(big.Int).SetString(s[2:], 16)
	return n, ok && n.Sign() >= 0
}

func stringField(m map[string]any, k string) string {
	v, _ := m[k].(string)
	return v
}

// lastTransferBlock is the highest blockNum among transfers.
func lastTransferBlock(transfers []any) (*big.Int, bool) {
	var last *big.Int
	for _, t := range transfers {
		m, ok := t.(map[string]any)
		if !ok {
			continue
		}
		if b, ok := parseHexBlock(stringField(m, "blockNum")); ok && (last == nil || b.Cmp(last) > 0) {
			last = b
		}
	}
	return last, last != nil
}

// fetchAssetTransfers reads the pages of alchemy_getAssetTransfers for one
// direction (addrField is "fromAddress" or "toAddress"), following pageKey
// up to maxAssetTransferPages. complete is false when pages remain.
func fetchAssetTransfers(ctx context.Context, client *http.Client, config AlchemyConfig,
	network string, base map[string]any, addrField, address string) (transfers []any, complete bool, err error) {
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
			return nil, false, err
		}
		data, ok := result["result"].(map[string]any)
		if !ok {
			return nil, false, fmt.Errorf("unexpected alchemy_getAssetTransfers response")
		}
		if page, ok := data["transfers"].([]any); ok {
			all = append(all, page...)
		}
		next, _ := data["pageKey"].(string)
		if next == "" {
			return all, true, nil
		}
		if next == pageKey {
			return nil, false, fmt.Errorf("alchemy_getAssetTransfers repeated page key")
		}
		pageKey = next
	}
	return all, false, nil
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
			alert.Key = eventKey(wallet.Network, wallet.Address, alert.AlertType, approval.TxHash, "log:"+approval.LogIndex)
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
	LogIndex      string
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

	event.LogIndex, _ = logData["logIndex"].(string)

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

	total := len(filteredAlerts)
	truncated := total > maxReturnedAlerts
	if truncated {
		filteredAlerts = filteredAlerts[:maxReturnedAlerts]
	}
	message := fmt.Sprintf("Found %d alert(s)", total)
	if truncated {
		message = fmt.Sprintf("Found %d alert(s); showing the newest %d", total, maxReturnedAlerts)
	}

	return map[string]any{
		"success":   true,
		"alerts":    filteredAlerts,
		"count":     len(filteredAlerts),
		"total":     total,
		"truncated": truncated,
		"message":   message,
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

	return atomicfile.Write(path, data, 0o600)
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

	return atomicfile.Write(path, data, 0o600)
}

// appendAlerts stores the alerts whose event is not stored yet and returns
// them. The log keeps the newest maxStoredAlerts.
func appendAlerts(newAlerts []SecurityAlert) ([]SecurityAlert, error) {
	log, err := loadAlertsLog()
	if err != nil {
		// Create new log if doesn't exist
		log = &AlertsLog{
			Alerts: []SecurityAlert{},
		}
	}

	seen := make(map[string]bool, len(log.Alerts)+len(newAlerts))
	for _, a := range log.Alerts {
		seen[alertKey(a)] = true
	}
	added := []SecurityAlert{}
	for _, a := range newAlerts {
		k := alertKey(a)
		if seen[k] {
			continue
		}
		seen[k] = true
		added = append(added, a)
	}
	if len(added) == 0 {
		return added, nil
	}

	log.Alerts = append(log.Alerts, added...)
	if over := len(log.Alerts) - maxStoredAlerts; over > 0 {
		log.Alerts = append([]SecurityAlert(nil), log.Alerts[over:]...)
	}
	return added, saveAlertsLog(log)
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
	if uid, ok := data["uniqueId"].(string); ok {
		transfer.UniqueID = uid
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
