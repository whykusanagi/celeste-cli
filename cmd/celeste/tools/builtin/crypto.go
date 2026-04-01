package builtin

import (
	"context"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/skills"
	"github.com/whykusanagi/celeste-cli/cmd/celeste/tools"
)

// skillsConfigAdapter wraps builtin.ConfigLoader to satisfy skills.ConfigLoader.
// Both interfaces have identical method signatures and return types, so this
// adapter enables the existing skill handlers to work with the new ConfigLoader.
type skillsConfigAdapter struct {
	cl ConfigLoader
}

func (a *skillsConfigAdapter) GetTarotConfig() (skills.TarotConfig, error) {
	c, err := a.cl.GetTarotConfig()
	return skills.TarotConfig{FunctionURL: c.FunctionURL, AuthToken: c.AuthToken}, err
}
func (a *skillsConfigAdapter) GetVeniceConfig() (skills.VeniceConfig, error) {
	c, err := a.cl.GetVeniceConfig()
	return skills.VeniceConfig{APIKey: c.APIKey, BaseURL: c.BaseURL, Model: c.Model, ImageModel: c.ImageModel, Upscaler: c.Upscaler}, err
}
func (a *skillsConfigAdapter) GetWeatherConfig() (skills.WeatherConfig, error) {
	c, err := a.cl.GetWeatherConfig()
	return skills.WeatherConfig{DefaultZipCode: c.DefaultZipCode}, err
}
func (a *skillsConfigAdapter) GetTwitchConfig() (skills.TwitchConfig, error) {
	c, err := a.cl.GetTwitchConfig()
	return skills.TwitchConfig{ClientID: c.ClientID, ClientSecret: c.ClientSecret, DefaultStreamer: c.DefaultStreamer}, err
}
func (a *skillsConfigAdapter) GetYouTubeConfig() (skills.YouTubeConfig, error) {
	c, err := a.cl.GetYouTubeConfig()
	return skills.YouTubeConfig{APIKey: c.APIKey, DefaultChannel: c.DefaultChannel}, err
}
func (a *skillsConfigAdapter) GetIPFSConfig() (skills.IPFSConfig, error) {
	c, err := a.cl.GetIPFSConfig()
	return skills.IPFSConfig{Provider: c.Provider, APIKey: c.APIKey, APISecret: c.APISecret, ProjectID: c.ProjectID, GatewayURL: c.GatewayURL, TimeoutSeconds: c.TimeoutSeconds}, err
}
func (a *skillsConfigAdapter) GetAlchemyConfig() (skills.AlchemyConfig, error) {
	c, err := a.cl.GetAlchemyConfig()
	return skills.AlchemyConfig{APIKey: c.APIKey, DefaultNetwork: c.DefaultNetwork, TimeoutSeconds: c.TimeoutSeconds}, err
}
func (a *skillsConfigAdapter) GetBlockmonConfig() (skills.BlockmonConfig, error) {
	c, err := a.cl.GetBlockmonConfig()
	return skills.BlockmonConfig{AlchemyAPIKey: c.AlchemyAPIKey, WebhookURL: c.WebhookURL, DefaultNetwork: c.DefaultNetwork, PollIntervalSeconds: c.PollIntervalSeconds}, err
}
func (a *skillsConfigAdapter) GetWalletSecurityConfig() (skills.WalletSecuritySettingsConfig, error) {
	c, err := a.cl.GetWalletSecurityConfig()
	return skills.WalletSecuritySettingsConfig{Enabled: c.Enabled, PollInterval: c.PollInterval, AlertLevel: c.AlertLevel}, err
}

func newSkillsAdapter(cl ConfigLoader) skills.ConfigLoader {
	return &skillsConfigAdapter{cl: cl}
}

// --- IPFS Tool ---

// IPFSTool provides IPFS decentralized storage operations.
type IPFSTool struct {
	BaseTool
	configLoader ConfigLoader
}

// NewIPFSTool creates an IPFSTool.
func NewIPFSTool(configLoader ConfigLoader) *IPFSTool {
	return &IPFSTool{
		BaseTool: BaseTool{
			ToolName:        "ipfs",
			ToolDescription: "IPFS decentralized storage operations: upload content/files, download by CID, manage pins. Supports string content and binary files. Works with Infura, Pinata, and custom IPFS nodes.",
			ToolParameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"upload", "download", "pin", "unpin", "list_pins"},
						"description": "IPFS operation to perform",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "String content to upload (for upload operation with text/data)",
					},
					"file_path": map[string]interface{}{
						"type":        "string",
						"description": "Path to file to upload (for upload operation with binary files)",
					},
					"cid": map[string]interface{}{
						"type":        "string",
						"description": "Content identifier (for download, pin, unpin operations)",
					},
				},
				"required": []string{"operation"},
			}),
			ReadOnly:        false,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"operation"},
		},
		configLoader: configLoader,
	}
}

