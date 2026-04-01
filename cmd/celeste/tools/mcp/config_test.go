// cmd/celeste/tools/mcp/config_test.go
package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadConfig_ValidStdio(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	configJSON := `{
		"mcpServers": {
			"weather": {
				"transport": "stdio",
				"command": "npx",
				"args": ["-y", "@weather/mcp-server"],
				"env": {
					"API_KEY": "test-key"
				}
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)
	require.Len(t, config.Servers, 1)

	server := config.Servers["weather"]
	assert.Equal(t, "stdio", server.Transport)
	assert.Equal(t, "npx", server.Command)
	assert.Equal(t, []string{"-y", "@weather/mcp-server"}, server.Args)
	assert.Equal(t, "test-key", server.Env["API_KEY"])
}

func TestLoadConfig_ValidSSE(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	configJSON := `{
		"mcpServers": {
			"remote": {
				"transport": "sse",
				"url": "http://localhost:3000/sse"
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)
	require.Len(t, config.Servers, 1)

	server := config.Servers["remote"]
	assert.Equal(t, "sse", server.Transport)
	assert.Equal(t, "http://localhost:3000/sse", server.URL)
}

func TestLoadConfig_MultipleServers(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	configJSON := `{
		"mcpServers": {
			"server1": {
				"transport": "stdio",
				"command": "node",
				"args": ["server1.js"]
			},
			"server2": {
				"transport": "sse",
				"url": "http://example.com/sse"
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)
	assert.Len(t, config.Servers, 2)
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	config, err := LoadConfig("/nonexistent/mcp.json")
	assert.NoError(t, err) // Missing config is not an error -- just no servers
	assert.NotNil(t, config)
	assert.Empty(t, config.Servers)
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{bad json`), 0644))

	_, err := LoadConfig(configPath)
	assert.Error(t, err)
}

func TestLoadConfig_EnvExpansion(t *testing.T) {
	t.Setenv("MCP_SECRET_KEY", "secret123")

	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	configJSON := `{
		"mcpServers": {
			"secure": {
				"transport": "stdio",
				"command": "secure-server",
				"env": {
					"API_KEY": "${MCP_SECRET_KEY}"
				}
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)

	// env expansion happens at transport creation time, not at config load time
	// so the raw value should still contain the ${...} reference
	assert.Equal(t, "${MCP_SECRET_KEY}", config.Servers["secure"].Env["API_KEY"])
}

func TestLoadConfig_DefaultTransport(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mcp.json")

	// No "transport" field -- should default to "stdio"
	configJSON := `{
		"mcpServers": {
			"simple": {
				"command": "my-server"
			}
		}
	}`

	require.NoError(t, os.WriteFile(configPath, []byte(configJSON), 0644))

	config, err := LoadConfig(configPath)
	require.NoError(t, err)

	server := config.Servers["simple"]
	assert.Equal(t, "stdio", server.Transport)
}

func TestDefaultConfigPath(t *testing.T) {
	path := DefaultConfigPath()
	assert.Contains(t, path, ".celeste")
	assert.Contains(t, path, "mcp.json")
}
