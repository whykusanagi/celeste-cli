package skills

import (
	"fmt"
	"testing"
)

// MockConfigLoader provides a mock implementation of configuration loading for testing
type MockConfigLoader struct {
	TarotCfg          TarotConfig
	VeniceCfg         VeniceConfig
	WeatherCfg        WeatherConfig
	TwitchCfg         TwitchConfig
	YouTubeCfg        YouTubeConfig
	IPFSCfg           IPFSConfig
	AlchemyCfg        AlchemyConfig
	BlockmonCfg       BlockmonConfig
	WalletSecurityCfg WalletSecuritySettingsConfig

	// Error flags to simulate missing config
	TarotError          error
	VeniceError         error
	WeatherError        error
	TwitchError         error
	YouTubeError        error
	IPFSError           error
	AlchemyError        error
	BlockmonError       error
	WalletSecurityError error
}

// GetTarotConfig returns mock tarot configuration
func (m *MockConfigLoader) GetTarotConfig() (TarotConfig, error) {
	if m.TarotError != nil {
		return TarotConfig{}, m.TarotError
	}
	return m.TarotCfg, nil
}

// GetVeniceConfig returns mock Venice.ai configuration
func (m *MockConfigLoader) GetVeniceConfig() (VeniceConfig, error) {
	if m.VeniceError != nil {
		return VeniceConfig{}, m.VeniceError
	}
	return m.VeniceCfg, nil
}

// GetWeatherConfig returns mock weather configuration
func (m *MockConfigLoader) GetWeatherConfig() (WeatherConfig, error) {
	if m.WeatherError != nil {
		return WeatherConfig{}, m.WeatherError
	}
	return m.WeatherCfg, nil
}

// GetTwitchConfig returns mock Twitch configuration
func (m *MockConfigLoader) GetTwitchConfig() (TwitchConfig, error) {
	if m.TwitchError != nil {
		return TwitchConfig{}, m.TwitchError
	}
	return m.TwitchCfg, nil
}

// GetYouTubeConfig returns mock YouTube configuration
func (m *MockConfigLoader) GetYouTubeConfig() (YouTubeConfig, error) {
	if m.YouTubeError != nil {
		return YouTubeConfig{}, m.YouTubeError
	}
	return m.YouTubeCfg, nil
}

// GetIPFSConfig returns mock IPFS configuration
func (m *MockConfigLoader) GetIPFSConfig() (IPFSConfig, error) {
	if m.IPFSError != nil {
		return IPFSConfig{}, m.IPFSError
	}
	return m.IPFSCfg, nil
}

// GetAlchemyConfig returns mock Alchemy configuration
func (m *MockConfigLoader) GetAlchemyConfig() (AlchemyConfig, error) {
	if m.AlchemyError != nil {
		return AlchemyConfig{}, m.AlchemyError
	}
	return m.AlchemyCfg, nil
}

// GetBlockmonConfig returns mock blockchain monitoring configuration
func (m *MockConfigLoader) GetBlockmonConfig() (BlockmonConfig, error) {
	if m.BlockmonError != nil {
		return BlockmonConfig{}, m.BlockmonError
	}
	return m.BlockmonCfg, nil
}

// GetWalletSecurityConfig returns mock wallet security configuration
func (m *MockConfigLoader) GetWalletSecurityConfig() (WalletSecuritySettingsConfig, error) {
	if m.WalletSecurityError != nil {
		return WalletSecuritySettingsConfig{}, m.WalletSecurityError
	}
	return m.WalletSecurityCfg, nil
}

// NewMockConfigLoader creates a mock config loader with default values
func NewMockConfigLoader() *MockConfigLoader {
	return &MockConfigLoader{
		TarotCfg: TarotConfig{
			FunctionURL: "http://mock-api:8080/tarot",
			AuthToken:   "mock-token",
		},
		VeniceCfg: VeniceConfig{
			APIKey:   "mock-venice-key",
			BaseURL:  "http://mock-api:8080/venice",
			Model:    "fluently-xl",
			Upscaler: "realesrgan",
		},
		WeatherCfg: WeatherConfig{
			DefaultZipCode: "10001",
		},
		TwitchCfg: TwitchConfig{
			ClientID:        "mock-twitch-client-id",
			DefaultStreamer: "test_streamer",
		},
		YouTubeCfg: YouTubeConfig{
			APIKey: "mock-youtube-key",
		},
		IPFSCfg: IPFSConfig{
			Provider:       "infura",
			APIKey:         "mock-ipfs-key",
			APISecret:      "mock-ipfs-secret",
			ProjectID:      "mock-project-id",
			GatewayURL:     "https://ipfs.infura.io",
			TimeoutSeconds: 30,
		},
		AlchemyCfg: AlchemyConfig{
			APIKey:         "mock-alchemy-key",
			DefaultNetwork: "eth-mainnet",
			TimeoutSeconds: 10,
		},
		BlockmonCfg: BlockmonConfig{
			AlchemyAPIKey:       "mock-alchemy-key",
			DefaultNetwork:      "eth-mainnet",
			PollIntervalSeconds: 15,
		},
		WalletSecurityCfg: WalletSecuritySettingsConfig{
			Enabled:      true,
			PollInterval: 300,
			AlertLevel:   "medium",
		},
	}
}

// NewMockConfigLoaderWithErrors creates a mock that returns errors for all configs
func NewMockConfigLoaderWithErrors() *MockConfigLoader {
	return &MockConfigLoader{
		TarotError:          fmt.Errorf("tarot config not found"),
		VeniceError:         fmt.Errorf("venice config not found"),
		WeatherError:        fmt.Errorf("weather config not found"),
		TwitchError:         fmt.Errorf("twitch config not found"),
		YouTubeError:        fmt.Errorf("youtube config not found"),
		IPFSError:           fmt.Errorf("IPFS config not found"),
		AlchemyError:        fmt.Errorf("Alchemy config not found"),
		BlockmonError:       fmt.Errorf("blockchain monitoring config not found"),
		WalletSecurityError: fmt.Errorf("wallet security config not found"),
	}
}

// AssertNoError is a test helper to check for errors
func AssertNoError(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		t.Errorf("%s: unexpected error: %v", msg, err)
	}
}

// AssertError is a test helper to check that an error occurred
func AssertError(t *testing.T, err error, msg string) {
	t.Helper()
	if err == nil {
		t.Errorf("%s: expected error but got nil", msg)
	}
}

// AssertContains checks if a string contains a substring
func AssertContains(t *testing.T, str, substr, msg string) {
	t.Helper()
	if !contains(str, substr) {
		t.Errorf("%s: expected string to contain %q, got %q", msg, substr, str)
	}
}

// AssertEqual checks if two values are equal
func AssertEqual(t *testing.T, expected, actual interface{}, msg string) {
	t.Helper()
	if expected != actual {
		t.Errorf("%s: expected %v, got %v", msg, expected, actual)
	}
}

// contains is a simple substring check helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		len(s) > 0 && (s[:len(substr)] == substr || contains(s[1:], substr)))
}
