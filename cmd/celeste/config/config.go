// Package config provides configuration management for Celeste CLI.
package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/whykusanagi/celeste-cli/cmd/celeste/providers"
)

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

const (
	RuntimeModeClassic           = "classic" // deprecated — tools always auto-loop now
	RuntimeModeClaw              = "claw"    // deprecated — tools always auto-loop now
	DefaultClawMaxToolIterations = 25        // safety cap for tool loop turns
)

// Config holds all configuration for Celeste CLI.
type Config struct {
	// API settings
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
	Model   string `json:"model"`
	// AgentModel is the model used for agent / orchestrate / subagent work
	// (free tool-selection). Chat/TTS use Model. Empty falls back to Model.
	// Lets you pin a reasoning/tool-capable model for agent work while keeping a
	// cheap non-reasoning model for chat (model-router guardrail, task e8775b91).
	AgentModel   string `json:"agent_model,omitempty"`
	Timeout      int    `json:"timeout"`                 // seconds
	ContextLimit int    `json:"context_limit,omitempty"` // Optional: Override context window size

	// Google Cloud authentication (for Gemini/Vertex AI)
	GoogleCredentialsFile string `json:"google_credentials_file,omitempty"` // Path to service account JSON file
	GoogleUseADC          bool   `json:"google_use_adc,omitempty"`          // Use Application Default Credentials

	// Runtime-detected provider (not persisted to config file)
	Provider string `json:"-"` // Detected from BaseURL at runtime

	// Default marks this named config as the one loaded when no -config flag
	// is given. Exactly one file should set it; the first match wins.
	Default bool `json:"default,omitempty"`

	// Persona settings
	SkipPersonaPrompt bool `json:"skip_persona_prompt"`

	// Confirm mode: when true, Celeste proposes actions before executing
	// write/generate operations. When false, she auto-executes.
	ConfirmActions bool `json:"confirm_actions,omitempty"`

	// ElevenLabs TTS settings
	ElevenLabsAPIKey  string `json:"elevenlabs_api_key,omitempty"`
	ElevenLabsVoiceID string `json:"elevenlabs_voice_id,omitempty"`

	// Streaming settings
	SimulateTyping bool `json:"simulate_typing"`
	TypingSpeed    int  `json:"typing_speed"` // chars per second

	// Runtime mode settings
	RuntimeMode           string `json:"runtime_mode,omitempty"`             // "classic" or "claw"
	ClawMaxToolIterations int    `json:"claw_max_tool_iterations,omitempty"` // Safety cap for repeated tool loops in claw mode

	// Venice.ai settings (for NSFW mode)
	VeniceAPIKey     string `json:"venice_api_key,omitempty"`
	VeniceBaseURL    string `json:"venice_base_url,omitempty"`
	VeniceModel      string `json:"venice_model,omitempty"`       // Chat model (venice-uncensored)
	VeniceImageModel string `json:"venice_image_model,omitempty"` // Image model (lustify-sdxl)

	// Tarot settings
	TarotFunctionURL string `json:"tarot_function_url,omitempty"`
	TarotAuthToken   string `json:"tarot_auth_token,omitempty"`

	// Twitter settings
	TwitterBearerToken       string `json:"twitter_bearer_token,omitempty"`
	TwitterAPIKey            string `json:"twitter_api_key,omitempty"`
	TwitterAPISecret         string `json:"twitter_api_secret,omitempty"`
	TwitterAccessToken       string `json:"twitter_access_token,omitempty"`
	TwitterAccessTokenSecret string `json:"twitter_access_token_secret,omitempty"`

	// Weather settings
	WeatherDefaultZipCode string `json:"weather_default_zip_code,omitempty"`

	// Twitch settings
	TwitchClientID        string `json:"twitch_client_id,omitempty"`
	TwitchClientSecret    string `json:"twitch_client_secret,omitempty"`
	TwitchDefaultStreamer string `json:"twitch_default_streamer,omitempty"`

	// YouTube settings
	YouTubeAPIKey         string `json:"youtube_api_key,omitempty"`
	YouTubeDefaultChannel string `json:"youtube_default_channel,omitempty"`

	// IPFS settings
	IPFSProvider       string `json:"ipfs_provider,omitempty"` // "infura", "pinata", "custom"
	IPFSAPIKey         string `json:"ipfs_api_key,omitempty"`
	IPFSAPISecret      string `json:"ipfs_api_secret,omitempty"`
	IPFSProjectID      string `json:"ipfs_project_id,omitempty"` // Infura specific
	IPFSGatewayURL     string `json:"ipfs_gateway_url,omitempty"`
	IPFSTimeoutSeconds int    `json:"ipfs_timeout_seconds,omitempty"`

	// Alchemy settings
	AlchemyAPIKey         string `json:"alchemy_api_key,omitempty"`
	AlchemyDefaultNetwork string `json:"alchemy_default_network,omitempty"`
	AlchemyTimeoutSeconds int    `json:"alchemy_timeout_seconds,omitempty"`

	// Blockchain monitoring settings
	BlockmonAlchemyAPIKey       string `json:"blockmon_alchemy_api_key,omitempty"`
	BlockmonWebhookURL          string `json:"blockmon_webhook_url,omitempty"`
	BlockmonDefaultNetwork      string `json:"blockmon_default_network,omitempty"`
	BlockmonPollIntervalSeconds int    `json:"blockmon_poll_interval_seconds,omitempty"`

	// Wallet security settings
	WalletSecurityEnabled      bool   `json:"wallet_security_enabled,omitempty"`
	WalletSecurityPollInterval int    `json:"wallet_security_poll_interval,omitempty"` // seconds
	WalletSecurityAlertLevel   string `json:"wallet_security_alert_level,omitempty"`   // "low", "medium", "high", "critical"

	// Collections configuration (xAI only)
	XAIManagementAPIKey string             `json:"xai_management_api_key,omitempty"`
	Collections         *CollectionsConfig `json:"collections,omitempty"`
	XAIFeatures         *XAIFeaturesConfig `json:"xai_features,omitempty"`

	// Orchestrator settings
	Orchestrator *OrchestratorConfig `json:"orchestrator,omitempty"`
}

