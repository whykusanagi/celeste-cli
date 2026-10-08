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

	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/atomicfile"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/internal/privfs"
	"github.com/whykusanagi/celeste-cli/v2/cmd/celeste/providers"
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

// DefaultMaxToolIterations is the chat's turn cap (#144 renamed it from
// claw_max_tool_iterations; migrate.go maps the old key).
const DefaultMaxToolIterations = 25

// DefaultTypingSpeed is typing_speed's default, in characters per second
// (3 characters per 50ms animation tick).
const DefaultTypingSpeed = 60

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
	AgentModel string `json:"agent_model,omitempty"`
	// SmallModel is a cheaper model for housekeeping calls such as context
	// compaction summaries (#174). Empty falls back to Model.
	SmallModel string `json:"small_model,omitempty"`
	// PinModel turns live model resolution off: the configured models are
	// sent as written even if the provider no longer lists them. Same as
	// CELESTE_PIN_MODEL=1.
	PinModel bool `json:"pin_model,omitempty"`
	// WebFetchAllowPrivate lets web_fetch reach loopback, private-network,
	// link-local and other non-public addresses, which it refuses by default.
	// For local docs servers. Same as CELESTE_WEB_FETCH_ALLOW_PRIVATE=1.
	WebFetchAllowPrivate bool `json:"web_fetch_allow_private,omitempty"`
	// JevPrune turns on TypeSafe Jev as a judge for context pruning (#175).
	// "shadow" asks Jev in the background and only logs what it would have
	// pruned; "on" (2.0 W3) elides the least-needed results first, asking
	// Jev inside the loop (2.5 s cap, rules on any error); anything else is
	// off. Redacted excerpts of old tool results go to TypeSafe. The key
	// comes from TYPESAFE_API_KEY or ~/.celeste/typesafe.key.
	JevPrune string `json:"jev_prune,omitempty"`
	// JevGate asks Jev whether a tool call is destructive, exfiltrates data
	// or goes beyond what was asked (2.0 W3): "shadow" logs, "on" turns an
	// allowed call into an Ask (never the reverse). Off by default.
	JevGate string `json:"jev_gate,omitempty"`
	// JevRoute picks /orchestrate's lane with a Jev choice question (2.0
	// W3): "shadow" logs, "on" uses it. Off by default.
	JevRoute string `json:"jev_route,omitempty"`
	// Oracle picks the judge for steering questions (2.0 W3):
	// "heuristic" (default), "llm" (the small model) or "jev". The
	// watchdog ballot asks it.
	Oracle string `json:"oracle,omitempty"`
	// StreamRules: "shadow" (default) matches stream rules and logs, "on"
	// acts, "off" skips them (2.0 W3).
	StreamRules string `json:"stream_rules,omitempty"`
	// Watchdog: the ballot every 3 turns (2.0 W3): "off" (default),
	// "shadow" (asked and logged) or "on" (steers).
	Watchdog string `json:"watchdog,omitempty"`
	// CompletionGate (agent runs, 2.0 W3): "shadow" (default) keeps the
	// substring TASK_COMPLETE check and logs where the gate differs; "on"
	// lets the gate decide.
	CompletionGate string `json:"completion_gate,omitempty"`
	Timeout        int    `json:"timeout"`                 // seconds
	ContextLimit   int    `json:"context_limit,omitempty"` // Optional: Override context window size

	// Google Cloud authentication (for Gemini/Vertex AI)
	GoogleCredentialsFile string `json:"google_credentials_file,omitempty"` // Path to service account JSON file
	GoogleUseADC          bool   `json:"google_use_adc,omitempty"`          // Use Application Default Credentials

	// Runtime-detected provider (not persisted to config file)
	Provider string `json:"-"` // Detected from BaseURL at runtime

	// Default marks this named config as the one loaded when no -config flag
	// is given. Exactly one file should set it; the first match wins.
	Default bool `json:"default,omitempty"`

	// Confirm mode: when true, Celeste proposes actions before executing
	// write/generate operations. When false, she auto-executes.
	ConfirmActions bool `json:"confirm_actions,omitempty"`

	// ElevenLabs TTS settings
	ElevenLabsAPIKey  string `json:"elevenlabs_api_key,omitempty"`
	ElevenLabsVoiceID string `json:"elevenlabs_voice_id,omitempty"`

	// Streaming settings
	SimulateTyping bool `json:"simulate_typing"`
	TypingSpeed    int  `json:"typing_speed"` // chars per second

	// MaxToolIterations caps the model turns in one chat turn's tool loop.
	MaxToolIterations int `json:"max_tool_iterations,omitempty"`

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

	// Sandbox is the OS sandbox for bash (2.0 W4); see Sandbox.
	Sandbox *Sandbox `json:"sandbox,omitempty"`

	// envFile holds the values the file had for the fields ApplyEnvOverrides
	// replaced, so a later Save writes the file's values, not the run's.
	envFile *envFileValues

	// sandboxFrom is set when Sandbox was filled in from config.json for a
	// named profile; see inheritUserSandbox.
	sandboxFrom *sandboxInherit

	// profile is the named profile this config was read from by LoadNamed
	// ("" = config.json), so a save can go back to that file (#324).
	profile string
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
		BaseURL:           seed.BaseURL,
		Model:             seed.DefaultModel,
		Timeout:           DefaultTimeoutSeconds,
		SimulateTyping:    true,
		TypingSpeed:       DefaultTypingSpeed,
		MaxToolIterations: DefaultMaxToolIterations,
		VeniceBaseURL:     venice.BaseURL,
		VeniceModel:       venice.DefaultModel,
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
	skillsConfig = skillsConfig.forSave()

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

	// Atomic and exactly 0600: skills.json holds API keys, and a torn write
	// would lose every one of them.
	return atomicfile.Write(skillsFile, data, 0600)
}

