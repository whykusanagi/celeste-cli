package builtin

// ConfigLoader provides access to configuration values.
// This interface mirrors skills.ConfigLoader exactly so that
// config.NewConfigLoader(cfg) satisfies both.
type ConfigLoader interface {
	GetTarotConfig() (TarotConfig, error)
	GetVeniceConfig() (VeniceConfig, error)
	GetWeatherConfig() (WeatherConfig, error)
	GetTwitchConfig() (TwitchConfig, error)
	GetYouTubeConfig() (YouTubeConfig, error)
	GetIPFSConfig() (IPFSConfig, error)
	GetAlchemyConfig() (AlchemyConfig, error)
	GetBlockmonConfig() (BlockmonConfig, error)
	GetWalletSecurityConfig() (WalletSecuritySettingsConfig, error)
}

// TarotConfig holds tarot function configuration.
type TarotConfig struct {
	FunctionURL string
	AuthToken   string
}

// VeniceConfig holds Venice.ai configuration.
type VeniceConfig struct {
	APIKey     string
	BaseURL    string
	Model      string // Chat model (venice-uncensored)
	ImageModel string // Image generation model (lustify-sdxl, animewan, hidream, wai-Illustrious)
	Upscaler   string
}

// WeatherConfig holds weather skill configuration.
type WeatherConfig struct {
	DefaultZipCode string
}

// TwitchConfig holds Twitch API configuration.
type TwitchConfig struct {
	ClientID        string
	ClientSecret    string
	DefaultStreamer string
}

// YouTubeConfig holds YouTube API configuration.
type YouTubeConfig struct {
	APIKey         string
	DefaultChannel string
}

// IPFSConfig holds IPFS configuration.
type IPFSConfig struct {
	Provider       string
	APIKey         string
	APISecret      string
	ProjectID      string
	GatewayURL     string
	TimeoutSeconds int
}

// AlchemyConfig holds Alchemy API configuration.
type AlchemyConfig struct {
	APIKey         string
	DefaultNetwork string
	TimeoutSeconds int
}

// BlockmonConfig holds blockchain monitoring configuration.
type BlockmonConfig struct {
	AlchemyAPIKey       string
	WebhookURL          string
	DefaultNetwork      string
	PollIntervalSeconds int
}

// WalletSecuritySettingsConfig holds wallet security settings.
type WalletSecuritySettingsConfig struct {
	Enabled      bool
	PollInterval int    // seconds
	AlertLevel   string // minimum severity to alert on
}