// CollectionsConfig holds collections settings
type CollectionsConfig struct {
	Enabled           bool     `json:"enabled"`
	ActiveCollections []string `json:"active_collections"`
	AutoEnable        bool     `json:"auto_enable"`
}

// XAIFeaturesConfig holds xAI-specific feature flags
type XAIFeaturesConfig struct {
	EnableWebSearch bool `json:"enable_web_search"`
	EnableXSearch   bool `json:"enable_x_search"`
}

// LaneConfig holds the primary and optional reviewer model for one task lane.
// PrimaryBaseURL/PrimaryAPIKey and ReviewerBaseURL/ReviewerAPIKey allow cross-provider
// orchestration (e.g. xAI primary + OpenAI reviewer) without changing the main config.
type LaneConfig struct {
	Primary         string `json:"primary"`
	PrimaryBaseURL  string `json:"primary_base_url,omitempty"`
	PrimaryAPIKey   string `json:"primary_api_key,omitempty"`
	Reviewer        string `json:"reviewer,omitempty"`
	ReviewerBaseURL string `json:"reviewer_base_url,omitempty"`
	ReviewerAPIKey  string `json:"reviewer_api_key,omitempty"`
}

// OrchestratorConfig controls multi-model orchestration behaviour.
type OrchestratorConfig struct {
	Lanes        map[string]LaneConfig `json:"lanes,omitempty"`
	DefaultLane  string                `json:"default_lane,omitempty"`
	DebateRounds int                   `json:"debate_rounds,omitempty"`
}

// DefaultProvider seeds a brand-new install that has no config file yet. It only
// names a provider in the registry — the BaseURL and model come from there, so
// retiring/renaming a model is a one-line registry edit, never a hunt across
// config.go, main.go templates, and the registry that diverge over time.
const DefaultProvider = "sakana"

// DefaultConfig returns a config with default values. Provider-specific bits
// (BaseURL, model) resolve from the provider registry rather than being pinned
// here; only behavioural defaults live in this struct.
func DefaultConfig() *Config {
	seed, _ := providers.GetProvider(DefaultProvider)
	venice, _ := providers.GetProvider("venice")
	return &Config{
		BaseURL:               seed.BaseURL,
		Model:                 seed.DefaultModel,
		Timeout:               60,
		SkipPersonaPrompt:     false,
		SimulateTyping:        true,
		TypingSpeed:           40,
		RuntimeMode:           RuntimeModeClassic,
		ClawMaxToolIterations: DefaultClawMaxToolIterations,
		VeniceBaseURL:         venice.BaseURL,
		VeniceModel:           venice.DefaultModel,
	}
}

// DefaultModelForBaseURL resolves the default model for a config from its own
// provider (detected via the registry), falling back to the seed provider's
// model when the URL is unknown. This is the dynamic resolution that replaces a
// hard-coded model string.
func DefaultModelForBaseURL(baseURL string) string {
	if caps, ok := providers.GetProvider(providers.DetectProvider(baseURL)); ok && caps.DefaultModel != "" {
		return caps.DefaultModel
	}
	seed, _ := providers.GetProvider(DefaultProvider)
	return seed.DefaultModel
}

