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
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// parallelDispatchParams are the params for the parallel.dispatch method.
type parallelDispatchParams struct {
	Prompt    string   `json:"prompt"`
	Providers []string `json:"providers,omitempty"`
	GroupID   string   `json:"group_id,omitempty"`
}

// parallelSlotInfo is one slot in the parallel.dispatch result.
type parallelSlotInfo struct {
	Handle   int64  `json:"handle"`
	Provider string `json:"provider"`
}

// skippedSlot records a provider that could not be opened.
type skippedSlot struct {
	Provider string `json:"provider"`
	Reason   string `json:"reason"`
}

// parallelDispatchResult is the result of a parallel.dispatch call.
type parallelDispatchResult struct {
	GroupID string             `json:"group_id"`
	Slots   []parallelSlotInfo `json:"slots"`
	Skipped []skippedSlot      `json:"skipped,omitempty"`
}

// parallelDispatch handles the "parallel.dispatch" JSON-RPC method.
// It opens one agent session per requested provider concurrently, stores the
// resulting group in the in-memory parallel store, and returns the group_id
// plus per-slot handles. If all providers fail it returns a JSON-RPC error.
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

	var slots []parallelSlotInfo
	var skipped []skippedSlot
	var slotRecords []parallelSlotRecord

	for _, r := range results {
		if r.err != nil {
			skipped = append(skipped, skippedSlot{
				Provider: r.provider,
				Reason:   r.err.Error(),
			})
			continue
		}
		slots = append(slots, parallelSlotInfo{Handle: r.handle, Provider: r.provider})
		slotRecords = append(slotRecords, parallelSlotRecord{
			Handle:    r.handle,
			Provider:  r.provider,
			Status:    "running",
			StartedAt: time.Now().UTC(),
		})
	}

	if len(slots) == 0 {
		writeError(enc, req.ID, ErrInvalidParams, "all providers failed to open")
		return
	}

	grp := &parallelGroupRecord{
		GroupID:   groupID,
		Prompt:    p.Prompt,
		Status:    "running",
		CreatedAt: time.Now().UTC(),
		Slots:     slotRecords,
	}
	s.parallel.put(grp)

	writeResult(enc, req.ID, parallelDispatchResult{
		GroupID: groupID,
		Slots:   slots,
		Skipped: skipped,
	})
}

// groupStatusResult is the response shape for group.status.
type groupStatusResult struct {
	GroupID     string             `json:"group_id"`
	Prompt      string             `json:"prompt"`
	Status      string             `json:"status"`
	CreatedAt   string             `json:"created_at"`
	CompletedAt string             `json:"completed_at,omitempty"`
	Slots       []groupSlotStatus  `json:"slots"`
}

// groupSlotStatus is the per-slot status in a group.status response.
type groupSlotStatus struct {
	Handle      int64  `json:"handle"`
	Provider    string `json:"provider"`
	Status      string `json:"status"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
	TokensIn    int    `json:"tokens_in"`
	TokensOut   int    `json:"tokens_out"`
}

// groupStatus handles the "group.status" JSON-RPC method.
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

	grp, err := s.parallel.get(p.GroupID)
	if err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("group not found: %s", p.GroupID))
			return
		}
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}

	slots := make([]groupSlotStatus, 0, len(grp.Slots))
	for _, sl := range grp.Slots {
		gs := groupSlotStatus{
			Handle:    sl.Handle,
			Provider:  sl.Provider,
			Status:    sl.Status,
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
		GroupID:   grp.GroupID,
		Prompt:    grp.Prompt,
		Status:    grp.Status,
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

// groupListResult is the response shape for group.list.
type groupListResult struct {
	Groups []groupSummary `json:"groups"`
}

// groupList handles the "group.list" JSON-RPC method.
// Returns up to 20 recent groups, newest first.
func (s *Server) groupList(enc *json.Encoder, req *Request) {
	const maxList = 20
	records := s.parallel.list(maxList)

	summaries := make([]groupSummary, 0, len(records))
	for _, g := range records {
		summaries = append(summaries, groupSummary{
			GroupID:   g.GroupID,
			Prompt:    g.Prompt,
			Status:    g.Status,
			CreatedAt: g.CreatedAt.UTC().Format(time.RFC3339),
			SlotCount: len(g.Slots),
		})
	}

	writeResult(enc, req.ID, groupListResult{Groups: summaries})
}

// consensusAggregateResult is the response shape for consensus.aggregate.
type consensusAggregateResult struct {
	Summary string `json:"summary"`
}

// consensusAggregate handles the "consensus.aggregate" JSON-RPC method.
// Until internal/parallel/consensus.go is implemented by Agent B, this
// returns a stub summary.
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

	// Verify the group exists.
	if _, err := s.parallel.get(p.GroupID); err != nil {
		if errors.Is(err, ErrGroupNotFound) {
			writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("group not found: %s", p.GroupID))
			return
		}
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}

	// Stub: consensus aggregation is not yet available (Agent B's work).
	writeResult(enc, req.ID, consensusAggregateResult{
		Summary: "[consensus not yet available]",
	})
}
