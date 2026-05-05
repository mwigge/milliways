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

// Package parallel implements multi-provider parallel session dispatch,
// consensus aggregation, and MemPalace-backed shared memory for milliways.
package parallel

import (
	"context"
	"time"
)

// SlotStatus mirrors pantry.ParallelStatus; duplicated here so callers of
// this package don't import pantry directly.
type SlotStatus string

const (
	SlotRunning     SlotStatus = "running"
	SlotDone        SlotStatus = "done"
	SlotError       SlotStatus = "error"
	SlotInterrupted SlotStatus = "interrupted"
)

// SlotRecord describes one provider slot in a parallel group.
type SlotRecord struct {
	Handle      int64
	Provider    string
	Status      SlotStatus
	StartedAt   time.Time
	CompletedAt time.Time
	TokensIn    int
	TokensOut   int
}

// Group is a parallel dispatch group returned to callers.
type Group struct {
	ID          string
	Prompt      string
	Status      SlotStatus
	Slots       []SlotRecord
	CreatedAt   time.Time
	CompletedAt time.Time
}

// DispatchRequest is the input to Dispatch.
type DispatchRequest struct {
	Prompt    string
	Providers []string // if empty, caller should populate from pool config
	GroupID   string   // optional; generated if empty
}

// DispatchResult is returned immediately by Dispatch.
type DispatchResult struct {
	GroupID string
	Slots   []SlotRecord
	Skipped []SkippedProvider
}

// SkippedProvider records a provider that could not be opened.
type SkippedProvider struct {
	Provider string
	Reason   string
}

// AgentOpener opens a daemon session for a given provider and returns the
// session handle. Implemented by the daemon RPC client in the real path and
// by a stub in tests.
type AgentOpener interface {
	OpenSession(ctx context.Context, providerID string) (int64, error)
}

// GroupStore persists parallel group state across daemon restarts.
// Implemented by pantry.ParallelStore; extracted as an interface for testing.
type GroupStore interface {
	InsertGroup(g interface{ groupRecord() }) error
	InsertSlot(s interface{ slotRecord() }) error
	UpdateSlotStatus(handle int64, status string, tokensIn, tokensOut int) error
	GetGroup(id string) (interface{}, error)
	ListGroups(n int) ([]interface{}, error)
	MarkInterruptedSlots() error
}

// MPClient is the subset of the MemPalace client used by the parallel package.
type MPClient interface {
	KGQuery(ctx context.Context, subjectPrefix, predicate string, filters map[string]string) ([]KGTriple, error)
	KGAdd(ctx context.Context, subject, predicate, object string, props map[string]string) error
}

// KGTriple is one triple from a MemPalace kg_query result.
type KGTriple struct {
	Subject    string
	Predicate  string
	Object     string
	Properties map[string]string
}

// Finding is one structured security/review finding extracted from an agent response.
type Finding struct {
	File        string
	Description string
}

// QuotaSummary holds per-provider quota information for the header bar.
type QuotaSummary struct {
	UsedToday int
	LimitDay  int
}