func IsValidRuntimeMode(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case RuntimeModeClassic, RuntimeModeClaw:
		return true
	default:
		return false
	}
}

func NormalizeRuntimeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case RuntimeModeClaw:
		return RuntimeModeClaw
	default:
		return RuntimeModeClassic
	}
}

// Paths returns the configuration directory and file paths.
func Paths() (configDir, configFile, secretsFile, skillsFile string) {
	homeDir, _ := os.UserHomeDir()
	configDir = filepath.Join(homeDir, ".celeste")
	configFile = filepath.Join(configDir, "config.json")
	secretsFile = filepath.Join(configDir, "secrets.json")
	skillsFile = filepath.Join(configDir, "skills.json")
	return
}

// NamedConfigPath returns the path for a named config file.
// If name is empty, returns the default config path.
func NamedConfigPath(name string) string {
	homeDir, _ := os.UserHomeDir()
	configDir := filepath.Join(homeDir, ".celeste")
	if name == "" {
		return filepath.Join(configDir, "config.json")
	}
	return filepath.Join(configDir, fmt.Sprintf("config.%s.json", name))
}

// LoadSkillsConfig loads skill-specific configuration from skills.json.
func LoadSkillsConfig() (*Config, error) {
	_, _, _, skillsFile := Paths()

	skillsConfig := &Config{}

	// Load skills.json if it exists
	if data, err := os.ReadFile(skillsFile); err == nil {
		if err := json.Unmarshal(data, skillsConfig); err != nil {
			return nil, fmt.Errorf("failed to parse skills config: %w", err)
		}
	}

	return skillsConfig, nil
}

// SaveSkillsConfig saves skill-specific configuration to skills.json.
func SaveSkillsConfig(skillsConfig *Config) error {
	_, _, _, skillsFile := Paths()

	// Create skills config with only skill-related fields
	skillsOnly := &Config{
		VeniceAPIKey:                skillsConfig.VeniceAPIKey,
		VeniceBaseURL:               skillsConfig.VeniceBaseURL,
		VeniceModel:                 skillsConfig.VeniceModel,
		TarotFunctionURL:            skillsConfig.TarotFunctionURL,
		TarotAuthToken:              skillsConfig.TarotAuthToken,
		TwitterBearerToken:          skillsConfig.TwitterBearerToken,
		TwitterAPIKey:               skillsConfig.TwitterAPIKey,
		TwitterAPISecret:            skillsConfig.TwitterAPISecret,
		TwitterAccessToken:          skillsConfig.TwitterAccessToken,
		TwitterAccessTokenSecret:    skillsConfig.TwitterAccessTokenSecret,
		WeatherDefaultZipCode:       skillsConfig.WeatherDefaultZipCode,
		TwitchClientID:              skillsConfig.TwitchClientID,
		TwitchDefaultStreamer:       skillsConfig.TwitchDefaultStreamer,
		YouTubeAPIKey:               skillsConfig.YouTubeAPIKey,
		YouTubeDefaultChannel:       skillsConfig.YouTubeDefaultChannel,
		IPFSProvider:                skillsConfig.IPFSProvider,
		IPFSAPIKey:                  skillsConfig.IPFSAPIKey,
		IPFSAPISecret:               skillsConfig.IPFSAPISecret,
		IPFSProjectID:               skillsConfig.IPFSProjectID,
		IPFSGatewayURL:              skillsConfig.IPFSGatewayURL,
		IPFSTimeoutSeconds:          skillsConfig.IPFSTimeoutSeconds,
		AlchemyAPIKey:               skillsConfig.AlchemyAPIKey,
		AlchemyDefaultNetwork:       skillsConfig.AlchemyDefaultNetwork,
		AlchemyTimeoutSeconds:       skillsConfig.AlchemyTimeoutSeconds,
		BlockmonAlchemyAPIKey:       skillsConfig.BlockmonAlchemyAPIKey,
		BlockmonWebhookURL:          skillsConfig.BlockmonWebhookURL,
		BlockmonDefaultNetwork:      skillsConfig.BlockmonDefaultNetwork,
		BlockmonPollIntervalSeconds: skillsConfig.BlockmonPollIntervalSeconds,
	}

	data, err := json.MarshalIndent(skillsOnly, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal skills config: %w", err)
	}

	return os.WriteFile(skillsFile, data, 0600) // Restrictive permissions for secrets
}

