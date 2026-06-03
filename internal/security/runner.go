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

package security

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/pantry"
)

const pollInterval = 5 * time.Minute

// ScanFunc is the signature for the function used to scan lockfiles.
// Decoupled to allow test injection.
type ScanFunc func(ctx context.Context, lockfiles []string) (ScanResult, error)

// Runner manages background OSV scanning for a workspace.
type Runner struct {
	store         *pantry.SecurityStore
	workspaceRoot string
	scanFn        ScanFunc

	enabled     bool // false = scanning disabled; toggled via Enable/Disable
	mu          sync.Mutex
	lastScanned map[string]time.Time // lockfile path → last scan time

	scanOnce sync.Once
	ready    chan struct{} // closed after first scan completes
}

// Enable turns scanning on. Safe to call concurrently.
func (r *Runner) Enable() {
	r.mu.Lock()
	r.enabled = true
	r.mu.Unlock()
}

// Disable turns scanning off. Safe to call concurrently.
func (r *Runner) Disable() {
	r.mu.Lock()
	r.enabled = false
	r.mu.Unlock()
}

// IsEnabled reports whether scanning is currently enabled.
func (r *Runner) IsEnabled() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.enabled
}

// NewRunner creates a Runner that uses the real Scan function.
func NewRunner(store *pantry.SecurityStore, workspaceRoot string) *Runner {
	return newRunner(store, workspaceRoot, Scan)
}

// NewRunnerWithScanFunc creates a Runner with a custom scan function for testing.
func NewRunnerWithScanFunc(store *pantry.SecurityStore, workspaceRoot string, fn ScanFunc) *Runner {
	return newRunner(store, workspaceRoot, fn)
}

func newRunner(store *pantry.SecurityStore, workspaceRoot string, fn ScanFunc) *Runner {
	// Enabled by default when the osv-scanner binary is available.
	enabled := ScannerPath() != ""
	return &Runner{
		store:         store,
		workspaceRoot: workspaceRoot,
		scanFn:        fn,
		enabled:       enabled,
		lastScanned:   make(map[string]time.Time),
		ready:         make(chan struct{}),
	}
}

// Start runs the initial scan in a background goroutine, then starts PollLoop.
// Returns immediately. Skips scan silently when disabled or binary absent.
func (r *Runner) Start(ctx context.Context) {
	go func() {
		if !r.IsEnabled() {
			slog.Debug("security: scanning disabled or osv-scanner not installed; skipping")
			r.markReady()
			go r.PollLoop(ctx)
			return
		}
		if _, err := r.ScanNow(ctx); err != nil {
			if !errors.Is(err, ErrScannerNotFound) {
				slog.Warn("security: initial scan failed", "err", err)
			}
		}
		go r.PollLoop(ctx)
	}()
}

// Ready returns a channel closed after the first scan completes.
func (r *Runner) Ready() <-chan struct{} {
	return r.ready
}

// ScanNow runs a synchronous scan with the given context. Returns the ScanResult.
func (r *Runner) ScanNow(ctx context.Context) (ScanResult, error) {
	lockfiles := DiscoverLockfiles(r.workspaceRoot)

	result, err := r.scanFn(ctx, lockfiles)
	if err != nil {
		r.markReady()
		return ScanResult{}, err
	}
	if result.Workspace == "" {
		result.Workspace = r.workspaceRoot
	}

	if upsertErr := r.UpsertFindings(result); upsertErr != nil {
		slog.Warn("security: upsert findings failed", "err", upsertErr)
	}

	// Update lastScanned for each lockfile.
	r.mu.Lock()
	for _, lf := range lockfiles {
		r.lastScanned[lf] = time.Now()
	}
	r.mu.Unlock()

	r.markReady()
	return result, nil
}

// PollLoop ticks every 5 minutes, checks mtime of discovered lockfiles, and
// triggers a rescan if any lockfile is newer than when it was last scanned.
func (r *Runner) PollLoop(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lockfiles := DiscoverLockfiles(r.workspaceRoot)
			var toScan []string
			r.mu.Lock()
			for _, lf := range lockfiles {
				info, err := os.Stat(lf)
				if err != nil {
					continue
				}
				if info.ModTime().After(r.lastScanned[lf]) {
					toScan = append(toScan, lf)
				}
			}
			r.mu.Unlock()

			if len(toScan) == 0 {
				continue
			}

			result, err := r.scanFn(ctx, toScan)
			if err != nil {
				slog.Warn("security: poll scan failed", "err", err)
				continue
			}
			if result.Workspace == "" {
				result.Workspace = r.workspaceRoot
			}

			if upsertErr := r.UpsertFindings(result); upsertErr != nil {
				slog.Warn("security: upsert findings failed", "err", upsertErr)
			}

			r.mu.Lock()
			for _, lf := range toScan {
				r.lastScanned[lf] = time.Now()
			}
			r.mu.Unlock()
		}
	}
}

// UpsertFindings persists findings to the store and marks resolved findings for
// each lockfile present in the scan result.
func (r *Runner) UpsertFindings(result ScanResult) error {
	// Group findings by scan source.
	bySrc := make(map[string][]Finding)
	for _, f := range result.Findings {
		bySrc[f.ScanSource] = append(bySrc[f.ScanSource], f)
	}

	// Also include lockfiles that produced zero findings so we mark everything resolved.
	for _, lf := range result.LockFiles {
		if _, ok := bySrc[lf]; !ok {
			bySrc[lf] = nil
		}
	}

	for src, findings := range bySrc {
		// Upsert each active finding.
		activeCVEs := make(map[string]struct{}, len(findings))
		for _, f := range findings {
			if err := r.store.UpsertFinding(pantry.SecurityFinding{
				Workspace:        result.Workspace,
				CVEID:            f.CVEID,
				PackageName:      f.PackageName,
				InstalledVersion: f.InstalledVersion,
				FixedInVersion:   f.FixedInVersion,
				Severity:         f.Severity,
				Ecosystem:        f.Ecosystem,
				Summary:          f.Summary,
				ScanSource:       f.ScanSource,
				Status:           "active",
			}); err != nil {
				slog.Warn("security: upsert finding", "cve", f.CVEID, "err", err)
				continue
			}
			activeCVEs[f.CVEID+":"+f.PackageName] = struct{}{}
		}

		// Mark stale findings for this source as resolved.
		if err := r.store.MarkResolvedForWorkspaceSource(result.Workspace, src, activeCVEs); err != nil {
			slog.Warn("security: mark resolved for source", "src", src, "err", err)
		}
	}
	return nil
}

// markReady closes the ready channel exactly once.
func (r *Runner) markReady() {
	r.scanOnce.Do(func() {
		close(r.ready)
	})
}
