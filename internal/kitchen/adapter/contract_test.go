package adapter

import "testing"

func TestSupervisorCapabilityReports(t *testing.T) {
	tests := []struct {
		agent     string
		qualified bool
		protocol  Protocol
	}{
		{"claude", true, ProtocolAnthropic},
		{"codex", true, ProtocolOpenAIResponses},
		{"gemini", false, ProtocolNativeCLI},
		{"copilot", false, ProtocolOpenAIChat},
		{"pool", true, ProtocolNativeCLI},
		{"future-agent", false, ProtocolNativeCLI},
	}
	for _, test := range tests {
		caps := SupervisorCapabilityReport(test.agent)
		if got := caps.SupervisoryQualified(); got != test.qualified {
			t.Errorf("%s qualified = %v, want %v", test.agent, got, test.qualified)
		}
		if got := Protocol(caps.NativeProtocol); got != test.protocol {
			t.Errorf("%s protocol = %q, want %q", test.agent, got, test.protocol)
		}
	}
}

func TestNormalizeSupportedProtocols(t *testing.T) {
	request := NormalizedRequest{
		Model:        "tier-1",
		Messages:     []NormalizedMessage{{Role: "user", Content: "fix the test"}},
		ResponseJSON: true,
	}
	for _, protocol := range []Protocol{
		ProtocolOpenAIChat,
		ProtocolOpenAIResponses,
		ProtocolAnthropic,
		ProtocolNativeCLI,
	} {
		if _, err := NormalizeRequest(protocol, request); err != nil {
			t.Errorf("NormalizeRequest(%s): %v", protocol, err)
		}
	}
}

func TestUnqualifiedClientsRemainDirectRunOnly(t *testing.T) {
	for _, agent := range []string{"gemini", "copilot", "future-agent"} {
		if !PolicyForAgent(agent).DirectRunOnly {
			t.Errorf("%s should remain direct-run only", agent)
		}
	}
	for _, agent := range []string{"claude", "codex", "pool"} {
		if PolicyForAgent(agent).DirectRunOnly {
			t.Errorf("%s should be supervisor-qualified", agent)
		}
	}
}

func TestSupervisorCapabilityFieldValues(t *testing.T) {
	tests := []struct {
		agent                string
		planning             bool
		structuredDelegation bool
		resultReview         bool
		continuation         bool
		structuredResults    bool
		modelTierAliases     bool
		toolCalls            bool
		nativeProtocol       Protocol
	}{
		{"claude", true, true, true, true, true, true, true, ProtocolAnthropic},
		{"codex", true, true, true, true, true, true, true, ProtocolOpenAIResponses},
		{"gemini", true, true, true, false, true, true, true, ProtocolNativeCLI},
		{"copilot", true, false, true, false, false, true, true, ProtocolOpenAIChat},
		{"pool", true, true, true, true, true, true, true, ProtocolNativeCLI},
		{"future-agent", false, false, false, false, false, false, false, ProtocolNativeCLI},
	}
	for _, test := range tests {
		caps := SupervisorCapabilityReport(test.agent)
		if caps.Planning != test.planning {
			t.Errorf("%s Planning = %v, want %v", test.agent, caps.Planning, test.planning)
		}
		if caps.StructuredDelegation != test.structuredDelegation {
			t.Errorf("%s StructuredDelegation = %v, want %v", test.agent, caps.StructuredDelegation, test.structuredDelegation)
		}
		if caps.ResultReview != test.resultReview {
			t.Errorf("%s ResultReview = %v, want %v", test.agent, caps.ResultReview, test.resultReview)
		}
		if caps.Continuation != test.continuation {
			t.Errorf("%s Continuation = %v, want %v", test.agent, caps.Continuation, test.continuation)
		}
		if caps.StructuredResults != test.structuredResults {
			t.Errorf("%s StructuredResults = %v, want %v", test.agent, caps.StructuredResults, test.structuredResults)
		}
		if caps.ModelTierAliases != test.modelTierAliases {
			t.Errorf("%s ModelTierAliases = %v, want %v", test.agent, caps.ModelTierAliases, test.modelTierAliases)
		}
		if caps.ToolCalls != test.toolCalls {
			t.Errorf("%s ToolCalls = %v, want %v", test.agent, caps.ToolCalls, test.toolCalls)
		}
		if Protocol(caps.NativeProtocol) != test.nativeProtocol {
			t.Errorf("%s NativeProtocol = %q, want %q", test.agent, caps.NativeProtocol, test.nativeProtocol)
		}
	}
}