// LoadNamed loads configuration from a named config file.
// If name is empty, loads the default config.
func LoadNamed(name string) (*Config, error) {
	if name == "" {
		// No explicit profile: honor a config.<name>.json flagged "default": true.
		// Falls back to the legacy config.json when none is flagged.
		if d := ResolveDefaultName(); d != "" {
			name = d
		} else {
			return Load()
		}
	}

	config := DefaultConfig()
	configPath := NamedConfigPath(name)

	// Load named config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("config '%s' not found at %s: %w", name, configPath, err)
	}

	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config '%s': %w", name, err)
	}

	// Load shared json (for all skill configurations)
	if skillsConfig, err := LoadSkillsConfig(); err == nil {
		// Merge skill configs (json takes precedence if set)
		if skillsConfig.VeniceAPIKey != "" {
			config.VeniceAPIKey = skillsConfig.VeniceAPIKey
		}
		if skillsConfig.VeniceBaseURL != "" {
			config.VeniceBaseURL = skillsConfig.VeniceBaseURL
		}
		if skillsConfig.VeniceModel != "" {
			config.VeniceModel = skillsConfig.VeniceModel
		}
		if skillsConfig.TarotFunctionURL != "" {
			config.TarotFunctionURL = skillsConfig.TarotFunctionURL
		}
		if skillsConfig.TarotAuthToken != "" {
			config.TarotAuthToken = skillsConfig.TarotAuthToken
		}
		if skillsConfig.TwitterBearerToken != "" {
			config.TwitterBearerToken = skillsConfig.TwitterBearerToken
		}
		if skillsConfig.TwitterAPIKey != "" {
			config.TwitterAPIKey = skillsConfig.TwitterAPIKey
		}
		if skillsConfig.TwitterAPISecret != "" {
			config.TwitterAPISecret = skillsConfig.TwitterAPISecret
		}
		if skillsConfig.TwitterAccessToken != "" {
			config.TwitterAccessToken = skillsConfig.TwitterAccessToken
		}
		if skillsConfig.TwitterAccessTokenSecret != "" {
			config.TwitterAccessTokenSecret = skillsConfig.TwitterAccessTokenSecret
		}
		if skillsConfig.WeatherDefaultZipCode != "" {
			config.WeatherDefaultZipCode = skillsConfig.WeatherDefaultZipCode
		}
		if skillsConfig.TwitchClientID != "" {
			config.TwitchClientID = skillsConfig.TwitchClientID
		}
		if skillsConfig.TwitchClientSecret != "" {
			config.TwitchClientSecret = skillsConfig.TwitchClientSecret
		}
		if skillsConfig.TwitchDefaultStreamer != "" {
			config.TwitchDefaultStreamer = skillsConfig.TwitchDefaultStreamer
		}
		if skillsConfig.YouTubeAPIKey != "" {
			config.YouTubeAPIKey = skillsConfig.YouTubeAPIKey
		}
		if skillsConfig.YouTubeDefaultChannel != "" {
			config.YouTubeDefaultChannel = skillsConfig.YouTubeDefaultChannel
		}
		if skillsConfig.IPFSProvider != "" {
			config.IPFSProvider = skillsConfig.IPFSProvider
		}
		if skillsConfig.IPFSAPIKey != "" {
			config.IPFSAPIKey = skillsConfig.IPFSAPIKey
		}
		if skillsConfig.IPFSAPISecret != "" {
			config.IPFSAPISecret = skillsConfig.IPFSAPISecret
		}
		if skillsConfig.IPFSProjectID != "" {
			config.IPFSProjectID = skillsConfig.IPFSProjectID
		}
		if skillsConfig.IPFSGatewayURL != "" {
			config.IPFSGatewayURL = skillsConfig.IPFSGatewayURL
		}
		if skillsConfig.IPFSTimeoutSeconds > 0 {
			config.IPFSTimeoutSeconds = skillsConfig.IPFSTimeoutSeconds
		}
		if skillsConfig.AlchemyAPIKey != "" {
			config.AlchemyAPIKey = skillsConfig.AlchemyAPIKey
		}
		if skillsConfig.AlchemyDefaultNetwork != "" {
			config.AlchemyDefaultNetwork = skillsConfig.AlchemyDefaultNetwork
		}
		if skillsConfig.AlchemyTimeoutSeconds > 0 {
			config.AlchemyTimeoutSeconds = skillsConfig.AlchemyTimeoutSeconds
		}
		if skillsConfig.BlockmonAlchemyAPIKey != "" {
			config.BlockmonAlchemyAPIKey = skillsConfig.BlockmonAlchemyAPIKey
		}
		if skillsConfig.BlockmonWebhookURL != "" {
			config.BlockmonWebhookURL = skillsConfig.BlockmonWebhookURL
		}
		if skillsConfig.BlockmonDefaultNetwork != "" {
			config.BlockmonDefaultNetwork = skillsConfig.BlockmonDefaultNetwork
		}
		if skillsConfig.BlockmonPollIntervalSeconds > 0 {
			config.BlockmonPollIntervalSeconds = skillsConfig.BlockmonPollIntervalSeconds
		}
	}

	return config, nil
}

