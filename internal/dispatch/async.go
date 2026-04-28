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

package dispatch

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/conversation"
	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/ledger"
	"github.com/mwigge/milliways/internal/pantry"
	"github.com/mwigge/milliways/internal/substrate"
)

// convWriter is the subset of substrate.Writer used by the async dispatcher.
// It is satisfied by *substrate.SessionWriter and nil-safe via the optional
// field pattern below.
type convWriter interface {
	Begin(ctx context.Context, convID, blockID, provider, prompt string) error
	StartSegment(ctx context.Context, provider string, repoContext *conversation.RepoContext) error
	AppendTurn(ctx context.Context, role conversation.TurnRole, provider, text string, reposAccessed []string, projectRefs []conversation.ProjectRef) error
	EndSegment(ctx context.Context, status, reason string) error
	CheckpointOnExhaustion(ctx context.Context, reason string) (substrate.CheckpointResponse, error)
	Finish(ctx context.Context, status, reason string) error
}

// AsyncDispatcher handles async and detached kitchen dispatches.
type AsyncDispatcher struct {
	pdb            *pantry.DB
	substrateNewFn func() convWriter // optional factory; nil disables substrate writes
	wg             sync.WaitGroup
}

// NewAsyncDispatcher creates an async dispatcher backed by PantryDB.
// Substrate writes are disabled by default; call WithSubstrateClient to enable.
func NewAsyncDispatcher(pdb *pantry.DB) *AsyncDispatcher {
	return &AsyncDispatcher{pdb: pdb}
}

// WithSubstrateClient enables substrate write mirroring for each async dispatch.
// fn is called once per dispatch to obtain a fresh SessionWriter.
func (d *AsyncDispatcher) WithSubstrateClient(fn func() convWriter) {
	d.substrateNewFn = fn
}

