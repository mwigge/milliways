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
	Level         EnforcementLevel   `json:"level"`
	ControlledEnv bool               `json:"controlled_env,omitempty"`
	BrokerPath    string             `json:"broker_path,omitempty"`
	Label         string             `json:"label,omitempty"`
	Reason        string             `json:"reason,omitempty"`
	Capabilities  ClientCapabilities `json:"capabilities,omitempty"`
}

// CapabilitySupport labels how MilliWays can participate in one client
// capability. The labels are intentionally stable strings for status surfaces.
type CapabilitySupport string

const (
	CapabilityNative           CapabilitySupport = "native"
	CapabilityRunnerControlled CapabilitySupport = "runner-controlled"
	CapabilityBrokered         CapabilitySupport = "brokered"
	CapabilityPreflightOnly    CapabilitySupport = "preflight-only"
	CapabilityExternal         CapabilitySupport = "external"
	CapabilityUnsupported      CapabilitySupport = "unsupported"
	CapabilityUnknown          CapabilitySupport = "unknown"
)

// ClientCapabilities reports the coding-agent control surface available for a
// runner. Unknown clients stay explicit rather than inheriting unsafe defaults.
type ClientCapabilities struct {
	Tools            CapabilitySupport `json:"tools"`
	Permissions      CapabilitySupport `json:"permissions"`
	FileChanges      CapabilitySupport `json:"file_changes"`
	Contract         ToolContract      `json:"contract"`
	LSP              CapabilitySupport `json:"lsp"`
	MCP              CapabilitySupport `json:"mcp"`
	Memory           CapabilitySupport `json:"memory"`
	Observability    CapabilitySupport `json:"observability"`
	EnforcementLevel EnforcementLevel  `json:"enforcement_level"`
}