// ResolveDefaultName returns the name of the config.<name>.json file flagged
// "default": true, or "" if none is flagged (or the dir is unreadable). Files
// are scanned in directory order; the first match wins.
func ResolveDefaultName() string {
	configDir, _, _, _ := Paths()
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		name := entry.Name()
		if len(name) <= 12 || name[:7] != "config." || name[len(name)-5:] != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(configDir, name))
		if err != nil {
			continue
		}
		var probe struct {
			Default bool `json:"default"`
		}
		if json.Unmarshal(data, &probe) == nil && probe.Default {
			return name[7 : len(name)-5]
		}
	}
	return ""
}

// SetDefaultProfile marks config.<name>.json as the default profile and clears
// the flag on every other named profile, so exactly one stays flagged.
func SetDefaultProfile(name string) error {
	if name == "" {
		return fmt.Errorf("default profile must be a named config, not the bare default")
	}
	target := NamedConfigPath(name)
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("config '%s' not found at %s: %w", name, target, err)
	}

	configDir, _, _, _ := Paths()
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		fname := entry.Name()
		if len(fname) <= 12 || fname[:7] != "config." || fname[len(fname)-5:] != ".json" {
			continue
		}
		profile := fname[7 : len(fname)-5]
		cfg, err := LoadNamed(profile)
		if err != nil {
			return fmt.Errorf("failed to load profile '%s': %w", profile, err)
		}
		want := profile == name
		if cfg.Default == want {
			continue // already correct, skip the write
		}
		cfg.Default = want
		if err := SaveNamed(profile, cfg); err != nil {
			return fmt.Errorf("failed to update profile '%s': %w", profile, err)
		}
	}
	return nil
}

// ListConfigs returns all available config names.
func ListConfigs() ([]string, error) {
	configDir, _, _, _ := Paths()

	entries, err := os.ReadDir(configDir)
	if err != nil {
		return nil, err
	}

	var configs []string
	for _, entry := range entries {
		name := entry.Name()
		if name == "config.json" {
			configs = append(configs, "default")
		} else if len(name) > 12 && name[:7] == "config." && name[len(name)-5:] == ".json" {
			// Extract name from config.<name>.json
			configName := name[7 : len(name)-5]
			configs = append(configs, configName)
		}
	}

	return configs, nil
}

