package mcp

import "fmt"

// ServerConfig defines one MCP server connection.
type ServerConfig struct {
	Type    string
	Command string
	Args    []string
	Env     []string
}

// LoadServers constructs MCP server instances from configuration.
func LoadServers(config map[string]ServerConfig) ([]*Server, error) {
	servers := make([]*Server, 0, len(config))
	for name, cfg := range config {
		server, err := NewServer(name, cfg)
		if err != nil {
			return nil, fmt.Errorf("load server %q: %w", name, err)
		}
		servers = append(servers, server)
	}
	return servers, nil
}
