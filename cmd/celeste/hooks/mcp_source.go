package hooks

import (
	"path/filepath"
	"strings"
)

// KindRepoMCP is one server in a workspace's .mcp.json or
// .celeste/mcp.json. An enabled one would start its command (or connect
// to its URL) when the chat launches, so it is trusted like a repo hook:
// by a hash of what it runs, in the same store, with the same
// `celeste hooks trust` flow.
const KindRepoMCP SourceKind = "repo-mcp"

// mcpSuffix separates a server's name from its config file in the trust
// key. The file is always named mcp.json or .mcp.json, so the first
// "mcp.json#mcp:" ends the file part even when the name holds the suffix.
const mcpSuffix = "#mcp:"

// MCPSource is the trust source for server name in the workspace MCP
// config at configPath: keyed by the file's path plus "#mcp:<name>",
// pinned to hash (mcp.ServerConfig.TrustHash), described by summary
// (mcp.ServerConfig.TrustSummary).
func MCPSource(configPath, name, summary, hash string) Source {
	root := filepath.Dir(configPath)
	if filepath.Base(root) == ".celeste" {
		root = filepath.Dir(root)
	}
	return Source{
		Path:  configPath + mcpSuffix + name,
		Root:  root,
		Kind:  KindRepoMCP,
		Rules: summary,
		Hash:  hash,
	}
}

// MCPServerName is the server name of a KindRepoMCP source.
func MCPServerName(src Source) string {
	if i := strings.Index(src.Path, "mcp.json"+mcpSuffix); i >= 0 {
		return src.Path[i+len("mcp.json"+mcpSuffix):]
	}
	return ""
}

func mcpSourceFile(path string) string {
	if i := strings.Index(path, "mcp.json"+mcpSuffix); i >= 0 {
		return path[:i+len("mcp.json")]
	}
	return path
}