// Load loads configuration from file and environment.
func Load() (*Config, error) {
	config := DefaultConfig()
	configDir, configFile, secretsFile, _ := Paths()

	// Ensure config directory exists
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Load main config file
	if data, err := os.ReadFile(configFile); err == nil {
		if err := json.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config: %w", err)
		}
	}

	// Load secrets file (for API keys - backward compatibility)
	if data, err := os.ReadFile(secretsFile); err == nil {
		var secrets Config
		if err := json.Unmarshal(data, &secrets); err == nil {
			if secrets.APIKey != "" {
				config.APIKey = secrets.APIKey
			}
		}
	}

	// Load json (shared across all configs)
	if skillsConfig, err := LoadSkillsConfig(); err == nil {
		// Merge skill configs
		if skillsConfig.VeniceAPIKey != "" {
			config.VeniceAPIKey = skillsConfig.VeniceAPIKey
		}
		if skillsConfig.VeniceBaseURL != "" {
			config.VeniceBaseURL = skillsConfig.VeniceBaseURL
		}
		if skillsConfig.VeniceModel != "" {
			config.VeniceModel = skillsConfig.VeniceModel
		}
		if skillsConfig.TarotFunctionURL != "" {
			config.TarotFunctionURL = skillsConfig.TarotFunctionURL
		}
		if skillsConfig.TarotAuthToken != "" {
			config.TarotAuthToken = skillsConfig.TarotAuthToken
		}
		if skillsConfig.TwitterBearerToken != "" {
			config.TwitterBearerToken = skillsConfig.TwitterBearerToken
		}
		if skillsConfig.TwitterAPIKey != "" {
			config.TwitterAPIKey = skillsConfig.TwitterAPIKey
		}
		if skillsConfig.TwitterAPISecret != "" {
			config.TwitterAPISecret = skillsConfig.TwitterAPISecret
		}
		if skillsConfig.TwitterAccessToken != "" {
			config.TwitterAccessToken = skillsConfig.TwitterAccessToken
		}
		if skillsConfig.TwitterAccessTokenSecret != "" {
			config.TwitterAccessTokenSecret = skillsConfig.TwitterAccessTokenSecret
		}
		if skillsConfig.WeatherDefaultZipCode != "" {
			config.WeatherDefaultZipCode = skillsConfig.WeatherDefaultZipCode
		}
		if skillsConfig.TwitchClientID != "" {
			config.TwitchClientID = skillsConfig.TwitchClientID
		}
		if skillsConfig.TwitchClientSecret != "" {
			config.TwitchClientSecret = skillsConfig.TwitchClientSecret
		}
		if skillsConfig.TwitchDefaultStreamer != "" {
			config.TwitchDefaultStreamer = skillsConfig.TwitchDefaultStreamer
		}
		if skillsConfig.YouTubeAPIKey != "" {
			config.YouTubeAPIKey = skillsConfig.YouTubeAPIKey
		}
		if skillsConfig.YouTubeDefaultChannel != "" {
			config.YouTubeDefaultChannel = skillsConfig.YouTubeDefaultChannel
		}
		if skillsConfig.IPFSProvider != "" {
			config.IPFSProvider = skillsConfig.IPFSProvider
		}
		if skillsConfig.IPFSAPIKey != "" {
			config.IPFSAPIKey = skillsConfig.IPFSAPIKey
		}
		if skillsConfig.IPFSAPISecret != "" {
			config.IPFSAPISecret = skillsConfig.IPFSAPISecret
		}
		if skillsConfig.IPFSProjectID != "" {
			config.IPFSProjectID = skillsConfig.IPFSProjectID
		}
		if skillsConfig.IPFSGatewayURL != "" {
			config.IPFSGatewayURL = skillsConfig.IPFSGatewayURL
		}
		if skillsConfig.IPFSTimeoutSeconds > 0 {
			config.IPFSTimeoutSeconds = skillsConfig.IPFSTimeoutSeconds
		}
		if skillsConfig.AlchemyAPIKey != "" {
			config.AlchemyAPIKey = skillsConfig.AlchemyAPIKey
		}
		if skillsConfig.AlchemyDefaultNetwork != "" {
			config.AlchemyDefaultNetwork = skillsConfig.AlchemyDefaultNetwork
		}
		if skillsConfig.AlchemyTimeoutSeconds > 0 {
			config.AlchemyTimeoutSeconds = skillsConfig.AlchemyTimeoutSeconds
		}
		if skillsConfig.BlockmonAlchemyAPIKey != "" {
			config.BlockmonAlchemyAPIKey = skillsConfig.BlockmonAlchemyAPIKey
		}
		if skillsConfig.BlockmonWebhookURL != "" {
			config.BlockmonWebhookURL = skillsConfig.BlockmonWebhookURL
		}
		if skillsConfig.BlockmonDefaultNetwork != "" {
			config.BlockmonDefaultNetwork = skillsConfig.BlockmonDefaultNetwork
		}
		if skillsConfig.BlockmonPollIntervalSeconds > 0 {
			config.BlockmonPollIntervalSeconds = skillsConfig.BlockmonPollIntervalSeconds
		}
	}

	// Reconcile the model so the saved config reflects what's actually used (#51):
	// an empty model falls back to the default, and models xAI no longer supports
	// are migrated to their replacement. Persisted so the header, config file, and
	// the model sent to the API all agree instead of silently diverging.
	dirty := false
	if changed, from, to := reconcileModel(config); changed {
		if from == "" {
			log.Printf("[config] no model set — using default %q (saved)", to)
		} else {
			log.Printf("[config] model %q is no longer supported — migrated to %q (saved)", from, to)
		}
		dirty = true
	}
	// Clamp a stale context_limit override that exceeds the (possibly migrated)
	// model's real window — e.g. a 2M limit carried over onto a 256K model. A
	// limit larger than the model supports is always invalid, so reset to the
	// model default (#51).
	if config.ContextLimit > 0 {
		if maxLimit := GetModelLimit(config.Model); config.ContextLimit > maxLimit {
			log.Printf("[config] context_limit %d exceeds %q's %d-token window — using model default (saved)", config.ContextLimit, config.Model, maxLimit)
			config.ContextLimit = 0
			dirty = true
		}
	}
	if dirty {
		_ = Save(config)
	}

	return config, nil
}

// deprecatedModels maps Grok models xAI no longer serves to their supported
// replacement. Loading a config on one of these silently fell back to a
// cost-prohibitive variant server-side; migrate it instead (#51).
// deprecatedModels maps Grok models that xAI silently ROUTES to the
// cost-prohibitive grok-4.3 (the grok-4-1-* family) to a safe replacement.
// Using any of these burns grok-4.3 pricing + reasoning tokens without the user
// knowing — migrate them to the non-reasoning default instead (#51).
var deprecatedModels = map[string]string{
	"grok-4-1-fast":               "grok-4.20-0309-non-reasoning",
	"grok-4-1-fast-reasoning":     "grok-4.20-0309-non-reasoning",
	"grok-4-1-fast-non-reasoning": "grok-4.20-0309-non-reasoning",
	"grok-4-1-reasoning":          "grok-4.20-0309-non-reasoning",
	"grok-4-1":                    "grok-4.20-0309-non-reasoning",
}

