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
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/mwigge/milliways/internal/mempalace"
	"github.com/mwigge/milliways/internal/parallel"
	"github.com/mwigge/milliways/internal/security"
)

// parallelDispatchParams are the JSON-RPC params for parallel.dispatch.
type parallelDispatchParams struct {
	Prompt    string   `json:"prompt"`
	Providers []string `json:"providers,omitempty"`
	GroupID   string   `json:"group_id,omitempty"`
}

// parallelSlotInfo is one slot in the parallel.dispatch response.
type parallelSlotInfo struct {
	Handle   int64  `json:"handle"`
	Provider string `json:"provider"`
}

// skippedSlot records a provider that could not be opened.
type skippedSlot struct {
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
}

// parallelDispatchResult is the parallel.dispatch response.
type parallelDispatchResult struct {
	GroupID string             `json:"group_id"`
	Slots   []parallelSlotInfo `json:"slots"`
	Skipped []skippedSlot      `json:"skipped,omitempty"`
}

// daemonAgentOpener adapts *AgentRegistry to parallel.AgentOpener.
type daemonAgentOpener struct{ r *AgentRegistry }

func (d *daemonAgentOpener) OpenSession(_ context.Context, providerID string) (int64, error) {
	sess, err := d.r.Open(providerID)
	if err != nil {
		return 0, err
	}
	return int64(sess.Handle), nil
}

// parallelDispatch handles "parallel.dispatch".
// It delegates to internal/parallel.Dispatch() so MemPalace baseline
// injection and CodeGraph context injection run on every dispatch.
// After the sessions are open it sends the preamble (if any) followed
// by the user prompt to each slot so the agents start immediately.
func (s *Server) parallelDispatch(enc *json.Encoder, req *Request) {
	var p parallelDispatchParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	if p.Prompt == "" {
		writeError(enc, req.ID, ErrInvalidParams, "prompt is required")
		return
	}
	if len(p.Providers) == 0 {
		writeError(enc, req.ID, ErrInvalidParams, "providers must not be empty")
		return
	}

	result, err := parallel.Dispatch(
		context.Background(),
		parallel.DispatchRequest{
			Prompt:    p.Prompt,
			Providers: p.Providers,
			GroupID:   p.GroupID,
		},
		&daemonAgentOpener{r: s.agents},
		s.pantryDB.Parallel(),
		s.mempalaceClient(),
		nil, // CodeGraph client — wired when CG index is available
	)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}

	// Prepend security context block to the preamble (generated once for the group).
	if s.pantryDB != nil {
		secFindings, _ := s.pantryDB.Security().ListActive([]string{"CRITICAL", "HIGH"})
		if len(secFindings) > 0 {
			result.ContextPreamble = security.BuildContextBlock(secFindings, security.DefaultTokenCap) + result.ContextPreamble
		}
	}

	// Send preamble + user prompt to every open session. Fire-and-forget:
	// errors are logged but do not fail the dispatch — the session stays
	// open and the user can see what happened in the attach pane.
	for _, slot := range result.Slots {
		sess, ok := s.agents.Get(AgentHandle(slot.Handle))
		if !ok {
			slog.Warn("parallel: handle missing after dispatch", "handle", slot.Handle, "provider", slot.Provider)
			continue
		}
		if result.ContextPreamble != "" {
			if err := sess.Send([]byte(result.ContextPreamble)); err != nil {
				slog.Warn("parallel: preamble send failed", "handle", slot.Handle, "err", err)
			}
		}
		if err := sess.Send([]byte(p.Prompt)); err != nil {
			slog.Warn("parallel: prompt send failed", "handle", slot.Handle, "err", err)
		}
	}

	// Map to RPC response types.
	slots := make([]parallelSlotInfo, len(result.Slots))
	for i, s := range result.Slots {
		slots[i] = parallelSlotInfo{Handle: s.Handle, Provider: s.Provider}
	}
	skipped := make([]skippedSlot, len(result.Skipped))
	for i, sk := range result.Skipped {
		skipped[i] = skippedSlot{Provider: sk.Provider, Reason: sk.Reason}
	}

	writeResult(enc, req.ID, parallelDispatchResult{
		GroupID: result.GroupID,
		Slots:   slots,
		Skipped: skipped,
	})
}

// groupStatusResult is the group.status response.
type groupStatusResult struct {
	GroupID     string            `json:"group_id"`
	Prompt      string            `json:"prompt"`
	Status      string            `json:"status"`
	CreatedAt   string            `json:"created_at"`
	CompletedAt string            `json:"completed_at,omitempty"`
	Slots       []groupSlotStatus `json:"slots"`
}

