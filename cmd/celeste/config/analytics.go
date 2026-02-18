package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// GlobalAnalytics tracks cumulative usage across all sessions
type GlobalAnalytics struct {
	TotalSessions int     `json:"total_sessions"`
	TotalMessages int     `json:"total_messages"`
	TotalTokens   int     `json:"total_tokens"`
	TotalCost     float64 `json:"total_cost"`

	// Per-provider breakdown
	ProviderUsage map[string]*ProviderStats `json:"provider_usage"`

	// Per-model breakdown
	ModelUsage map[string]*ModelStats `json:"model_usage"`

	// Time-based tracking (YYYY-MM-DD -> stats)
	DailyUsage map[string]*DailyStats `json:"daily_usage"`

	LastUpdated time.Time `json:"last_updated"`
}

// ProviderStats tracks usage for a specific provider
type ProviderStats struct {
	SessionCount int     `json:"session_count"`
	MessageCount int     `json:"message_count"`
	TokenCount   int     `json:"token_count"`
	Cost         float64 `json:"cost"`
}

// ModelStats tracks usage for a specific model
type ModelStats struct {
	SessionCount int     `json:"session_count"`
	MessageCount int     `json:"message_count"`
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	Cost         float64 `json:"cost"`
}

// DailyStats tracks usage for a specific day
type DailyStats struct {
	Date         string  `json:"date"`
	SessionCount int     `json:"session_count"`
	MessageCount int     `json:"message_count"`
	TokenCount   int     `json:"token_count"`
	Cost         float64 `json:"cost"`
}

// NewGlobalAnalytics creates a new empty analytics tracker
func NewGlobalAnalytics() *GlobalAnalytics {
	return &GlobalAnalytics{
		ProviderUsage: make(map[string]*ProviderStats),
		ModelUsage:    make(map[string]*ModelStats),
		DailyUsage:    make(map[string]*DailyStats),
		LastUpdated:   time.Now(),
	}
}

// LoadGlobalAnalytics loads analytics from ~/.celeste/analytics.json
func LoadGlobalAnalytics() (*GlobalAnalytics, error) {
	analyticsPath := GetAnalyticsPath()

	// Check if file exists
	if _, err := os.Stat(analyticsPath); os.IsNotExist(err) {
		// Return new analytics if file doesn't exist
		return NewGlobalAnalytics(), nil
	}

	// Read file
	data, err := os.ReadFile(analyticsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read analytics file: %w", err)
	}

	// Parse JSON
	var analytics GlobalAnalytics
	if err := json.Unmarshal(data, &analytics); err != nil {
		return nil, fmt.Errorf("failed to parse analytics JSON: %w", err)
	}

	// Initialize maps if nil (for backward compatibility)
	if analytics.ProviderUsage == nil {
		analytics.ProviderUsage = make(map[string]*ProviderStats)
	}
	if analytics.ModelUsage == nil {
		analytics.ModelUsage = make(map[string]*ModelStats)
	}
	if analytics.DailyUsage == nil {
		analytics.DailyUsage = make(map[string]*DailyStats)
	}

	return &analytics, nil
}

// Save persists analytics to ~/.celeste/analytics.json
func (ga *GlobalAnalytics) Save() error {
	analyticsPath := GetAnalyticsPath()

	// Ensure directory exists
	dir := filepath.Dir(analyticsPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create analytics directory: %w", err)
	}

	// Update timestamp
	ga.LastUpdated = time.Now()

	// Marshal to JSON
	data, err := json.MarshalIndent(ga, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal analytics: %w", err)
	}

	// Write file
	if err := os.WriteFile(analyticsPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write analytics file: %w", err)
	}

	return nil
}

