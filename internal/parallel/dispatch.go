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

package parallel

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mwigge/milliways/internal/pantry"
)

// errAllUnavailable is returned when every provider in the request fails to open.
var errAllUnavailable = errors.New("all providers unavailable")

// filePathRe matches the first Go file path of the form dir/file.go in a string.
var filePathRe = regexp.MustCompile(`([\w./:-]+/[\w./:-]+\.go)`)

// Dispatch opens sessions for all requested providers concurrently, persists
// the group and per-slot records, and returns immediately without waiting for
// the sessions to complete.
//
// If a provider cannot be opened its error is captured in DispatchResult.Skipped
// rather than aborting the whole dispatch. If all providers fail, errAllUnavailable
// is returned.
func Dispatch(ctx context.Context, req DispatchRequest, opener AgentOpener, store *pantry.ParallelStore, mp MPClient) (DispatchResult, error) {
	groupID := req.GroupID
	if groupID == "" {
		groupID = uuid.NewString()
	}

	type openResult struct {
		provider string
		handle   int64
		err      error
	}

	results := make([]openResult, len(req.Providers))
	var wg sync.WaitGroup
	wg.Add(len(req.Providers))
	for i, p := range req.Providers {
		i, p := i, p
		go func() {
			defer wg.Done()
			handle, err := opener.OpenSession(ctx, p)
			results[i] = openResult{provider: p, handle: handle, err: err}
		}()
	}
	wg.Wait()

	var slots []SlotRecord
	var skipped []SkippedProvider
	now := time.Now().UTC()

	for _, r := range results {
		if r.err != nil {
			skipped = append(skipped, SkippedProvider{
				Provider: r.provider,
				Reason:   r.err.Error(),
			})
			continue
		}
		slots = append(slots, SlotRecord{
			Handle:    r.handle,
			Provider:  r.provider,
			Status:    SlotRunning,
			StartedAt: now,
		})
	}

	if len(slots) == 0 {
		return DispatchResult{}, errAllUnavailable
	}

	if err := store.InsertGroup(pantry.ParallelGroupRecord{
		ID:        groupID,
		Prompt:    req.Prompt,
		Status:    pantry.ParallelStatusRunning,
		CreatedAt: now,
	}); err != nil {
		return DispatchResult{}, fmt.Errorf("inserting group: %w", err)
	}

	for i := range slots {
		if err := store.InsertSlot(pantry.ParallelSlotRecord{
			GroupID:   groupID,
			Handle:    slots[i].Handle,
			Provider:  slots[i].Provider,
			Status:    pantry.ParallelStatusRunning,
			StartedAt: now,
		}); err != nil {
			return DispatchResult{}, fmt.Errorf("inserting slot for %s: %w", slots[i].Provider, err)
		}
	}

	return DispatchResult{
		GroupID: groupID,
		Slots:   slots,
		Skipped: skipped,
	}, nil
}

// InjectBaseline extracts a Go file path from prompt, queries MemPalace for
// prior findings on that file, and returns a formatted context block.
//
// Returns empty string when mp is nil, no file path is found, or no findings exist.
func InjectBaseline(ctx context.Context, prompt string, mp MPClient) string {
	if mp == nil {
		return ""
	}
	match := filePathRe.FindStringSubmatch(prompt)
	if len(match) == 0 {
		return ""
	}
	path := match[1]

	triples, err := mp.KGQuery(ctx, "file:"+path, "has_finding", nil)
	if err != nil || len(triples) == 0 {
		return ""
	}

	sort.Slice(triples, func(i, j int) bool {
		return triples[i].Properties["ts"] > triples[j].Properties["ts"]
	})

	total := len(triples)
	if len(triples) > 20 {
		triples = triples[:20]
	}

	var sb strings.Builder
	sb.WriteString("[prior findings from mempalace]\n")
	for _, t := range triples {
		source := t.Properties["source"]
		ts := t.Properties["ts"]
		sb.WriteString(fmt.Sprintf("%s: %s (source: %s, %s)\n", path, t.Object, source, ts))
	}
	if total > 20 {
		sb.WriteString(fmt.Sprintf("[truncated — showing 20 of %d prior findings]\n", total))
	}
	return sb.String()
}

// RecoverInterrupted transitions all running slots to interrupted status.
// Called on daemon startup to recover from unclean shutdowns.
func RecoverInterrupted(store *pantry.ParallelStore) error {
	if err := store.MarkInterruptedSlots(); err != nil {
		return fmt.Errorf("recovering interrupted slots: %w", err)
	}
	return nil
}
