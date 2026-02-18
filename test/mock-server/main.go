// Package main provides a mock API server for testing CelesteCLI
// Simulates OpenAI, Venice.ai, Weather, and other external APIs
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Server configuration
type Config struct {
	Port        string
	FixturesDir string
}

func main() {
	config := Config{
		Port:        getEnv("PORT", "8080"),
		FixturesDir: getEnv("FIXTURES_DIR", "./fixtures"),
	}

	http.HandleFunc("/health", handleHealth)
	http.HandleFunc("/v1/chat/completions", handleChatCompletions(config))
	http.HandleFunc("/api/v1/chat", handleVeniceChat(config))
	http.HandleFunc("/", handleGeneric(config))

	addr := ":" + config.Port
	log.Printf("🧪 Mock API server starting on %s", addr)
	log.Printf("📁 Fixtures directory: %s", config.FixturesDir)

	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

// Health check endpoint
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// Ignore encoding error as this is a simple test server health check
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "healthy",
		"service": "celestecli-mock-api",
		"time":    time.Now().Format(time.RFC3339),
	})
}

// OpenAI-compatible chat completions (with tool calls)
func handleChatCompletions(config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse request
		var req map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// Check if tools are provided
		tools, hasTools := req["tools"].([]interface{})

		// Determine response type
		var fixtureName string
		if hasTools && len(tools) > 0 {
			// Check the last user message for keywords
			messages, ok := req["messages"].([]interface{})
			if ok && len(messages) > 0 {
				lastMsg := messages[len(messages)-1].(map[string]interface{})
				content := strings.ToLower(lastMsg["content"].(string))

				// Match content to appropriate fixture
				if strings.Contains(content, "weather") {
					fixtureName = "openai/tool-call-weather.json"
				} else if strings.Contains(content, "tarot") {
					fixtureName = "openai/tool-call-tarot.json"
				} else {
					fixtureName = "openai/simple-response.json"
				}
			}
		} else {
			fixtureName = "openai/simple-response.json"
		}

		// Load fixture
		fixture := loadFixture(config.FixturesDir, fixtureName)
		if fixture == nil {
			// Generate default response
			fixture = generateDefaultResponse()
		}

		// Return response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Ignore encoding error as test server responses are best-effort
		_ = json.NewEncoder(w).Encode(fixture)

		log.Printf("✅ Served OpenAI completion: %s", fixtureName)
	}
}

// Venice.ai chat endpoint
func handleVeniceChat(config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		fixtureName := "venice/nsfw-response.json"
		fixture := loadFixture(config.FixturesDir, fixtureName)
		if fixture == nil {
			fixture = map[string]interface{}{
				"response": "Test NSFW response from Venice.ai mock",
				"model":    "venice-uncensored",
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Ignore encoding error as test server responses are best-effort
		_ = json.NewEncoder(w).Encode(fixture)

		log.Printf("✅ Served Venice.ai response: %s", fixtureName)
	}
}

// Generic handler for other endpoints (wttr.in, etc.)
func handleGeneric(config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Match path to fixture
		var fixtureName string
		if strings.Contains(path, "wttr") || strings.Contains(path, "weather") {
			fixtureName = "weather/wttr-10001.json"
		} else {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprintf(w, "Mock endpoint not found: %s", path)
			return
		}

		fixture := loadFixture(config.FixturesDir, fixtureName)
		if fixture == nil {
			http.Error(w, "Fixture not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		// Ignore encoding error as test server responses are best-effort
		_ = json.NewEncoder(w).Encode(fixture)

		log.Printf("✅ Served generic response: %s", fixtureName)
	}
}

// Load fixture from file
func loadFixture(baseDir, name string) map[string]interface{} {
	path := filepath.Join(baseDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		log.Printf("⚠️  Failed to load fixture %s: %v", name, err)
		return nil
	}

	var fixture map[string]interface{}
	if err := json.Unmarshal(data, &fixture); err != nil {
		log.Printf("⚠️  Failed to parse fixture %s: %v", name, err)
		return nil
	}

	return fixture
}

// Generate default OpenAI response
func generateDefaultResponse() map[string]interface{} {
	return map[string]interface{}{
		"id":      "chatcmpl-test-" + fmt.Sprint(time.Now().Unix()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   "gpt-4o-mini",
		"choices": []interface{}{
			map[string]interface{}{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": "This is a mock response from the test server.",
				},
				"finish_reason": "stop",
			},
		},
	}
}

// Get environment variable with default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