func TestPolicyForAgentFields(t *testing.T) {
	tests := []struct {
		agent              string
		protocol           Protocol
		unsupportedHeaders []string
		directRunOnly      bool
	}{
		{"claude", ProtocolAnthropic, []string{"anthropic-beta:context-1m"}, false},
		{"codex", ProtocolOpenAIResponses, []string{"x-openai-internal-*"}, false},
		{"gemini", ProtocolNativeCLI, []string{"x-goog-user-project"}, true},
		{"copilot", ProtocolOpenAIChat, []string{}, true},
		{"pool", ProtocolNativeCLI, []string{}, false},
		{"future-agent", ProtocolNativeCLI, []string{}, true},
	}
	for _, test := range tests {
		policy := PolicyForAgent(test.agent)
		if policy.AgentID != test.agent {
			t.Errorf("%s AgentID = %q, want %q", test.agent, policy.AgentID, test.agent)
		}
		if policy.Protocol != test.protocol {
			t.Errorf("%s Protocol = %q, want %q", test.agent, policy.Protocol, test.protocol)
		}
		if policy.UnsupportedHeaders == nil {
			t.Errorf("%s UnsupportedHeaders = nil, want non-nil", test.agent)
		}
		if len(policy.UnsupportedHeaders) != len(test.unsupportedHeaders) {
			t.Errorf("%s UnsupportedHeaders = %v, want %v", test.agent, policy.UnsupportedHeaders, test.unsupportedHeaders)
		}
		for i, header := range test.unsupportedHeaders {
			if policy.UnsupportedHeaders[i] != header {
				t.Errorf("%s UnsupportedHeaders[%d] = %q, want %q", test.agent, i, policy.UnsupportedHeaders[i], header)
			}
		}
		if policy.DirectRunOnly != test.directRunOnly {
			t.Errorf("%s DirectRunOnly = %v, want %v", test.agent, policy.DirectRunOnly, test.directRunOnly)
		}
		if policy.Authentication == "" {
			t.Errorf("%s Authentication is empty", test.agent)
		}
		if policy.CapabilityContract != CapabilityContractVersion {
			t.Errorf("%s CapabilityContract = %q, want %q", test.agent, policy.CapabilityContract, CapabilityContractVersion)
		}
		wantAliases := map[string]string{"fast": "tier-1", "balanced": "tier-2", "best": "tier-3"}
		for tier, want := range wantAliases {
			if got := policy.TierAliases[tier]; got != want {
				t.Errorf("%s TierAliases[%q] = %q, want %q", test.agent, tier, got, want)
			}
		}
	}
}

func TestNormalizeRequestMaxTokensDefault(t *testing.T) {
	request := NormalizedRequest{
		Model:    "tier-1",
		Messages: []NormalizedMessage{{Role: "user", Content: "hello"}},
	}
	got, err := NormalizeRequest(ProtocolAnthropic, request)
	if err != nil {
		t.Fatalf("NormalizeRequest: %v", err)
	}
	if got["max_tokens"] != 4096 {
		t.Errorf("max_tokens = %v, want 4096", got["max_tokens"])
	}
}

func TestNormalizeRequestMaxTokensExplicit(t *testing.T) {
	request := NormalizedRequest{
		Model:     "tier-1",
		Messages:  []NormalizedMessage{{Role: "user", Content: "hello"}},
		MaxTokens: 8192,
	}
	got, err := NormalizeRequest(ProtocolAnthropic, request)
	if err != nil {
		t.Fatalf("NormalizeRequest: %v", err)
	}
	if got["max_tokens"] != 8192 {
		t.Errorf("max_tokens = %v, want 8192", got["max_tokens"])
	}
}

func TestNormalizeRequestDoesNotMutateInput(t *testing.T) {
	request := NormalizedRequest{
		Model:    "tier-1",
		Messages: []NormalizedMessage{{Role: "user", Content: "hello"}},
	}
	if _, err := NormalizeRequest(ProtocolAnthropic, request); err != nil {
		t.Fatalf("NormalizeRequest: %v", err)
	}
	if request.Version != "" {
		t.Errorf("request.Version = %q, want unchanged empty string", request.Version)
	}
}

func TestNormalizeRequestNativeCLIDefaultsVersion(t *testing.T) {
	request := NormalizedRequest{
		Model:    "tier-1",
		Messages: []NormalizedMessage{{Role: "user", Content: "hello"}},
	}
	got, err := NormalizeRequest(ProtocolNativeCLI, request)
	if err != nil {
		t.Fatalf("NormalizeRequest: %v", err)
	}
	wrapped, ok := got["request"].(NormalizedRequest)
	if !ok {
		t.Fatalf("request field has type %T, want NormalizedRequest", got["request"])
	}
	if wrapped.Version != "execution-chat/v1" {
		t.Errorf("wrapped.Version = %q, want %q", wrapped.Version, "execution-chat/v1")
	}
}

func TestNormalizeRequestResponseFormatText(t *testing.T) {
	t.Parallel()

	request := NormalizedRequest{
		Model:        "tier-1",
		Messages:     []NormalizedMessage{{Role: "user", Content: "hello"}},
		ResponseJSON: false,
	}
	got, err := NormalizeRequest(ProtocolOpenAIChat, request)
	if err != nil {
		t.Fatalf("NormalizeRequest: %v", err)
	}
	rf, ok := got["response_format"].(map[string]string)
	if !ok {
		t.Fatalf("response_format has type %T, want map[string]string", got["response_format"])
	}
	if rf["type"] != "text" {
		t.Errorf("response_format.type = %q, want %q", rf["type"], "text")
	}
}

func TestNormalizeRequestUnsupportedProtocolError(t *testing.T) {
	t.Parallel()

	_, err := NormalizeRequest(Protocol("bogus-protocol"), NormalizedRequest{})
	if err == nil {
		t.Fatal("NormalizeRequest with unknown protocol: expected error, got nil")
	}
}