// ToolContract reports the baseline agentic operations a client can perform
// natively or through MilliWays brokerage.
type ToolContract struct {
	Read             CapabilitySupport `json:"read"`
	Write            CapabilitySupport `json:"write"`
	Edit             CapabilitySupport `json:"edit"`
	Delete           CapabilitySupport `json:"delete"`
	Bash             CapabilitySupport `json:"bash"`
	Glob             CapabilitySupport `json:"glob"`
	Grep             CapabilitySupport `json:"grep"`
	ListTree         CapabilitySupport `json:"list_tree"`
	Artifacts        CapabilitySupport `json:"artifacts"`
	Approvals        CapabilitySupport `json:"approvals"`
	StructuredErrors CapabilitySupport `json:"structured_errors"`
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

// ToolHooksProvider returns the permission/change-tracking hooks for a runner
// session. It is configured by the daemon so runner-owned HTTP/local tools can
// share the same approval, audit, and memory plumbing as the control plane.
type ToolHooksProvider func(agentID, workspace string) ToolHooks

var toolHooksProvider struct {
	mu sync.RWMutex
	fn ToolHooksProvider
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

// SetToolHooksProvider configures runner-owned tool permission/change hooks.
// Passing nil disables hook injection.
func SetToolHooksProvider(fn ToolHooksProvider) {
	toolHooksProvider.mu.Lock()
	defer toolHooksProvider.mu.Unlock()
	toolHooksProvider.fn = fn
}

// ClientEnforcementMetadata returns the current enforcement metadata for an
// agent id. It is intentionally independent of availability/auth probing so
// status surfaces can show expected enforcement for every first-class client.
func ClientEnforcementMetadata(agentID string) EnforcementMetadata {
	caps := ClientCapabilitiesForAgent(agentID)
	switch agentID {
	case AgentIDMiniMax, AgentIDLocal, AgentIDKimi, AgentIDDeepSeek:
		return EnforcementMetadata{
			Level:        EnforcementFull,
			Label:        "http/local full",
			Reason:       "milliways owns model-requested tool execution",
			Capabilities: caps,
		}
	case AgentIDClaude, AgentIDCodex, AgentIDCopilot, AgentIDGemini, AgentIDPool:
		if path := brokerPathForAgent(agentID); path != "" {
			return EnforcementMetadata{
				Level:         EnforcementBrokered,
				ControlledEnv: true,
				BrokerPath:    path,
				Label:         "external brokered",
				Reason:        "launched by milliways with filtered environment and broker shim path metadata",
				Capabilities:  caps,
			}
		}
		return EnforcementMetadata{
			Level:         EnforcementPreflightOnly,
			ControlledEnv: true,
			Label:         "external preflight",
			Reason:        "broker shim path unavailable; startup preflight is enforced but command brokerage is not active",
			Capabilities:  caps,
		}
	default:
		return EnforcementMetadata{Level: EnforcementUnknown, Label: "unknown", Capabilities: caps}
	}
}

// ClientCapabilitiesForAgent returns a future-safe capability report for known
// first-class runners and explicit unknowns for agents added outside this build.
func ClientCapabilitiesForAgent(agentID string) ClientCapabilities {
	switch agentID {
	case AgentIDMiniMax, AgentIDLocal, AgentIDKimi, AgentIDDeepSeek:
		return ClientCapabilities{
			Tools:            CapabilityRunnerControlled,
			Permissions:      CapabilityRunnerControlled,
			FileChanges:      CapabilityRunnerControlled,
			Contract:         nativeToolContract(CapabilityRunnerControlled, CapabilityRunnerControlled),
			LSP:              CapabilityUnsupported,
			MCP:              CapabilityUnsupported,
			Memory:           CapabilityRunnerControlled,
			Observability:    CapabilityRunnerControlled,
			EnforcementLevel: EnforcementFull,
		}
	case AgentIDClaude, AgentIDCodex, AgentIDCopilot, AgentIDGemini, AgentIDPool:
		if brokerPathForAgent(agentID) != "" {
			return ClientCapabilities{
				Tools:            CapabilityBrokered,
				Permissions:      CapabilityBrokered,
				FileChanges:      CapabilityBrokered,
				Contract:         nativeToolContract(CapabilityBrokered, CapabilityBrokered),
				LSP:              CapabilityUnsupported,
				MCP:              CapabilityUnsupported,
				Memory:           CapabilityRunnerControlled,
				Observability:    CapabilityRunnerControlled,
				EnforcementLevel: EnforcementBrokered,
			}
		}
		return ClientCapabilities{
			Tools:            CapabilityNative,
			Permissions:      CapabilityPreflightOnly,
			FileChanges:      CapabilityPreflightOnly,
			Contract:         externalPreflightToolContract(),
			LSP:              CapabilityUnsupported,
			MCP:              CapabilityUnsupported,
			Memory:           CapabilityRunnerControlled,
			Observability:    CapabilityRunnerControlled,
			EnforcementLevel: EnforcementPreflightOnly,
		}
	default:
		return ClientCapabilities{
			Tools:            CapabilityUnknown,
			Permissions:      CapabilityUnknown,
			FileChanges:      CapabilityUnknown,
			Contract:         nativeToolContract(CapabilityUnknown, CapabilityUnknown),
			LSP:              CapabilityUnknown,
			MCP:              CapabilityUnknown,
			Memory:           CapabilityUnknown,
			Observability:    CapabilityUnknown,
			EnforcementLevel: EnforcementUnknown,
		}
	}
}

func nativeToolContract(operations, approvals CapabilitySupport) ToolContract {
	return ToolContract{
		Read:             operations,
		Write:            operations,
		Edit:             operations,
		Delete:           operations,
		Bash:             operations,
		Glob:             operations,
		Grep:             operations,
		ListTree:         operations,
		Artifacts:        operations,
		Approvals:        approvals,
		StructuredErrors: operations,
	}
}

func externalPreflightToolContract() ToolContract {
	return ToolContract{
		Read:             CapabilityNative,
		Write:            CapabilityPreflightOnly,
		Edit:             CapabilityPreflightOnly,
		Delete:           CapabilityPreflightOnly,
		Bash:             CapabilityPreflightOnly,
		Glob:             CapabilityNative,
		Grep:             CapabilityNative,
		ListTree:         CapabilityNative,
		Artifacts:        CapabilityUnsupported,
		Approvals:        CapabilityPreflightOnly,
		StructuredErrors: CapabilityUnsupported,
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

func toolHooksForAgentWorkspace(agentID, workspace string) ToolHooks {
	toolHooksProvider.mu.RLock()
	fn := toolHooksProvider.fn
	toolHooksProvider.mu.RUnlock()
	if fn == nil {
		return ToolHooks{}
	}
	return fn(agentID, workspace)
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