// reconcileModel fills an empty model with the default and migrates a known-
// deprecated model to its replacement. Returns whether config.Model changed,
// the previous value (empty if it was unset), and the new value.
func reconcileModel(config *Config) (changed bool, from, to string) {
	// Migrate AgentModel too (if set) so the same grok-4-1-* trap protection
	// applies to the agent-router model. Reported via the chat-model return only;
	// the agent-model migration is silent (best-effort).
	if config.AgentModel != "" {
		if repl, ok := deprecatedModels[config.AgentModel]; ok && repl != config.AgentModel {
			config.AgentModel = repl
		}
	}
	if config.Model == "" {
		// Resolve from the config's OWN provider, not a global default — a Venice
		// config with no model should get Venice's default, not the seed's.
		config.Model = DefaultModelForBaseURL(config.BaseURL)
		return true, "", config.Model
	}
	if repl, ok := deprecatedModels[config.Model]; ok && repl != config.Model {
		from = config.Model
		config.Model = repl
		return true, from, repl
	}
	return false, "", ""
}

// ResolveAgentModel returns the model to use for agent / orchestrate / subagent
// work: AgentModel if set, otherwise the chat Model. This is the router seam —
// callers entering agent mode use this instead of Model.
func (c *Config) ResolveAgentModel() string {
	if c.AgentModel != "" {
		return c.AgentModel
	}
	return c.Model
}

// Save saves configuration to file.
func Save(config *Config) error {
	_, configFile, _, _ := Paths()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(configFile, data, 0644)
}

