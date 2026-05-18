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
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
)

var toolApprovalWaiters = struct {
	sync.Mutex
	chans map[int64][]chan string
}{chans: map[int64][]chan string{}}

const defaultToolApprovalWaitTimeout = 30 * time.Minute

var errToolApprovalWaitTimeout = errors.New("approval wait timed out")

func waitForToolApproval(ctx context.Context, store *pantry.CodingStore, id int64) (string, error) {
	if store == nil || id <= 0 {
		return "", fmt.Errorf("approval wait requires pantry storage and a positive id")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, toolApprovalWaitTimeout())
		defer cancel()
	}
	ch := registerToolApprovalWaiter(id)
	defer unregisterToolApprovalWaiter(id, ch)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		decision, err := currentToolApprovalDecision(store, id)
		if err != nil {
			return "", err
		}
		if decision != "" {
			return decision, nil
		}
		select {
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return "", errToolApprovalWaitTimeout
			}
			return "", ctx.Err()
		case decision := <-ch:
			decision = normalizeStoredApprovalDecision(decision)
			if decision != "" {
				return decision, nil
			}
		case <-ticker.C:
		}
	}
}

func toolApprovalWaitTimeout() time.Duration {
	raw := strings.TrimSpace(os.Getenv("MILLIWAYS_APPROVAL_WAIT_TIMEOUT"))
	if raw == "" {
		return defaultToolApprovalWaitTimeout
	}
	timeout, err := time.ParseDuration(raw)
	if err != nil || timeout <= 0 {
		return defaultToolApprovalWaitTimeout
	}
	return timeout
}

func registerToolApprovalWaiter(id int64) chan string {
	ch := make(chan string, 1)
	toolApprovalWaiters.Lock()
	toolApprovalWaiters.chans[id] = append(toolApprovalWaiters.chans[id], ch)
	toolApprovalWaiters.Unlock()
	return ch
}

func unregisterToolApprovalWaiter(id int64, ch chan string) {
	toolApprovalWaiters.Lock()
	defer toolApprovalWaiters.Unlock()
	waiters := toolApprovalWaiters.chans[id]
	for i, waiter := range waiters {
		if waiter != ch {
			continue
		}
		waiters = append(waiters[:i], waiters[i+1:]...)
		break
	}
	if len(waiters) == 0 {
		delete(toolApprovalWaiters.chans, id)
		return
	}
	toolApprovalWaiters.chans[id] = waiters
}

func notifyToolApproval(id int64, decision string) {
	decision = normalizeStoredApprovalDecision(decision)
	if id <= 0 || decision == "" {
		return
	}
	toolApprovalWaiters.Lock()
	waiters := append([]chan string(nil), toolApprovalWaiters.chans[id]...)
	delete(toolApprovalWaiters.chans, id)
	toolApprovalWaiters.Unlock()
	for _, ch := range waiters {
		select {
		case ch <- decision:
		default:
		}
	}
}

func currentToolApprovalDecision(store *pantry.CodingStore, id int64) (string, error) {
	record, err := store.GetToolApproval(id)
	if err != nil {
		return "", err
	}
	return normalizeStoredApprovalDecision(record.Decision), nil
}

func normalizeStoredApprovalDecision(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approve", "approved", "allow", "allowed", "yes":
		return "approve"
	case "deny", "denied", "reject", "rejected", "no":
		return "deny"
	default:
		return ""
	}
}
