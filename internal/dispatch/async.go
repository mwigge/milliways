package dispatch

import (
	"context"
	"fmt"
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
	StartSegment(ctx context.Context, provider string) error
	AppendTurn(ctx context.Context, role conversation.TurnRole, provider, text string) error
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

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()

		// Substrate: conversation start + initial user turn + segment start.
		if sw != nil {
			if err := sw.Begin(ctx, ticketID, "", k.Name(), prompt); err != nil {
				fmt.Fprintf(os.Stderr, "[async] substrate Begin warning: %v\n", err)
			}
			if err := sw.StartSegment(ctx, k.Name()); err != nil {
				fmt.Fprintf(os.Stderr, "[async] substrate StartSegment warning: %v\n", err)
			}
		}

		task := kitchen.Task{Prompt: prompt}
		start := time.Now()
		result, execErr := k.Exec(ctx, task)
		dur := time.Since(start)

		// Substrate: append assistant turn.
		if sw != nil && result.Output != "" {
			if err := sw.AppendTurn(ctx, conversation.RoleAssistant, k.Name(), result.Output); err != nil {
				fmt.Fprintf(os.Stderr, "[async] substrate AppendTurn warning: %v\n", err)
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
			fmt.Fprintf(os.Stderr, "[async] ticket update warning: %v\n", updateErr)
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
				fmt.Fprintf(os.Stderr, "[async] ticket update warning: %v\n", updateErr)
			}
		}

		// Substrate: close segment and write checkpoint on exhaustion, or finish normally.
		if sw != nil {
			exhausted := exitCode != 0 && execErr == nil
			if exhausted {
				if _, ckptErr := sw.CheckpointOnExhaustion(ctx, "dispatch-exhausted"); ckptErr != nil {
					fmt.Fprintf(os.Stderr, "[async] substrate CheckpointOnExhaustion warning: %v\n", ckptErr)
				}
			} else {
				segStatus := "done"
				if execErr != nil {
					segStatus = "failed"
				}
				if err := sw.EndSegment(ctx, segStatus, status); err != nil {
					fmt.Fprintf(os.Stderr, "[async] substrate EndSegment warning: %v\n", err)
				}
			}
			convStatus := "done"
			if execErr != nil {
				convStatus = "failed"
			}
			if err := sw.Finish(ctx, convStatus, status); err != nil {
				fmt.Fprintf(os.Stderr, "[async] substrate Finish warning: %v\n", err)
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