// SaveNamed writes the config to a named profile file (config.<name>.json).
// Named profiles store everything inline, including the API key — that is how
// LoadNamed reads them — so unlike Save there is no separate secrets file. An
// empty name falls back to the default Save path.
func SaveNamed(name string, config *Config) error {
	if name == "" {
		return Save(config)
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Ensure ~/.celeste exists (first-run without --init wouldn't have created it).
	path := NamedConfigPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// 0600: a named profile carries the API key inline.
	return os.WriteFile(path, data, 0600)
}

// SaveSecrets saves API key to secrets file (backward compatibility).
func SaveSecrets(config *Config) error {
	_, _, secretsFile, _ := Paths()

	secrets := &Config{
		APIKey: config.APIKey, // Only API key in secrets.json now
	}

	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal secrets: %w", err)
	}

	return os.WriteFile(secretsFile, data, 0600) // More restrictive permissions for secrets
}

// ConfigLoader provides configuration values to tools.
type ConfigLoader struct {
	config *Config
}

// NewConfigLoader creates a new config loader.
func NewConfigLoader(config *Config) *ConfigLoader {
	return &ConfigLoader{config: config}
}

// GetTarotConfig returns tarot configuration.
func (l *ConfigLoader) GetTarotConfig() (TarotConfig, error) {
	if l.config.TarotAuthToken == "" {
		return TarotConfig{}, fmt.Errorf("tarot auth token not configured")
	}

	url := l.config.TarotFunctionURL
	if url == "" {
		url = "https://faas-nyc1-2ef2e6cc.doserverless.co/api/v1/namespaces/fn-30b193db-d334-4dab-b5cd-ab49067f88cc/actions/tarot/logic?blocking=true&result=true"
	}

	return TarotConfig{
		FunctionURL: url,
		AuthToken:   l.config.TarotAuthToken,
	}, nil
}

// GetVeniceConfig returns Venice.ai configuration.
func (l *ConfigLoader) GetVeniceConfig() (VeniceConfig, error) {
	if l.config.VeniceAPIKey == "" {
		return VeniceConfig{}, fmt.Errorf("Venice.ai API key not configured")
	}

	baseURL := l.config.VeniceBaseURL
	if baseURL == "" {
		baseURL = "https://api.venice.ai/api/v1"
	}

	model := l.config.VeniceModel
	if model == "" {
		model = "venice-uncensored"
	}

	imageModel := l.config.VeniceImageModel
	if imageModel == "" {
		imageModel = "lustify-sdxl" // Default NSFW image generation model
	}

	return VeniceConfig{
		APIKey:     l.config.VeniceAPIKey,
		BaseURL:    baseURL,
		Model:      model,
		ImageModel: imageModel,
		Upscaler:   "upscaler",
	}, nil
}

// GetWeatherConfig returns weather skill configuration.
func (l *ConfigLoader) GetWeatherConfig() (WeatherConfig, error) {
	return WeatherConfig{
		DefaultZipCode: l.config.WeatherDefaultZipCode,
	}, nil
}

// GetTwitchConfig returns Twitch API configuration.
func (l *ConfigLoader) GetTwitchConfig() (TwitchConfig, error) {
	if l.config.TwitchClientID == "" {
		return TwitchConfig{}, fmt.Errorf("Twitch Client ID not configured")
	}

	defaultStreamer := l.config.TwitchDefaultStreamer
	if defaultStreamer == "" {
		defaultStreamer = "whykusanagi"
	}

	return TwitchConfig{
		ClientID:        l.config.TwitchClientID,
		ClientSecret:    l.config.TwitchClientSecret,
		DefaultStreamer: defaultStreamer,
	}, nil
}

// GetYouTubeConfig returns YouTube API configuration.
func (l *ConfigLoader) GetYouTubeConfig() (YouTubeConfig, error) {
	if l.config.YouTubeAPIKey == "" {
		return YouTubeConfig{}, fmt.Errorf("YouTube API key not configured")
	}

	defaultChannel := l.config.YouTubeDefaultChannel
	if defaultChannel == "" {
		defaultChannel = "whykusanagi"
	}

	return YouTubeConfig{
		APIKey:         l.config.YouTubeAPIKey,
		DefaultChannel: defaultChannel,
	}, nil
}

// GetIPFSConfig returns IPFS configuration.
func (l *ConfigLoader) GetIPFSConfig() (IPFSConfig, error) {
	if l.config.IPFSAPIKey == "" {
		return IPFSConfig{}, fmt.Errorf("IPFS API key not configured")
	}

	provider := l.config.IPFSProvider
	if provider == "" {
		provider = "infura"
	}

	timeout := l.config.IPFSTimeoutSeconds
	if timeout == 0 {
		timeout = 30
	}

	return IPFSConfig{
		Provider:       provider,
		APIKey:         l.config.IPFSAPIKey,
		APISecret:      l.config.IPFSAPISecret,
		ProjectID:      l.config.IPFSProjectID,
		GatewayURL:     l.config.IPFSGatewayURL,
		TimeoutSeconds: timeout,
	}, nil
}

// GetAlchemyConfig returns Alchemy API configuration.
func (l *ConfigLoader) GetAlchemyConfig() (AlchemyConfig, error) {
	if l.config.AlchemyAPIKey == "" {
		return AlchemyConfig{}, fmt.Errorf("Alchemy API key not configured")
	}

	network := l.config.AlchemyDefaultNetwork
	if network == "" {
		network = "eth-mainnet"
	}

	timeout := l.config.AlchemyTimeoutSeconds
	if timeout == 0 {
		timeout = 10
	}

	return AlchemyConfig{
		APIKey:         l.config.AlchemyAPIKey,
		DefaultNetwork: network,
		TimeoutSeconds: timeout,
	}, nil
}

// GetBlockmonConfig returns blockchain monitoring configuration.
func (l *ConfigLoader) GetBlockmonConfig() (BlockmonConfig, error) {
	apiKey := l.config.BlockmonAlchemyAPIKey
	if apiKey == "" {
		// Fall back to main Alchemy API key
		apiKey = l.config.AlchemyAPIKey
	}
	if apiKey == "" {
		return BlockmonConfig{}, fmt.Errorf("Alchemy API key not configured for blockchain monitoring")
	}

	network := l.config.BlockmonDefaultNetwork
	if network == "" {
		network = "eth-mainnet"
	}

	pollInterval := l.config.BlockmonPollIntervalSeconds
	if pollInterval == 0 {
		pollInterval = 15
	}

	return BlockmonConfig{
		AlchemyAPIKey:       apiKey,
		WebhookURL:          l.config.BlockmonWebhookURL,
		DefaultNetwork:      network,
		PollIntervalSeconds: pollInterval,
	}, nil
}

// GetWalletSecurityConfig returns wallet security monitoring configuration.
func (l *ConfigLoader) GetWalletSecurityConfig() (WalletSecuritySettingsConfig, error) {
	pollInterval := l.config.WalletSecurityPollInterval
	if pollInterval == 0 {
		pollInterval = 300 // 5 minutes default
	}

	alertLevel := l.config.WalletSecurityAlertLevel
	if alertLevel == "" {
		alertLevel = "medium"
	}

	return WalletSecuritySettingsConfig{
		Enabled:      l.config.WalletSecurityEnabled,
		PollInterval: pollInterval,
		AlertLevel:   alertLevel,
	}, nil
}

// GetTimeout returns the configured timeout as a duration.
func (c *Config) GetTimeout() time.Duration {
	if c.Timeout <= 0 {
		return 60 * time.Second
	}
	return time.Duration(c.Timeout) * time.Second
}
