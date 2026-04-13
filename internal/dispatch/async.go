package dispatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/pantry"
)

// AsyncDispatcher handles async and detached kitchen dispatches.
type AsyncDispatcher struct {
	pdb *pantry.DB
	wg  sync.WaitGroup
}

// NewAsyncDispatcher creates an async dispatcher backed by PantryDB.
func NewAsyncDispatcher(pdb *pantry.DB) *AsyncDispatcher {
	return &AsyncDispatcher{pdb: pdb}
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

	d.wg.Add(1)
	go func() {
		defer d.wg.Done()

		task := kitchen.Task{Prompt: prompt}
		start := time.Now()
		result, execErr := k.Exec(ctx, task)
		dur := time.Since(start)

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
		_ = d.pdb.Tickets().UpdateStatus(ticketID, status, exitCode, nil)

		// Write ledger entry
		entry := pantry.LedgerEntry{
			Timestamp:    time.Now().UTC().Format(time.RFC3339),
			TaskHash:     fmt.Sprintf("async:%s", ticketID),
			Kitchen:      k.Name(),
			DurationSec:  dur.Seconds(),
			ExitCode:     exitCode,
			Outcome:      outcomeStr(exitCode),
			DispatchMode: "async",
		}
		if ledgerID, ledgerErr := d.pdb.Ledger().Insert(entry); ledgerErr == nil {
			_ = d.pdb.Tickets().UpdateStatus(ticketID, status, exitCode, &ledgerID)
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

func outcomeStr(exitCode int) string {
	if exitCode == 0 {
		return "success"
	}
	return "failure"
}