// Environment overrides. Precedence is environment > config file.
// They apply to this run only: ApplyEnvOverrides is called by the entry
// points that run a session (chat, message, agent, serve), never by the
// loads that are saved back, so a variable's value is never written to disk.
const (
	EnvAPIKey      = "CELESTE_API_KEY"
	EnvAPIEndpoint = "CELESTE_API_ENDPOINT"
	EnvTarotToken  = "TAROT_AUTH_TOKEN"
)

// envFileValues are the loaded file's values for the overridable fields.
type envFileValues struct {
	apiKey, baseURL, tarotToken string // the file's value
	envKey, envURL, envTarot    string // what replaced it ("" = not overridden)
}

// forSave is cfg with any still-unchanged env override swapped back for the
// file's value; a field changed after the override is saved as changed.
// A profile's sandbox inherited from config.json is saved as the
// profile's own (see savedSandbox).
func (c *Config) forSave() *Config {
	if c == nil || (c.envFile == nil && c.sandboxFrom == nil) {
		return c
	}
	out := *c
	out.Sandbox = c.savedSandbox()
	f := c.envFile
	if f == nil {
		return &out
	}
	if f.envKey != "" && out.APIKey == f.envKey {
		out.APIKey = f.apiKey
	}
	if f.envURL != "" && out.BaseURL == f.envURL {
		out.BaseURL = f.baseURL
	}
	if f.envTarot != "" && out.TarotAuthToken == f.envTarot {
		out.TarotAuthToken = f.tarotToken
	}
	return &out
}

