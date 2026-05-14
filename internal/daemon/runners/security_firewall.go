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

package runners

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// EnforcementLevel describes how much of a client's tool execution MilliWays
// can enforce directly.
type EnforcementLevel string

const (
	EnforcementFull          EnforcementLevel = "full"
	EnforcementBrokered      EnforcementLevel = "brokered"
	EnforcementPreflightOnly EnforcementLevel = "preflight-only"
	EnforcementUnknown       EnforcementLevel = "unknown"
)

// EnforcementMetadata is the client-facing observability/status shape for
// security enforcement. Keep it small so cockpit badges can consume it without
// knowing runner internals.
type EnforcementMetadata struct {
	Level         EnforcementLevel `json:"level"`
	ControlledEnv bool             `json:"controlled_env,omitempty"`
	BrokerPath    string           `json:"broker_path,omitempty"`
	Reason        string           `json:"reason,omitempty"`
}

// CommandFirewallProvider returns the current command firewall for a runner.
// It is configured by the daemon so macOS and Linux app launches share the same
// persisted workspace security policy.
type CommandFirewallProvider func(agentID, workspace string) CommandFirewall

var commandFirewallProvider struct {
	mu sync.RWMutex
	fn CommandFirewallProvider
}

// BrokerPathProvider returns the path to a broker/shim executable or shim
// directory for a runner when one is available.
type BrokerPathProvider func(agentID string) string

var brokerPathProvider struct {
	mu sync.RWMutex
	fn BrokerPathProvider
}

// SetCommandFirewallProvider configures the runtime firewall provider. Passing
// nil disables runtime command firewall injection.
func SetCommandFirewallProvider(fn CommandFirewallProvider) {
	commandFirewallProvider.mu.Lock()
	defer commandFirewallProvider.mu.Unlock()
	commandFirewallProvider.fn = fn
}

// SetBrokerPathProvider configures the future broker/shim path provider used
// to report upgraded enforcement for external CLI clients.
func SetBrokerPathProvider(fn BrokerPathProvider) {
	brokerPathProvider.mu.Lock()
	defer brokerPathProvider.mu.Unlock()
	brokerPathProvider.fn = fn
}

// ClientEnforcementMetadata returns the current enforcement metadata for an
// agent id. It is intentionally independent of availability/auth probing so
// status surfaces can show expected enforcement for every first-class client.
func ClientEnforcementMetadata(agentID string) EnforcementMetadata {
	switch agentID {
	case AgentIDMiniMax, AgentIDLocal:
		return EnforcementMetadata{
			Level:  EnforcementFull,
			Reason: "milliways owns model-requested tool execution",
		}
	case AgentIDClaude, AgentIDCodex, AgentIDCopilot, AgentIDGemini, AgentIDPool:
		if path := brokerPathForAgent(agentID); path != "" {
			return EnforcementMetadata{
				Level:         EnforcementBrokered,
				ControlledEnv: true,
				BrokerPath:    path,
				Reason:        "launched by milliways with filtered environment and broker shim path metadata",
			}
		}
		return EnforcementMetadata{
			Level:         EnforcementPreflightOnly,
			ControlledEnv: true,
			Reason:        "broker shim path unavailable; startup preflight is enforced but command brokerage is not active",
		}
	default:
		return EnforcementMetadata{Level: EnforcementUnknown}
	}
}

func commandFirewallForAgent(agentID string) CommandFirewall {
	return commandFirewallForAgentWorkspace(agentID, "")
}

func commandFirewallForAgentWorkspace(agentID, workspace string) CommandFirewall {
	commandFirewallProvider.mu.RLock()
	fn := commandFirewallProvider.fn
	commandFirewallProvider.mu.RUnlock()
	if fn == nil {
		return nil
	}
	return fn(agentID, workspace)
}

func brokerPathForAgent(agentID string) string {
	brokerPathProvider.mu.RLock()
	fn := brokerPathProvider.fn
	brokerPathProvider.mu.RUnlock()
	if fn == nil {
		return ""
	}
	return fn(agentID)
}

func brokerShimDirForAgent(agentID string) string {
	path := strings.TrimSpace(brokerPathForAgent(agentID))
	if path == "" {
		return ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Dir(path))
}
