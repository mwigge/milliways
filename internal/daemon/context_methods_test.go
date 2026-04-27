package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/mwigge/milliways/internal/daemon/observability"
)

// newCtxCapturingEncoder returns a json.Encoder that writes into a fresh
// buffer, plus the buffer for assertions.
func newCtxCapturingEncoder() (*json.Encoder, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	return enc, buf
}

// newCtxTestServer returns a Server wired with the bits the context
// dispatchers actually use: streams registry, span ring, the agents
// cache, and an AgentRegistry. We avoid NewServer (which binds a UDS,
// opens metrics.db, and probes runners) — for these tests we only need
// the in-memory pieces.
func newCtxTestServer(t *testing.T) *Server {
	t.Helper()
	s := &Server{
		streams: NewStreamRegistry(),
		spans:   observability.NewRing(100),
		agentsCache: []AgentInfo{
			{ID: "claude", Available: true, AuthStatus: "authed", Model: "claude-3-5-sonnet"},
			{ID: "codex", Available: true, AuthStatus: "authed", Model: "gpt-4o"},
			{ID: "minimax", Available: false, AuthStatus: "unknown"},
			{ID: "copilot", Available: true, AuthStatus: "authed"},
		},
	}
	s.bgCtx, s.bgCancel = context.WithCancel(context.Background())
	t.Cleanup(s.bgCancel)
	s.agents = NewAgentRegistry(s)
	return s
}

// TestContextGet_KnownAgentReturnsSnapshot covers the happy path: a
// known agent_id from the runner roster returns a ContextSnapshot with
// model populated from the cache and zero-valued tokens / empty arrays.
func TestContextGet_KnownAgentReturnsSnapshot(t *testing.T) {
	t.Parallel()
	s := newCtxTestServer(t)

	req := &Request{
		Method: "context.get",
		Params: json.RawMessage(`{"agent_id":"claude"}`),
		ID:     json.RawMessage(`1`),
	}
	enc, captured := newCtxCapturingEncoder()
	s.contextGet(enc, req)

	var resp struct {
		Result ContextSnapshot `json:"result"`
		Error  *Error          `json:"error,omitempty"`
	}
	if err := json.Unmarshal(captured.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, captured.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Result.AgentID != "claude" {
		t.Errorf("agent_id = %q, want claude", resp.Result.AgentID)
	}
	if resp.Result.Model != "claude-3-5-sonnet" {
		t.Errorf("model = %q, want claude-3-5-sonnet", resp.Result.Model)
	}
	if resp.Result.Tokens.Input != 0 || resp.Result.Tokens.Output != 0 || resp.Result.Tokens.Cached != 0 {
		t.Errorf("tokens = %+v, want all zero (Phase 5 stub)", resp.Result.Tokens)
	}
	if resp.Result.Tools == nil {
		t.Errorf("tools is nil; schema requires array")
	}
	if resp.Result.MCPServers == nil {
		t.Errorf("mcp_servers is nil; schema requires array")
	}
	if resp.Result.FilesInContext == nil {
		t.Errorf("files_in_context is nil; schema requires array")
	}
}

// TestContextGet_UnknownAgentErrors verifies the negative path: an
// agent_id outside the runner roster returns an InvalidParams error
// rather than a malformed snapshot.
func TestContextGet_UnknownAgentErrors(t *testing.T) {
	t.Parallel()
	s := newCtxTestServer(t)

	req := &Request{
		Method: "context.get",
		Params: json.RawMessage(`{"agent_id":"frogs"}`),
		ID:     json.RawMessage(`2`),
	}
	enc, captured := newCtxCapturingEncoder()
	s.contextGet(enc, req)

	var resp struct {
		Error *Error `json:"error,omitempty"`
	}
	if err := json.Unmarshal(captured.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, captured.String())
	}
	if resp.Error == nil {
		t.Fatalf("expected error for unknown agent_id, got success")
	}
	if resp.Error.Code != ErrInvalidParams {
		t.Errorf("error code = %d, want %d (ErrInvalidParams)", resp.Error.Code, ErrInvalidParams)
	}
}

// TestContextGetAll_TotalsSumPerAgentFields verifies that the totals
// header is the per-agent column sum and ActiveAgents counts only agents
// with an open session. Tokens/cost are zero across the board until
// runners wire metrics, but the active-agents field is wired today.
func TestContextGetAll_TotalsSumPerAgentFields(t *testing.T) {
	t.Parallel()
	s := newCtxTestServer(t)
	// Simulate one open session for `claude` so ActiveAgents = 1.
	if _, err := s.agents.Open("claude"); err != nil {
		t.Fatalf("open claude session: %v", err)
	}

	req := &Request{
		Method: "context.get_all",
		Params: json.RawMessage(`{}`),
		ID:     json.RawMessage(`3`),
	}
	enc, captured := newCtxCapturingEncoder()
	s.contextGetAll(enc, req)

	var resp struct {
		Result AggregateContext `json:"result"`
		Error  *Error           `json:"error,omitempty"`
	}
	if err := json.Unmarshal(captured.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v (raw=%s)", err, captured.String())
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if got, want := len(resp.Result.Agents), len(knownContextAgents); got != want {
		t.Errorf("agents count = %d, want %d", got, want)
	}

	// Totals MUST equal the per-agent column sum. With everything
	// zero-stubbed today this is a wiring check: any future non-zero
	// per-agent field must propagate to totals.
	var sumIn, sumOut, sumCached, sumErr int
	var sumCost float64
	var active int
	for _, a := range resp.Result.Agents {
		sumIn += a.Tokens.Input
		sumOut += a.Tokens.Output
		sumCached += a.Tokens.Cached
		sumCost += a.CostUSD
		sumErr += a.Errors5m
		if a.SessionID != "" {
			active++
		}
	}
	totals := resp.Result.Totals
	if totals.TokensIn != sumIn {
		t.Errorf("totals.tokens_in = %d, want %d (sum of per-agent input)", totals.TokensIn, sumIn)
	}
	if totals.TokensOut != sumOut {
		t.Errorf("totals.tokens_out = %d, want %d", totals.TokensOut, sumOut)
	}
	if totals.Cached != sumCached {
		t.Errorf("totals.cached = %d, want %d", totals.Cached, sumCached)
	}
	if totals.CostUSD != sumCost {
		t.Errorf("totals.cost_usd = %f, want %f", totals.CostUSD, sumCost)
	}
	if totals.Errors5m != sumErr {
		t.Errorf("totals.errors_5m = %d, want %d", totals.Errors5m, sumErr)
	}
	if totals.ActiveAgents != active {
		t.Errorf("totals.active_agents = %d, want %d", totals.ActiveAgents, active)
	}
	if totals.ActiveAgents != 1 {
		t.Errorf("expected exactly 1 active agent (claude session), got %d", totals.ActiveAgents)
	}
	if resp.Result.RecentRouting == nil {
		t.Errorf("recent_routing is nil; schema requires array")
	}
}
