package adapter

import (
	"encoding/json"
	"fmt"
	"strings"
)

// CapabilityContractVersion identifies the version of the supervisory
// capability contract implemented by this package. It is reported in
// ClientPolicy.CapabilityContract so consumers can detect mismatches.
const CapabilityContractVersion = "supervisory-agent-capabilities/v1"

// defaultMaxTokens is the Anthropic max_tokens value used when a
// NormalizedRequest does not specify one.
const defaultMaxTokens = 4096

// Protocol identifies the wire format an adapter expects when receiving a
// normalized request.
type Protocol string

const (
	// ProtocolOpenAIChat is the OpenAI chat completions request format.
	ProtocolOpenAIChat Protocol = "openai-chat-completions"
	// ProtocolOpenAIResponses is the OpenAI responses API request format.
	ProtocolOpenAIResponses Protocol = "openai-responses"
	// ProtocolAnthropic is the Anthropic messages API request format.
	ProtocolAnthropic Protocol = "anthropic-messages"
	// ProtocolNativeCLI passes the normalized request through unchanged for
	// adapters that drive a native command-line interface.
	ProtocolNativeCLI Protocol = "native-cli"
)

// NormalizedMessage represents a single chat message in the
// protocol-agnostic request and result format.
type NormalizedMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// NormalizedTool describes a tool definition in the protocol-agnostic
// request format, ready to be translated into the target protocol's
// tool/function schema.
type NormalizedTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
}

// NormalizedRequest is the protocol-agnostic representation of an agent
// request. NormalizeRequest translates it into the wire format required by
// a specific Protocol.
type NormalizedRequest struct {
	Version      string              `json:"version"`
	Model        string              `json:"model"`
	Messages     []NormalizedMessage `json:"messages"`
	Tools        []NormalizedTool    `json:"tools,omitempty"`
	ResponseJSON bool                `json:"response_json"`
	// MaxTokens is the maximum number of tokens to generate. If zero,
	// NormalizeRequest substitutes defaultMaxTokens for protocols that
	// require this field (currently ProtocolAnthropic).
	MaxTokens int `json:"max_tokens,omitempty"`
}

// NormalizedResult is the protocol-agnostic representation of an agent
// response, translated back from the adapter's native wire format.
type NormalizedResult struct {
	Version      string               `json:"version"`
	Message      NormalizedMessage    `json:"message"`
	ToolCalls    []NormalizedToolCall `json:"tool_calls,omitempty"`
	Model        string               `json:"model,omitempty"`
	FinishReason string               `json:"finish_reason,omitempty"`
	Healthy      bool                 `json:"healthy"`
}

// NormalizedToolCall represents a single tool invocation requested by the
// model within a NormalizedResult.
type NormalizedToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// ClientPolicy describes how a supervisor should communicate with an
// adapter: which protocol to speak, which headers to avoid, and whether the
// adapter must be run directly rather than supervised.
type ClientPolicy struct {
	AgentID            string            `json:"agent_id"`
	Protocol           Protocol          `json:"protocol"`
	TierAliases        map[string]string `json:"tier_aliases"`
	Authentication     string            `json:"authentication"`
	UnsupportedHeaders []string          `json:"unsupported_headers,omitempty"`
	DirectRunOnly      bool              `json:"direct_run_only"`
	CapabilityContract string            `json:"capability_contract"`
}

// NormalizeRequest translates a protocol-agnostic NormalizedRequest into the
// wire format expected by the given Protocol. It returns an error if the
// protocol is not recognized.
func NormalizeRequest(protocol Protocol, request NormalizedRequest) (map[string]any, error) {
	switch protocol {
	case ProtocolOpenAIChat:
		return map[string]any{
			"model": request.Model, "messages": request.Messages, "tools": request.Tools,
			"response_format": map[string]string{"type": responseType(request.ResponseJSON)},
		}, nil
	case ProtocolOpenAIResponses:
		return map[string]any{
			"model": request.Model, "input": request.Messages, "tools": request.Tools,
			"text": map[string]any{"format": map[string]string{"type": responseType(request.ResponseJSON)}},
		}, nil
	case ProtocolAnthropic:
		maxTokens := request.MaxTokens
		if maxTokens == 0 {
			maxTokens = defaultMaxTokens
		}
		return map[string]any{
			"model": request.Model, "messages": request.Messages, "tools": request.Tools,
			"max_tokens": maxTokens,
		}, nil
	case ProtocolNativeCLI:
		if request.Version == "" {
			request.Version = "execution-chat/v1"
		}
		return map[string]any{"request": request}, nil
	default:
		return nil, fmt.Errorf("unsupported agent protocol %q", protocol)
	}
}