// UpdateFromSession updates analytics with data from a session
func (ga *GlobalAnalytics) UpdateFromSession(session *Session) {
	if session == nil {
		return
	}

	// Only update if session has usage metrics
	if session.UsageMetrics == nil {
		return
	}

	metrics := session.UsageMetrics

	// Update totals
	ga.TotalSessions++
	ga.TotalMessages += metrics.MessageCount
	ga.TotalTokens += metrics.TotalTokens
	ga.TotalCost += metrics.EstimatedCost

	// Update provider stats
	provider := session.Provider
	if provider == "" {
		provider = "unknown"
	}
	if ga.ProviderUsage[provider] == nil {
		ga.ProviderUsage[provider] = &ProviderStats{}
	}
	ga.ProviderUsage[provider].SessionCount++
	ga.ProviderUsage[provider].MessageCount += metrics.MessageCount
	ga.ProviderUsage[provider].TokenCount += metrics.TotalTokens
	ga.ProviderUsage[provider].Cost += metrics.EstimatedCost

	// Update model stats
	model := session.Model
	if model == "" {
		model = "unknown"
	}
	if ga.ModelUsage[model] == nil {
		ga.ModelUsage[model] = &ModelStats{}
	}
	ga.ModelUsage[model].SessionCount++
	ga.ModelUsage[model].MessageCount += metrics.MessageCount
	ga.ModelUsage[model].InputTokens += metrics.TotalInputTokens
	ga.ModelUsage[model].OutputTokens += metrics.TotalOutputTokens
	ga.ModelUsage[model].Cost += metrics.EstimatedCost

	// Update daily stats
	dateKey := time.Now().Format("2006-01-02")
	if ga.DailyUsage[dateKey] == nil {
		ga.DailyUsage[dateKey] = &DailyStats{
			Date: dateKey,
		}
	}
	ga.DailyUsage[dateKey].SessionCount++
	ga.DailyUsage[dateKey].MessageCount += metrics.MessageCount
	ga.DailyUsage[dateKey].TokenCount += metrics.TotalTokens
	ga.DailyUsage[dateKey].Cost += metrics.EstimatedCost
}

// GetTopModels returns the top N models by usage
func (ga *GlobalAnalytics) GetTopModels(n int) []ModelStats {
	// Convert map to slice
	models := make([]ModelStats, 0, len(ga.ModelUsage))
	for _, stats := range ga.ModelUsage {
		model := *stats
		// Store model name in a custom way (we'll add it to the string representation)
		models = append(models, model)
	}

	// Sort by session count (descending)
	sort.Slice(models, func(i, j int) bool {
		return models[i].SessionCount > models[j].SessionCount
	})

	// Return top N
	if n > len(models) {
		n = len(models)
	}
	return models[:n]
}

// GetTopModelNames returns the top N model names with their stats
func (ga *GlobalAnalytics) GetTopModelNames(n int) []struct {
	Name  string
	Stats ModelStats
} {
	// Convert map to slice with names
	type namedModel struct {
		Name  string
		Stats ModelStats
	}
	models := make([]namedModel, 0, len(ga.ModelUsage))
	for name, stats := range ga.ModelUsage {
		models = append(models, namedModel{Name: name, Stats: *stats})
	}

	// Sort by session count (descending)
	sort.Slice(models, func(i, j int) bool {
		return models[i].Stats.SessionCount > models[j].Stats.SessionCount
	})

	// Return top N
	if n > len(models) {
		n = len(models)
	}

	result := make([]struct {
		Name  string
		Stats ModelStats
	}, n)
	for i := 0; i < n; i++ {
		result[i].Name = models[i].Name
		result[i].Stats = models[i].Stats
	}
	return result
}

// GetWeeklyUsage returns usage stats for the last 7 days
func (ga *GlobalAnalytics) GetWeeklyUsage() []DailyStats {
	stats := make([]DailyStats, 0, 7)

	// Get last 7 days
	for i := 6; i >= 0; i-- {
		date := time.Now().AddDate(0, 0, -i).Format("2006-01-02")
		if dailyStats, exists := ga.DailyUsage[date]; exists {
			stats = append(stats, *dailyStats)
		} else {
			// Add empty stats for missing days
			stats = append(stats, DailyStats{
				Date:         date,
				SessionCount: 0,
				MessageCount: 0,
				TokenCount:   0,
				Cost:         0,
			})
		}
	}

	return stats
}

// GetTopProviders returns providers sorted by usage
func (ga *GlobalAnalytics) GetTopProviders() []struct {
	Name  string
	Stats ProviderStats
} {
	// Convert map to slice with names
	type namedProvider struct {
		Name  string
		Stats ProviderStats
	}
	providers := make([]namedProvider, 0, len(ga.ProviderUsage))
	for name, stats := range ga.ProviderUsage {
		providers = append(providers, namedProvider{Name: name, Stats: *stats})
	}

	// Sort by session count (descending)
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].Stats.SessionCount > providers[j].Stats.SessionCount
	})

	result := make([]struct {
		Name  string
		Stats ProviderStats
	}, len(providers))
	for i, p := range providers {
		result[i].Name = p.Name
		result[i].Stats = p.Stats
	}
	return result
}

// GetAnalyticsPath returns the path to the analytics file
func GetAnalyticsPath() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".celeste", "analytics.json")
	}
	return filepath.Join(homeDir, ".celeste", "analytics.json")
}