// ApplyEnvOverrides lets CELESTE_API_KEY, CELESTE_API_ENDPOINT and
// TAROT_AUTH_TOKEN replace the loaded api_key, base_url and
// tarot_auth_token. An unset or blank variable changes nothing.
func ApplyEnvOverrides(cfg *Config) {
	if cfg == nil {
		return
	}
	f := &envFileValues{apiKey: cfg.APIKey, baseURL: cfg.BaseURL, tarotToken: cfg.TarotAuthToken}
	if v := strings.TrimSpace(os.Getenv(EnvAPIKey)); v != "" {
		cfg.APIKey, f.envKey = v, v
	}
	if v := strings.TrimSpace(os.Getenv(EnvAPIEndpoint)); v != "" {
		cfg.BaseURL, f.envURL = v, v
	}
	if v := strings.TrimSpace(os.Getenv(EnvTarotToken)); v != "" {
		cfg.TarotAuthToken, f.envTarot = v, v
	}
	if f.envKey != "" || f.envURL != "" || f.envTarot != "" {
		cfg.envFile = f
	}
}

// LoadNamedWithEnv is LoadNamed plus the environment overrides, for the
// entry points that run a session.
func LoadNamedWithEnv(name string) (*Config, error) {
	cfg, err := LoadNamed(name)
	if err != nil {
		return nil, err
	}
	ApplyEnvOverrides(cfg)
	sessionWebFetchAllowPrivate.Store(cfg.WebFetchAllowPrivate)
	return cfg, nil
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
	config.profile = name
	configPath := NamedConfigPath(name)

	// Load named config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("config '%s' not found at %s: %w", name, configPath, err)
	}
	tightenPrivate(filepath.Dir(configPath))
	tightenPrivate(configPath)
	data = migrateFile(configPath, data)

	if err := json.Unmarshal(data, config); err != nil {
		return nil, fmt.Errorf("failed to parse config '%s': %w", name, err)
	}

	// Same validation Load applies; before #147 named profiles skipped it and
	// kept retired models, empty models and oversized context limits.
	// Reconciled before the skills merge so only the profile's own fields are
	// persisted.
	if reconcileLoaded(config) {
		if err := persistReconciled(configPath, config); err != nil {
			log.Printf("[config] could not save reconciled profile %q: %v", name, err)
		}
	}

	// The user's sandbox settings live in config.json (docs/SANDBOX.md);
	// a profile inherits every key it does not set itself.
	if err := config.inheritUserSandbox(); err != nil {
		return nil, err
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
	configDir, configFile, secretsFile, skillsFile := Paths()

	// Ensure config directory exists, owner-only: it holds every key,
	// log and transcript. An older version made it 0755, and made the
	// files below 0644; tighten those too.
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}
	tightenPrivate(configDir)
	tightenPrivate(configFile)
	tightenPrivate(secretsFile)
	tightenPrivate(skillsFile)
	// Named profiles hold keys too, and one an older version wrote 0644 may
	// never be opened by LoadNamed.
	if profiles, err := filepath.Glob(filepath.Join(configDir, "config.*.json")); err == nil {
		for _, p := range profiles {
			tightenPrivate(p)
		}
	}

	// Load main config file
	if data, err := os.ReadFile(configFile); err == nil {
		data = migrateFile(configFile, data)
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

	// A config the user made read-only is reconciled in memory only.
	if reconcileLoaded(config) && !readOnlyFile(configFile) {
		_ = Save(config)
	}

	return config, nil
}

// readOnlyFile reports an existing file without owner write permission: one
// the user made read-only, which an atomic save (rename over it) would
// replace regardless, so automatic saves leave it alone.
func readOnlyFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().Perm()&0o200 == 0
}

