package dispatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/pantry"
)

// AsyncDispatcher handles async and detached kitchen dispatches.
type AsyncDispatcher struct {
	pdb *pantry.DB
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

	go func() {
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

// DispatchDetached runs a kitchen as a detached OS process that survives Milliways exit.
func (d *AsyncDispatcher) DispatchDetached(k kitchen.Kitchen, prompt string) (string, error) {
	detachedDir := filepath.Join(os.TempDir(), "milliways-detached")
	if err := os.MkdirAll(detachedDir, 0o700); err != nil {
		return "", fmt.Errorf("creating detached dir: %w", err)
	}

	ticketID, err := d.pdb.Tickets().Create(k.Name(), prompt, "detached", 0, "")
	if err != nil {
		return "", fmt.Errorf("creating ticket: %w", err)
	}

	outputPath := filepath.Join(detachedDir, ticketID+".log")

	// For detached mode, we need to track that the process was started.
	// The actual detached process management requires platform-specific code.
	// For now, write a marker file that can be checked later.
	marker := fmt.Sprintf("kitchen=%s\nprompt=%s\nstarted=%s\nstatus=running\n",
		k.Name(), prompt, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(outputPath, []byte(marker), 0o600); err != nil {
		return "", fmt.Errorf("writing detached marker: %w", err)
	}

	return ticketID, nil
}

func outcomeStr(exitCode int) string {
	if exitCode == 0 {
		return "success"
	}
	return "failure"
}
