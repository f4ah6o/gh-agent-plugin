package discovery

import (
	"encoding/json"
	"os"
	"sort"
)

// mcpConfig is the minimal shape of a .mcp.json file we need to enumerate the
// configured MCP servers and inspect them for security findings.
type mcpConfig struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

type mcpServer struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	URL     string            `json:"url"`
	Env     map[string]string `json:"env"`
}

// readMCP parses a .mcp.json file, returning a zero config on any error.
func readMCP(path string) mcpConfig {
	var cfg mcpConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	return cfg
}

// mcpServerNames returns the sorted names of MCP servers declared in a .mcp.json.
func mcpServerNames(path string) []string {
	cfg := readMCP(path)
	out := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