// DispatchAsync runs a kitchen in a background goroutine and returns a ticket ID.
func (d *AsyncDispatcher) DispatchAsync(ctx context.Context, k kitchen.Kitchen, prompt string) (string, error) {
	outputDir := filepath.Join(os.TempDir(), "milliways-async")
	if err := os.MkdirAll(outputDir, 0o700); err != nil {
		return "", fmt.Errorf("creating async output dir: %w", err)
	}

	ticketID, err := d.pdb.Tickets().Create(k.Name(), prompt, "async", 0, "")
	if err != nil {
		return "", fmt.Errorf("creating ticket: %w", err)
	}

	outputPath := filepath.Join(outputDir, ticketID+".out")

	// Obtain a substrate writer for this dispatch (nil when substrate is disabled).
	var sw convWriter
	if d.substrateNewFn != nil {
		sw = d.substrateNewFn()
	}

	// Declare result and execErr in the outer scope so the cancellation guard
	// goroutine can assign to them.
	var result kitchen.Result
	var execErr error

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer func() {
			if p := recover(); p != nil {
				slog.Error("dispatch async goroutine panicked", "panic", p, "ticket", ticketID)
				_ = d.pdb.Tickets().UpdateStatus(ticketID, "panicked", 1, nil)
			}
		}()

		// Respect parent cancellation: if the dispatch is cancelled (process interrupt,
		// timeout, etc.) before or during execution, abort early instead of letting
		// the goroutine run to completion.
		select {
		case <-ctx.Done():
			d.pdb.Tickets().UpdateStatus(ticketID, "cancelled", 130, nil)
			return
		default:
		}

		// Substrate: conversation start + initial user turn + segment start.
		if sw != nil {
			if err := sw.Begin(ctx, ticketID, "", k.Name(), prompt); err != nil {
				slog.WarnContext(ctx, "async substrate begin", "ticket", ticketID, "err", err)
			}
			if err := sw.StartSegment(ctx, k.Name(), nil); err != nil {
				slog.WarnContext(ctx, "async substrate start segment", "ticket", ticketID, "err", err)
			}
		}

		task := kitchen.Task{Prompt: prompt}
		start := time.Now()

		// Guard k.Exec with ctx.Done() to allow early abort when parent cancels.
		execCtx := ctx
		doneCh := make(chan struct{})
		go func() {
			defer close(doneCh)
			defer func() {
				if p := recover(); p != nil {
					result = kitchen.Result{ExitCode: 1}
					execErr = fmt.Errorf("kitchen exec panicked: %v", p)
				}
			}()
			result, execErr = k.Exec(execCtx, task)
		}()
		select {
		case <-doneCh:
			// k.Exec returned normally.
		case <-ctx.Done():
			// Parent cancelled — k.Exec may still be running but will respect
			// its own context deadline. We record cancellation and exit.
			d.pdb.Tickets().UpdateStatus(ticketID, "cancelled", 130, nil)
			return
		}
		dur := time.Since(start)

		// Substrate: append assistant turn.
		if sw != nil && result.Output != "" {
			if err := sw.AppendTurn(ctx, conversation.RoleAssistant, k.Name(), result.Output, nil, nil); err != nil {
				slog.WarnContext(ctx, "async substrate append turn", "ticket", ticketID, "err", err)
			}
		}

		// Write output to file
		if result.Output != "" {
			_ = os.WriteFile(outputPath, []byte(result.Output), 0o600)
		}

		status := "complete"
		exitCode := result.ExitCode
		if execErr != nil {
			status = "failed"
			exitCode = 1
		}

		// Update ticket
		if updateErr := d.pdb.Tickets().UpdateStatus(ticketID, status, exitCode, nil); updateErr != nil {
			slog.WarnContext(ctx, "async ticket update", "ticket", ticketID, "status", status, "exit_code", exitCode, "err", updateErr)
		}

		// Write ledger entry
		entry := pantry.LedgerEntry{
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
			TaskHash:     fmt.Sprintf("async:%s", ticketID),
			Kitchen:      k.Name(),
			DurationSec:  dur.Seconds(),
			ExitCode:     exitCode,
			Outcome:      ledger.OutcomeFromExitCode(exitCode),
			DispatchMode: "async",
		}
		if ledgerID, ledgerErr := d.pdb.Ledger().Insert(entry); ledgerErr == nil {
			if updateErr := d.pdb.Tickets().UpdateStatus(ticketID, status, exitCode, &ledgerID); updateErr != nil {
				slog.WarnContext(ctx, "async ticket update ledger", "ticket", ticketID, "ledger_id", ledgerID, "status", status, "exit_code", exitCode, "err", updateErr)
			}
		}

		// Substrate: close segment and write checkpoint on exhaustion, or finish normally.
		if sw != nil {
			exhausted := exitCode != 0 && execErr == nil
			if exhausted {
				if _, ckptErr := sw.CheckpointOnExhaustion(ctx, "dispatch-exhausted"); ckptErr != nil {
					slog.WarnContext(ctx, "async substrate checkpoint on exhaustion", "ticket", ticketID, "err", ckptErr)
				}
			} else {
				segStatus := "done"
				if execErr != nil {
					segStatus = "failed"
				}
				if err := sw.EndSegment(ctx, segStatus, status); err != nil {
					slog.WarnContext(ctx, "async substrate end segment", "ticket", ticketID, "segment_status", segStatus, "status", status, "err", err)
				}
			}
			convStatus := "done"
			if execErr != nil {
				convStatus = "failed"
			}
			if err := sw.Finish(ctx, convStatus, status); err != nil {
				slog.WarnContext(ctx, "async substrate finish", "ticket", ticketID, "conversation_status", convStatus, "status", status, "err", err)
			}
		}
	}()

	return ticketID, nil
}

// DispatchDetached is not yet implemented. Detached mode requires
// platform-specific process management. Use --async instead.
func (d *AsyncDispatcher) DispatchDetached(_ kitchen.Kitchen, _ string) (string, error) {
	return "", fmt.Errorf("detached dispatch not yet implemented — use --async instead")
}

// Wait blocks until all async dispatches have completed.
func (d *AsyncDispatcher) Wait() {
	d.wg.Wait()
}
