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

package daemon

import (
	"encoding/json"
	"fmt"
	"time"
)

// JSON-RPC method handlers for the context.* surface. The daemon composes
// per-agent and aggregated context snapshots from the AgentRegistry plus the
// cached runner probes; runners will progressively populate the token / tool
// / MCP / files / cost fields as each runner lifts (Phase 6+).
//
// The /context cockpit lands as a pane (not a wezterm overlay) — the Rust
// side AgentDomain special-cases reserved agent_ids `_context` and
// `_context_all`, spawning `milliwaysctl context-render` instead of the
// regular bridge. The render subprocess subscribes here and prints text
// frames at 2 Hz.

// ContextTokens mirrors `#/$defs/ContextSnapshot/properties/tokens` in
// proto/milliways.json. All fields are required by the schema.
type ContextTokens struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Cached int `json:"cached"`
}

// ContextTool mirrors one element of ContextSnapshot.tools.
type ContextTool struct {
	Name       string `json:"name"`
	LastUsedTS string `json:"last_used_ts,omitempty"`
}

// ContextMCPServer mirrors one element of ContextSnapshot.mcp_servers.
type ContextMCPServer struct {
	Name      string `json:"name"`
	Connected bool   `json:"connected"`
	ToolCount int    `json:"tool_count"`
}

// ContextFile mirrors one element of ContextSnapshot.files_in_context.
type ContextFile struct {
	Path  string `json:"path"`
	Bytes int    `json:"bytes"`
}

// ContextSnapshot mirrors `#/$defs/ContextSnapshot`.
//
// Tokens, tools, MCP servers, files and cost are populated incrementally as
// runners wire metrics. Until then they are zero / empty arrays. agent_id is
// always set; absent fields are omitted from JSON via `omitempty` where the
// schema makes them optional.
type ContextSnapshot struct {
	AgentID             string             `json:"agent_id"`
	Model               string             `json:"model,omitempty"`
	SessionID           string             `json:"session_id,omitempty"`
	SystemPromptSummary string             `json:"system_prompt_summary,omitempty"`
	Turn                int                `json:"turn,omitempty"`
	Tokens              ContextTokens      `json:"tokens"`
	Tools               []ContextTool      `json:"tools"`
	MCPServers          []ContextMCPServer `json:"mcp_servers"`
	FilesInContext      []ContextFile      `json:"files_in_context"`
	CostUSD             float64            `json:"cost_usd,omitempty"`
	Errors5m            int                `json:"errors_5m,omitempty"`

	// UptimeS is a milliways extension (not in schema) populated by the
	// daemon for the renderer's "uptime" line.
	UptimeS float64 `json:"uptime_s,omitempty"`
}

// AggregateTotals mirrors AggregateContext.totals.
type AggregateTotals struct {
	TokensIn     int     `json:"tokens_in"`
	TokensOut    int     `json:"tokens_out"`
	Cached       int     `json:"cached"`
	CostUSD      float64 `json:"cost_usd"`
	ActiveAgents int     `json:"active_agents"`
	Errors5m     int     `json:"errors_5m"`
}

// AggregateContext mirrors `#/$defs/AggregateContext`.
type AggregateContext struct {
	Agents        []ContextSnapshot `json:"agents"`
	Totals        AggregateTotals   `json:"totals"`
	RecentRouting []RoutingDecision `json:"recent_routing"`
}

// known agent ids served by context.get_all even when no session is open.
// Mirrors the runner set lifted in Phase 4 (claude, codex, minimax, copilot).
var knownContextAgents = []string{"claude", "codex", "minimax", "copilot"}

type contextGetParams struct {
	AgentID string `json:"agent_id"`
}

// contextGet handles `context.get({agent_id})`. Returns a ContextSnapshot for
// the requested agent, drawing the model and auth_status hint from the
// cached runner probes. Returns ErrInvalidParams if agent_id is unknown.
func (s *Server) contextGet(enc *json.Encoder, req *Request) {
	var p contextGetParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, "invalid context.get params: "+err.Error())
			return
		}
	}
	if p.AgentID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "context.get requires agent_id")
		return
	}
	if !s.knownAgent(p.AgentID) {
		writeError(enc, req.ID, ErrInvalidParams, "unknown agent_id: "+p.AgentID)
		return
	}
	writeResult(enc, req.ID, s.buildContextSnapshot(p.AgentID))
}

// contextGetAll handles `context.get_all()`. Returns one snapshot per
// registered runner agent plus zero totals + empty recent_routing until
// runners wire metrics and the sommelier wires routing history.
func (s *Server) contextGetAll(enc *json.Encoder, req *Request) {
	writeResult(enc, req.ID, s.buildAggregateContext())
}