// responseType maps the ResponseJSON flag to the protocol-level response
// format identifier.
func responseType(structured bool) string {
	if structured {
		return "json_object"
	}
	return "text"
}

// SupervisorCapabilityReport returns the supervisory capabilities for the
// adapter identified by agentID. Unknown agent IDs report no supervisory
// capabilities and remain direct-run only.
func SupervisorCapabilityReport(agentID string) Capabilities {
	id := strings.ToLower(agentID)
	switch id {
	case "claude":
		return supervisory(true, true, true, true, ProtocolAnthropic, true)
	case "codex":
		return supervisory(true, true, true, true, ProtocolOpenAIResponses, true)
	case "gemini":
		return supervisory(true, true, true, false, ProtocolNativeCLI, true)
	case "copilot":
		// Copilot can plan but the adapter does not support structured
		// delegation, so SupervisoryQualified() is false overall and the
		// agent remains direct-run only. This split is intentional.
		return supervisory(true, false, true, false, ProtocolOpenAIChat, true)
	case "pool":
		return supervisory(true, true, true, true, ProtocolNativeCLI, true)
	default:
		return supervisory(false, false, false, false, ProtocolNativeCLI, false)
	}
}

// supervisory builds a Capabilities value for an adapter with the given
// supervisory qualifications. toolCalls reports whether the adapter
// genuinely supports tool calls, independent of its supervisory
// qualification.
func supervisory(plan, delegate, review, continuation bool, protocol Protocol, toolCalls bool) Capabilities {
	return Capabilities{
		Planning:             plan,
		StructuredDelegation: delegate,
		ResultReview:         review,
		Continuation:         continuation,
		NativeProtocol:       string(protocol),
		ToolCalls:            toolCalls,
		StructuredResults:    delegate,
		ModelTierAliases:     plan,
	}
}

// MergeCapabilities overlays the supervisory capability fields from
// supervisory onto base, preserving base's other fields (e.g.
// NativeResume, InteractiveSend) unchanged.
func MergeCapabilities(base, supervisory Capabilities) Capabilities {
	base.Planning = supervisory.Planning
	base.StructuredDelegation = supervisory.StructuredDelegation
	base.ResultReview = supervisory.ResultReview
	base.Continuation = supervisory.Continuation
	base.NativeProtocol = supervisory.NativeProtocol
	base.ToolCalls = supervisory.ToolCalls
	base.StructuredResults = supervisory.StructuredResults
	base.ModelTierAliases = supervisory.ModelTierAliases
	return base
}

// PolicyForAgent returns the ClientPolicy a supervisor should apply when
// communicating with the adapter identified by agentID, derived from its
// SupervisorCapabilityReport.
func PolicyForAgent(agentID string) ClientPolicy {
	caps := SupervisorCapabilityReport(agentID)
	policy := ClientPolicy{
		AgentID:     agentID,
		Protocol:    Protocol(caps.NativeProtocol),
		TierAliases: map[string]string{"fast": "tier-1", "balanced": "tier-2", "best": "tier-3"},
		// Authentication is supplied by the operator at runtime (e.g. via
		// environment variables or an existing CLI login session) rather
		// than embedded in the policy. This is the intended final design,
		// not a placeholder pending implementation.
		Authentication:     "operator-managed-placeholder",
		UnsupportedHeaders: []string{},
		DirectRunOnly:      !caps.SupervisoryQualified(),
		CapabilityContract: CapabilityContractVersion,
	}
	switch strings.ToLower(agentID) {
	case "claude":
		policy.UnsupportedHeaders = []string{"anthropic-beta:context-1m"}
	case "codex":
		policy.UnsupportedHeaders = []string{"x-openai-internal-*"}
	case "gemini":
		policy.UnsupportedHeaders = []string{"x-goog-user-project"}
	}
	return policy
}
