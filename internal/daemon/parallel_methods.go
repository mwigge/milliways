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
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/parallel"
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

// parallelDispatch handles "parallel.dispatch".
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

	groupID := p.GroupID
	if groupID == "" {
		groupID = uuid.New().String()
	}

	type openResult struct {
		provider string
		handle   int64
		err      error
	}

	results := make([]openResult, len(p.Providers))
	var wg sync.WaitGroup
	for i, prov := range p.Providers {
		i, prov := i, prov
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess, err := s.agents.Open(prov)
			if err != nil {
				results[i] = openResult{provider: prov, err: err}
				return
			}
			results[i] = openResult{provider: prov, handle: int64(sess.Handle)}
		}()
	}
	wg.Wait()

	now := time.Now().UTC()
	var slots []parallelSlotInfo
	var skipped []skippedSlot

	store := s.pantryDB.Parallel()
	if err := store.InsertGroup(pantry.ParallelGroupRecord{
		ID:        groupID,
		Prompt:    p.Prompt,
		Status:    pantry.ParallelStatusRunning,
		CreatedAt: now,
	}); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("insert group: %v", err))
		return
	}

	for _, r := range results {
		if r.err != nil {
			skipped = append(skipped, skippedSlot{Provider: r.provider, Reason: r.err.Error()})
			continue
		}
		if err := store.InsertSlot(pantry.ParallelSlotRecord{
			GroupID:   groupID,
			Handle:    r.handle,
			Provider:  r.provider,
			Status:    pantry.ParallelStatusRunning,
			StartedAt: now,
		}); err != nil {
			slog.Warn("parallel: InsertSlot failed", "provider", r.provider, "err", err)
		}
		slots = append(slots, parallelSlotInfo{Handle: r.handle, Provider: r.provider})
	}

	if len(slots) == 0 {
		writeError(enc, req.ID, ErrInvalidParams, "all providers failed to open")
		return
	}

	writeResult(enc, req.ID, parallelDispatchResult{
		GroupID: groupID,
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
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("group not found: %s", p.GroupID))
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
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("group not found: %s", p.GroupID))
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

// mempalaceClient returns the MemPalace MPClient if configured, or nil.
// Returns nil gracefully when MEMPALACE_MCP_CMD is unset.
func (s *Server) mempalaceClient() parallel.MPClient {
	return nil // wired to real mempalace.Client in a follow-up story
}