// reconcileLoaded applies the post-read validation every loaded config gets,
// whichever loader read it (#147). Returns true when the config changed and
// should be persisted.
//
// It reconciles the model so the saved config reflects what's actually used
// (#51): an empty model falls back to the provider's default, and models xAI
// no longer supports are migrated to their replacement. It then clamps a stale
// context_limit that exceeds the (possibly migrated) model's real window, such
// as a 2M limit carried over onto a 256K model.
func reconcileLoaded(config *Config) (dirty bool) {
	if changed, from, to := reconcileModel(config); changed {
		if from == "" {
			log.Printf("[config] no model set — using default %q (saved)", to)
		} else {
			log.Printf("[config] model %q is no longer supported — migrated to %q (saved)", from, to)
		}
		dirty = true
	}
	if config.ContextLimit > 0 {
		// Only reject a limit we can actually contradict. For a model absent
		// from the table the "limit" is a conservative fallback, so clamping
		// against it silently deleted correct settings — a local server's
		// window is whatever it was started with, and 8192 is a guess.
		if maxLimit, known := LookupModelLimit(config.Model); known && config.ContextLimit > maxLimit {
			log.Printf("[config] context_limit %d exceeds %q's %d-token window — using model default (saved)", config.ContextLimit, config.Model, maxLimit)
			config.ContextLimit = 0
			dirty = true
		}
	}
	return dirty
}

// persistReconciled writes the fields reconcileLoaded may change back into a
// named profile file, leaving every other key exactly as the user wrote it.
// Rewriting the whole *Config would copy in the defaults and the skills.json
// secrets that LoadNamed merges after reading the file. A profile without
// owner write permission is left as it is: the user made it read-only, and
// the atomic rename would replace it regardless of its mode.
func persistReconciled(path string, config *Config) error {
	if readOnlyFile(path) {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	raw["model"] = config.Model
	if config.AgentModel != "" {
		raw["agent_model"] = config.AgentModel
	}
	if config.ContextLimit > 0 {
		raw["context_limit"] = config.ContextLimit
	} else {
		delete(raw, "context_limit")
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}
	// Atomic and owner-only: the profile can hold an API key.
	return writePrivate(path, out)
}

// writePrivate replaces a credential-bearing file atomically with an
// owner-only mode (0600 when new; an existing file keeps only its owner
// bits, so a read-only one stays read-only), and says so when an older
// file was readable by other users.
func writePrivate(path string, data []byte) error {
	loosened, err := privfs.WriteFile(path, data)
	if err == nil && loosened {
		log.Printf("[config] %s was readable by other users; it is now owner-only", path)
	}
	return err
}

// tightenPrivate strips the group and other bits from an existing
// credential-bearing file or directory, with a warning when it had them.
func tightenPrivate(path string) {
	changed, err := privfs.Tighten(path)
	switch {
	case err != nil:
		log.Printf("[config] could not make %s owner-only: %v", path, err)
	case changed:
		log.Printf("[config] %s was readable by other users; it is now owner-only", path)
	}
}

// deprecatedModels maps the Grok models xAI routes to the cost-prohibitive
// grok-4.3 to a safe replacement (#51). It lives in providers so model
// resolution never picks one either.
var deprecatedModels = providers.DeprecatedModels

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

// ResolveSmallModel returns the model for housekeeping calls (compaction
// summaries): SmallModel if set, otherwise the chat Model.
func (c *Config) ResolveSmallModel() string {
	if c.SmallModel != "" {
		return c.SmallModel
	}
	return c.Model
}

// Save saves configuration to file.
func Save(config *Config) error {
	_, configFile, _, _ := Paths()
	config = config.forSave()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Atomic, so a crash or a concurrent reader never sees half a config,
	// and owner-only, new or existing: the config can hold an API key.
	return writePrivate(configFile, data)
}

// SaveNamed writes the config to a named profile file (config.<name>.json).
// Named profiles store everything inline, including the API key — that is how
// LoadNamed reads them — so unlike Save there is no separate secrets file. An
// empty name falls back to the default Save path.
func SaveNamed(name string, config *Config) error {
	if name == "" {
		return Save(config)
	}
	config = config.forSave()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Ensure ~/.celeste exists (first-run without --init wouldn't have created it).
	path := NamedConfigPath(name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("failed to create config dir: %w", err)
	}

	// Owner-only: a named profile carries the API key inline.
	return writePrivate(path, data)
}

// SaveCollections writes cfg's collections block back to the file cfg was
// loaded from: the named profile LoadNamed read, or config.json. Only the
// "collections" key changes; every other key stays as the file has it, so
// nothing a load merged in (config.json's sandbox, skills.json and
// secrets.json values, defaults) is copied into the wrong file (#324).
func SaveCollections(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("no config to save")
	}
	path := NamedConfigPath(cfg.profile)
	raw := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &raw); err != nil {
			return fmt.Errorf("failed to parse %s: %w", filepath.Base(path), err)
		}
		if raw == nil { // the file holds JSON null
			raw = map[string]json.RawMessage{}
		}
	case os.IsNotExist(err) && cfg.profile == "":
		// No config.json yet: create one holding just the collections.
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return fmt.Errorf("failed to create config dir: %w", err)
		}
	case os.IsNotExist(err):
		// Writing only the collections would leave a profile with no
		// provider or key; never create one here.
		return fmt.Errorf("config '%s' not found at %s: %w", cfg.profile, path, err)
	default:
		return err
	}
	if cfg.Collections == nil {
		delete(raw, "collections")
	} else {
		b, err := json.Marshal(cfg.Collections)
		if err != nil {
			return fmt.Errorf("failed to marshal collections: %w", err)
		}
		raw["collections"] = b
	}
	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	// Atomic and owner-only: the file can hold an API key.
	return writePrivate(path, out)
}

