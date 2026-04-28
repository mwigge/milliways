// Copyright 2024 The milliways Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