type contextSubscribeParams struct {
	AgentID string `json:"agent_id,omitempty"`
}

// contextSubscribe handles `context.subscribe({agent_id?})`. Allocates a
// stream, starts a 2 Hz pusher, and returns the stream id. If agent_id is
// "_all" or empty the pusher emits AggregateContext frames; otherwise it
// emits per-agent ContextSnapshot frames.
func (s *Server) contextSubscribe(enc *json.Encoder, req *Request) {
	var p contextSubscribeParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, "invalid context.subscribe params: "+err.Error())
			return
		}
	}
	all := p.AgentID == "" || p.AgentID == "_all"
	if !all && !s.knownAgent(p.AgentID) {
		writeError(enc, req.ID, ErrInvalidParams, "unknown agent_id: "+p.AgentID)
		return
	}

	stream := s.streams.Allocate()
	writeResult(enc, req.ID, map[string]any{
		"stream_id":     stream.ID,
		"output_offset": int64(0),
	})

	// Push an initial snapshot so the renderer doesn't have to wait for the
	// first 2Hz tick (matches status.subscribe behaviour).
	go s.runContextPusher(stream, p.AgentID, all)
}

// runContextPusher emits a snapshot every 500ms until the stream is closed.
// One goroutine per subscriber; cheap because the snapshot construction is
// O(n_agents) over an in-memory cache.
func (s *Server) runContextPusher(stream *Stream, agentID string, all bool) {
	push := func() {
		if all {
			stream.Push(map[string]any{"t": "data", "snapshot": s.buildAggregateContext()})
		} else {
			stream.Push(map[string]any{"t": "data", "snapshot": s.buildContextSnapshot(agentID)})
		}
	}
	push()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.bgCtx.Done():
			return
		case <-ticker.C:
			if streamIsClosed(stream) {
				return
			}
			push()
		}
	}
}

// streamIsClosed returns true if the Stream has been Closed (e.g. by the
// sidecar disconnecting and the registry removing it) or never attached.
func streamIsClosed(s *Stream) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// knownAgent reports whether agent_id is in the daemon's runner roster.
func (s *Server) knownAgent(agentID string) bool {
	for _, info := range s.agentsCache {
		if info.ID == agentID {
			return true
		}
	}
	for _, id := range knownContextAgents {
		if id == agentID {
			return true
		}
	}
	return false
}

// buildContextSnapshot composes the per-agent snapshot. Tokens / tools /
// mcp_servers / files_in_context / cost are zero / empty until runners wire
// metrics. Model is pulled from the cached runner probes when available.
func (s *Server) buildContextSnapshot(agentID string) ContextSnapshot {
	snap := ContextSnapshot{
		AgentID:        agentID,
		Tokens:         ContextTokens{},
		Tools:          []ContextTool{},
		MCPServers:     []ContextMCPServer{},
		FilesInContext: []ContextFile{},
		UptimeS:        time.Since(startTime).Seconds(),
	}
	for _, info := range s.agentsCache {
		if info.ID == agentID {
			snap.Model = info.Model
			break
		}
	}
	if sess := s.findSessionByAgent(agentID); sess != nil {
		snap.SessionID = fmt.Sprintf("h-%d", sess.Handle)
	}
	return snap
}

// findSessionByAgent returns one open session for agent_id, or nil. Stable
// iteration order is not required — context only needs "is there a session?
// what's its handle?" — so the first match wins.
func (s *Server) findSessionByAgent(agentID string) *AgentSession {
	if s.agents == nil {
		return nil
	}
	s.agents.mu.Lock()
	defer s.agents.mu.Unlock()
	for _, sess := range s.agents.sessions {
		if sess.AgentID == agentID {
			return sess
		}
	}
	return nil
}

// buildAggregateContext composes the aggregated snapshot. Per-agent frames
// are summed into Totals; ActiveAgents counts agents with an open session.
func (s *Server) buildAggregateContext() AggregateContext {
	agents := make([]ContextSnapshot, 0, len(knownContextAgents))
	totals := AggregateTotals{}
	for _, id := range knownContextAgents {
		snap := s.buildContextSnapshot(id)
		agents = append(agents, snap)
		totals.TokensIn += snap.Tokens.Input
		totals.TokensOut += snap.Tokens.Output
		totals.Cached += snap.Tokens.Cached
		totals.CostUSD += snap.CostUSD
		totals.Errors5m += snap.Errors5m
		if snap.SessionID != "" {
			totals.ActiveAgents++
		}
	}
	return AggregateContext{
		Agents:        agents,
		Totals:        totals,
		RecentRouting: []RoutingDecision{},
	}
}