// groupSlotStatus is one slot in the group.status response.
type groupSlotStatus struct {
	Handle      int64  `json:"handle"`
	Provider    string `json:"provider"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	TokensIn    int    `json:"tokens_in"`
	TokensOut   int    `json:"tokens_out"`
}

// groupStatus handles "group.status".
func (s *Server) groupStatus(enc *json.Encoder, req *Request) {
	var p struct {
		GroupID string `json:"group_id"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	if p.GroupID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "group_id is required")
		return
	}

	grp, err := s.pantryDB.Parallel().GetGroup(p.GroupID)
	if err != nil {
		writeError(enc, req.ID, ErrNotFound, fmt.Sprintf("group not found: %s", p.GroupID))
		return
	}

	slots := make([]groupSlotStatus, 0, len(grp.Slots))
	for _, sl := range grp.Slots {
		gs := groupSlotStatus{
			Handle:    sl.Handle,
			Provider:  sl.Provider,
			Status:    string(sl.Status),
			TokensIn:  sl.TokensIn,
			TokensOut: sl.TokensOut,
		}
		if !sl.StartedAt.IsZero() {
			gs.StartedAt = sl.StartedAt.UTC().Format(time.RFC3339)
		}
		if !sl.CompletedAt.IsZero() {
			gs.CompletedAt = sl.CompletedAt.UTC().Format(time.RFC3339)
		}
		slots = append(slots, gs)
	}

	result := groupStatusResult{
		GroupID:   grp.ID,
		Prompt:    grp.Prompt,
		Status:    string(grp.Status),
		CreatedAt: grp.CreatedAt.UTC().Format(time.RFC3339),
		Slots:     slots,
	}
	if !grp.CompletedAt.IsZero() {
		result.CompletedAt = grp.CompletedAt.UTC().Format(time.RFC3339)
	}
	writeResult(enc, req.ID, result)
}

// groupSummary is one entry in the group.list response.
type groupSummary struct {
	GroupID   string `json:"group_id"`
	Prompt    string `json:"prompt"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
	SlotCount int    `json:"slot_count"`
}

// groupListResult is the group.list response.
type groupListResult struct {
	Groups []groupSummary `json:"groups"`
}

// groupList handles "group.list".
func (s *Server) groupList(enc *json.Encoder, req *Request) {
	records, err := s.pantryDB.Parallel().ListGroups(20)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("list groups: %v", err))
		return
	}

	summaries := make([]groupSummary, 0, len(records))
	for _, g := range records {
		summaries = append(summaries, groupSummary{
			GroupID:   g.ID,
			Prompt:    g.Prompt,
			Status:    string(g.Status),
			CreatedAt: g.CreatedAt.UTC().Format(time.RFC3339),
			SlotCount: len(g.Slots),
		})
	}
	writeResult(enc, req.ID, groupListResult{Groups: summaries})
}

// consensusAggregateResult is the consensus.aggregate response.
type consensusAggregateResult struct {
	Summary string `json:"summary"`
}

// consensusAggregate handles "consensus.aggregate".
// It calls the real parallel.Aggregate() when MemPalace is available,
// falling back to a structured summary from pantry findings only.
func (s *Server) consensusAggregate(enc *json.Encoder, req *Request) {
	var p struct {
		GroupID string `json:"group_id"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
			return
		}
	}
	if p.GroupID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "group_id is required")
		return
	}

	// Verify group exists.
	if _, err := s.pantryDB.Parallel().GetGroup(p.GroupID); err != nil {
		writeError(enc, req.ID, ErrNotFound, fmt.Sprintf("group not found: %s", p.GroupID))
		return
	}

	// Run consensus aggregation. MemPalace client is nil when not configured;
	// Aggregate() handles that gracefully (returns empty findings, no error).
	agg := parallel.ConsensusAggregator{MP: s.mempalaceClient()}
	summary, err := agg.Aggregate(context.Background(), p.GroupID)
	if err != nil {
		slog.Warn("consensus.aggregate failed", "group", p.GroupID, "err", err)
		writeResult(enc, req.ID, consensusAggregateResult{
			Summary: fmt.Sprintf("[consensus error: %v]", err),
		})
		return
	}

	writeResult(enc, req.ID, consensusAggregateResult{
		Summary: parallel.RenderSummary(summary),
	})
}

// mempalaceClient returns a parallel.MPClient backed by the MemPalace MCP
// server when MEMPALACE_MCP_CMD is set. Returns nil gracefully when unset.
func (s *Server) mempalaceClient() parallel.MPClient {
	c, err := mempalace.NewClientFromEnv()
	if err != nil {
		return nil
	}
	return &mempalaceParallelAdapter{c: c}
}

// mempalaceParallelAdapter bridges *mempalace.Client to parallel.MPClient.
type mempalaceParallelAdapter struct {
	c *mempalace.Client
}

func (a *mempalaceParallelAdapter) KGQuery(ctx context.Context, subjectPrefix, predicate string, filters map[string]string) ([]parallel.KGTriple, error) {
	results, err := a.c.Search(ctx, subjectPrefix, 20)
	if err != nil {
		return nil, err
	}
	triples := make([]parallel.KGTriple, 0, len(results))
	for _, r := range results {
		triples = append(triples, parallel.KGTriple{
			Subject:    r.DrawerID,
			Predicate:  predicate,
			Object:     r.Content,
			Properties: map[string]string{"source": r.Wing, "ts": ""},
		})
	}
	return triples, nil
}

func (a *mempalaceParallelAdapter) KGAdd(ctx context.Context, subject, predicate, object string, props map[string]string) error {
	wing := props["source"]
	if wing == "" {
		wing = "parallel"
	}
	drawerID := predicate + ":" + truncate(object, 80)
	return a.c.Write(ctx, wing, subject, drawerID, object)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