func (t *IPFSTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	result, err := skills.IPFSHandler(input, newSkillsAdapter(t.configLoader))
	if err != nil {
		return tools.ToolResult{}, err
	}
	return resultFromMap(result)
}

// --- Alchemy Tool ---

// AlchemyTool provides blockchain data and analytics via Alchemy API.
type AlchemyTool struct {
	BaseTool
	configLoader ConfigLoader
}

// NewAlchemyTool creates an AlchemyTool.
func NewAlchemyTool(configLoader ConfigLoader) *AlchemyTool {
	return &AlchemyTool{
		BaseTool: BaseTool{
			ToolName:        "alchemy",
			ToolDescription: "Blockchain data and analytics via Alchemy API: wallet tracing, token prices, NFT data, transaction monitoring across Ethereum and L2s (Arbitrum, Optimism, Polygon, Base)",
			ToolParameters: mustJSON(map[string]interface{}{
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
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"operation"},
		},
		configLoader: configLoader,
	}
}

func (t *AlchemyTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	result, err := skills.AlchemyHandler(input, newSkillsAdapter(t.configLoader))
	if err != nil {
		return tools.ToolResult{}, err
	}
	return resultFromMap(result)
}

// --- Blockmon Tool ---

// BlockmonTool provides real-time blockchain monitoring.
type BlockmonTool struct {
	BaseTool
	configLoader ConfigLoader
}

// NewBlockmonTool creates a BlockmonTool.
func NewBlockmonTool(configLoader ConfigLoader) *BlockmonTool {
	return &BlockmonTool{
		BaseTool: BaseTool{
			ToolName:        "blockmon",
			ToolDescription: "Real-time blockchain monitoring: watch addresses, get latest blocks, monitor network activity across Ethereum and L2s",
			ToolParameters: mustJSON(map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"operation": map[string]interface{}{
						"type":        "string",
						"enum":        []string{"get_latest_block", "watch_address", "get_block_by_number"},
						"description": "Monitoring operation to perform",
					},
					"network": map[string]interface{}{
						"type":        "string",
						"description": "Blockchain network to monitor (eth-mainnet, polygon-mainnet, etc.)",
					},
					"address": map[string]interface{}{
						"type":        "string",
						"description": "Address to watch (for watch_address operation)",
					},
					"blocks_history": map[string]interface{}{
						"type":        "number",
						"description": "Number of past blocks to check (default: 10)",
					},
					"block_number": map[string]interface{}{
						"type":        "string",
						"description": "Block number to fetch (hex or decimal)",
					},
				},
				"required": []string{"operation"},
			}),
			ReadOnly:        true,
			ConcurrencySafe: true,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"operation"},
		},
		configLoader: configLoader,
	}
}

func (t *BlockmonTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	result, err := skills.BlockmonHandler(input, newSkillsAdapter(t.configLoader))
	if err != nil {
		return tools.ToolResult{}, err
	}
	return resultFromMap(result)
}

// --- WalletSecurity Tool ---

// WalletSecurityTool monitors wallet addresses for security threats.
type WalletSecurityTool struct {
	BaseTool
	configLoader ConfigLoader
}

// NewWalletSecurityTool creates a WalletSecurityTool.
func NewWalletSecurityTool(configLoader ConfigLoader) *WalletSecurityTool {
	return &WalletSecurityTool{
		BaseTool: BaseTool{
			ToolName:        "wallet_security",
			ToolDescription: "Monitor wallet addresses for security threats: dust attacks, NFT scams, dangerous approvals, large transfers across Ethereum and L2s",
			ToolParameters: mustJSON(map[string]interface{}{
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
			}),
			ReadOnly:        false,
			ConcurrencySafe: false,
			Interrupt:       tools.InterruptCancel,
			RequiredFields:  []string{"operation"},
		},
		configLoader: configLoader,
	}
}

func (t *WalletSecurityTool) Execute(ctx context.Context, input map[string]any, progress chan<- tools.ProgressEvent) (tools.ToolResult, error) {
	result, err := skills.WalletSecurityHandler(input, newSkillsAdapter(t.configLoader))
	if err != nil {
		return tools.ToolResult{}, err
	}
	return resultFromMap(result)
}
