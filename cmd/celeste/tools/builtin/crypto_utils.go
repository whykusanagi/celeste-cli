// Package builtin provides crypto utility functions using modern Go crypto libraries
package builtin

import (
	"fmt"
	"math/big"
	"strings"

	// Use official Ethereum Go implementation for address handling
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
)

// Supported Alchemy networks with chain IDs
var AlchemyNetworks = map[string]struct {
	Name    string
	ChainID int64
}{
	"eth-mainnet":      {"Ethereum Mainnet", 1},
	"eth-sepolia":      {"Ethereum Sepolia Testnet", 11155111},
	"polygon-mainnet":  {"Polygon Mainnet", 137},
	"polygon-amoy":     {"Polygon Amoy Testnet", 80002},
	"arbitrum-mainnet": {"Arbitrum One", 42161},
	"arbitrum-sepolia": {"Arbitrum Sepolia", 421614},
	"optimism-mainnet": {"Optimism Mainnet", 10},
	"optimism-sepolia": {"Optimism Sepolia", 11155420},
	"base-mainnet":     {"Base Mainnet", 8453},
	"base-sepolia":     {"Base Sepolia", 84532},
}

// ValidateAlchemyNetwork checks if a network identifier is valid
func ValidateAlchemyNetwork(network string) error {
	if _, ok := AlchemyNetworks[network]; !ok {
		return fmt.Errorf("unsupported network: %s", network)
	}
	return nil
}

// BuildAlchemyURL constructs the Alchemy API URL
func BuildAlchemyURL(network, apiKey string) string {
	return fmt.Sprintf("https://%s.g.alchemy.com/v2/%s", network, apiKey)
}

// NormalizeAddress returns a checksummed Ethereum address using EIP-55
// This is the proper way to handle Ethereum addresses
func NormalizeAddress(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if !common.IsHexAddress(addr) {
		return "", fmt.Errorf("invalid Ethereum address: %s", addr)
	}
	// Convert to common.Address and get checksummed string
	return common.HexToAddress(addr).Hex(), nil
}

// WeiToEther converts Wei (*big.Int) to Ether as a formatted string
// Uses params.Ether from go-ethereum for accurate conversion
func WeiToEther(wei *big.Int) string {
	if wei == nil {
		return "0"
	}
	// Use go-ethereum's params.Ether constant (10^18)
	ether := new(big.Float).SetInt(wei)
	etherDivisor := new(big.Float).SetInt(big.NewInt(params.Ether))
	ether.Quo(ether, etherDivisor)
	return ether.Text('f', 18)
}

// WeiToGwei converts Wei to Gwei (useful for displaying gas prices)
func WeiToGwei(wei *big.Int) int64 {
	gwei := new(big.Int).Div(wei, big.NewInt(params.GWei))
	return gwei.Int64()
}