// SaveSecrets saves API key to secrets file (backward compatibility).
func SaveSecrets(config *Config) error {
	_, _, secretsFile, _ := Paths()
	config = config.forSave()

	secrets := &Config{
		APIKey: config.APIKey, // Only API key in secrets.json now
	}

	data, err := json.MarshalIndent(secrets, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal secrets: %w", err)
	}

	// Atomic and exactly 0600, even over an older file with a looser mode.
	return atomicfile.Write(secretsFile, data, 0600)
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
		venice, _ := providers.GetProvider("venice")
		model = venice.DefaultModel
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

// DefaultTimeoutSeconds is the request timeout a new profile gets: how
// long a request may receive nothing from the provider before it fails.
const DefaultTimeoutSeconds = 60

// LocalTimeoutSeconds replaces DefaultTimeoutSeconds for a server on this
// machine or the local network (providers.IsLocalEndpoint). A local model
// sends nothing while it reads the prompt: a cold 16K-token first turn on a
// 14B model took longer than 300 s.
const LocalTimeoutSeconds = 600

// LocalFirstByteSeconds is how long a request to a local server may wait
// for the first byte of the reply (GetFirstByteTimeout). Prefill sends
// nothing: on a loaded machine a 32K prompt on a 14B model took ~563 s to
// the first byte, and a full re-prefill after plan mode 11.5 min (#359).
const LocalFirstByteSeconds = 1800

// GetTimeout returns the request timeout. It is a stall timeout: a request
// fails when nothing arrives for this long, however long the reply takes in
// all (llm.MaxRequestDuration bounds that). A local endpoint whose timeout
// is unset or still the generic default gets LocalTimeoutSeconds; any other
// value is the user's and is kept.
func (c *Config) GetTimeout() time.Duration {
	if c.Timeout > 0 && (c.Timeout != DefaultTimeoutSeconds || !providers.IsLocalEndpoint(c.BaseURL)) {
		return time.Duration(c.Timeout) * time.Second
	}
	if providers.IsLocalEndpoint(c.BaseURL) {
		return LocalTimeoutSeconds * time.Second
	}
	return DefaultTimeoutSeconds * time.Second
}

// GetFirstByteTimeout returns how long a request may wait for the first
// byte of the reply. For a local endpoint it is LocalFirstByteSeconds, or
// GetTimeout when that is longer; once data flows, GetTimeout applies
// between chunks. Hosted providers use GetTimeout for both.
func (c *Config) GetFirstByteTimeout() time.Duration {
	t := c.GetTimeout()
	if providers.IsLocalEndpoint(c.BaseURL) {
		return max(t, LocalFirstByteSeconds*time.Second)
	}
	return t
}
