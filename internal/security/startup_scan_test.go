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

package security_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/rules"
)

func TestRunStartupScanCleanFixture(t *testing.T) {
	t.Parallel()

	result, err := security.RunStartupScan(context.Background(), security.StartupScanOptions{
		WorkspaceRoot: filepath.Join("testdata", "startup", "clean"),
		Now:           fixedStartupScanTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Findings) != 0 {
		t.Fatalf("expected no findings, got %#v", result.Findings)
	}
	if len(result.FilesScanned) == 0 {
		t.Fatal("expected startup scan to record scanned files")
	}
	if result.StartedAt.IsZero() || result.CompletedAt.IsZero() {
		t.Fatal("expected scan timestamps")
	}
}

func TestRunStartupScanWarnAndBlockFixture(t *testing.T) {
	t.Parallel()

	result, err := security.RunStartupScan(context.Background(), security.StartupScanOptions{
		WorkspaceRoot: filepath.Join("testdata", "startup", "bad"),
		UserPersistenceRoots: []security.StartupScanRoot{
			{Name: "systemd", Path: filepath.Join("testdata", "startup", "bad-user", "systemd")},
		},
		Now: fixedStartupScanTime,
	})
	if err != nil {
		t.Fatal(err)
	}

	findings := findingsByID(result.Findings)
	for _, id := range []string{
		"ioc.mini-shai-hulud.file",
		"ioc.mini-shai-hulud.network",
		"persist.gh-token-monitor",
		"client.vscode.folder-open-task",
		"client.claude.hooks",
		"client.claude.executable-config",
		"pkg.lifecycle-script",
		"pkg.github-commit-dependency",
		"policy.npm.ignore-scripts",
		"policy.package.lockfile",
		"policy.package.release-age",
	} {
		if _, ok := findings[id]; !ok {
			t.Fatalf("missing finding %q in %#v", id, result.Findings)
		}
	}

	if got := findings["ioc.mini-shai-hulud.network"].Severity; got != rules.SeverityBlock {
		t.Fatalf("network IOC severity = %q, want %q", got, rules.SeverityBlock)
	}
	if got := findings["client.vscode.folder-open-task"].Severity; got != rules.SeverityWarn {
		t.Fatalf("VS Code folder-open severity = %q, want %q", got, rules.SeverityWarn)
	}
	if findings["ioc.mini-shai-hulud.network"].Line == 0 {
		t.Fatal("expected line number for network IOC")
	}
}

func TestRunStartupScanRequiresWorkspaceRoot(t *testing.T) {
	t.Parallel()

	_, err := security.RunStartupScan(context.Background(), security.StartupScanOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func findingsByID(findings []security.StartupFinding) map[string]security.StartupFinding {
	out := make(map[string]security.StartupFinding, len(findings))
	for _, f := range findings {
		out[f.ID] = f
	}
	return out
}

func fixedStartupScanTime() time.Time {
	return time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
}
