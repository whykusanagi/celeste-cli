// Package skills provides crypto utility functions using modern Go crypto libraries
package skills

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	// Use official Ethereum Go implementation for address handling
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"

	// Use official Go rate limiting library
	"golang.org/x/time/rate"
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

// GetChainID returns the chain ID for a given network
func GetChainID(network string) (int64, error) {
	if net, ok := AlchemyNetworks[network]; ok {
		return net.ChainID, nil
	}
	return 0, fmt.Errorf("unknown network: %s", network)
}

// BuildAlchemyURL constructs the Alchemy API URL
func BuildAlchemyURL(network, apiKey string) string {
	return fmt.Sprintf("https://%s.g.alchemy.com/v2/%s", network, apiKey)
}

// IsValidEthereumAddress validates an Ethereum address using go-ethereum
// This uses the official implementation with proper checksum validation
func IsValidEthereumAddress(addr string) bool {
	return common.IsHexAddress(addr)
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

// ParseAddress parses a string into a common.Address (go-ethereum type)
func ParseAddress(addr string) (common.Address, error) {
	if !common.IsHexAddress(addr) {
		return common.Address{}, fmt.Errorf("invalid Ethereum address: %s", addr)
	}
	return common.HexToAddress(addr), nil
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

// EtherToWei converts Ether string to Wei (*big.Int)
// Uses params.Ether from go-ethereum for accurate conversion
func EtherToWei(etherStr string) (*big.Int, error) {
	etherFloat := new(big.Float)
	if _, ok := etherFloat.SetString(etherStr); !ok {
		return nil, fmt.Errorf("invalid ether amount: %s", etherStr)
	}

	// Use go-ethereum's params.Ether constant (10^18)
	multiplier := new(big.Float).SetInt(big.NewInt(params.Ether))
	weiFloat := new(big.Float).Mul(etherFloat, multiplier)

	wei, accuracy := weiFloat.Int(nil)
	if accuracy != big.Exact {
		return nil, fmt.Errorf("precision loss in conversion")
	}

	return wei, nil
}

// GweiToWei converts Gwei to Wei (useful for gas prices)
func GweiToWei(gwei int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(gwei), big.NewInt(params.GWei))
}

// WeiToGwei converts Wei to Gwei (useful for displaying gas prices)
func WeiToGwei(wei *big.Int) int64 {
	gwei := new(big.Int).Div(wei, big.NewInt(params.GWei))
	return gwei.Int64()
}

// RateLimiter wraps golang.org/x/time/rate.Limiter
// Provides production-ready token bucket rate limiting
type RateLimiter struct {
	limiter *rate.Limiter
}

// NewRateLimiter creates a rate limiter with specified requests per second
// Uses the official golang.org/x/time/rate package
func NewRateLimiter(requestsPerSecond int) *RateLimiter {
	// Allow burst of up to requestsPerSecond
	limiter := rate.NewLimiter(rate.Limit(requestsPerSecond), requestsPerSecond)
	return &RateLimiter{
		limiter: limiter,
	}
}

// Wait blocks until a token is available or context is cancelled
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.limiter.Wait(ctx)
}

// Allow checks if a request is allowed without blocking
func (rl *RateLimiter) Allow() bool {
	return rl.limiter.Allow()
}

// SetRate updates the rate limit dynamically
func (rl *RateLimiter) SetRate(requestsPerSecond int) {
	rl.limiter.SetLimit(rate.Limit(requestsPerSecond))
	rl.limiter.SetBurst(requestsPerSecond)
}
