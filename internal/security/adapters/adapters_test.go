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

package adapters_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/security"
	"github.com/mwigge/milliways/internal/security/adapters"
)

func TestAdapterInstalledAndVersionWithInjectedRunners(t *testing.T) {
	t.Parallel()

	adapter := adapters.NewGitleaks(
		adapters.WithLookPath(func(file string) (string, error) {
			if file != "gitleaks" {
				t.Fatalf("lookpath file = %q, want gitleaks", file)
			}
			return "/fake/gitleaks", nil
		}),
		adapters.WithExec(func(_ context.Context, path string, args ...string) ([]byte, []byte, error) {
			if path != "/fake/gitleaks" {
				t.Fatalf("path = %q, want fake path", path)
			}
			if strings.Join(args, " ") != "--version" {
				t.Fatalf("args = %v, want --version", args)
			}
			return []byte("gitleaks version 8.24.0\n"), nil, nil
		}),
	)

	if !adapter.Installed() {
		t.Fatal("Installed() = false, want true")
	}
	version, err := adapter.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version != "gitleaks version 8.24.0" {
		t.Fatalf("version = %q", version)
	}
}

func TestAdapterNotInstalled(t *testing.T) {
	t.Parallel()

	adapter := adapters.NewSemgrep(adapters.WithLookPath(func(string) (string, error) {
		return "", errors.New("missing")
	}))

	if adapter.Installed() {
		t.Fatal("Installed() = true, want false")
	}
	_, err := adapter.Scan(context.Background(), "/work/app", nil)
	if !errors.Is(err, adapters.ErrNotInstalled) {
		t.Fatalf("Scan error = %v, want ErrNotInstalled", err)
	}
}

func TestGitleaksScanParsesFixture(t *testing.T) {
	t.Parallel()

	result := scanWithFixture(t, adapters.NewGitleaks, "gitleaks.json", []string{"/work/app"})
	if result.Kind != security.ScanSecret {
		t.Fatalf("Kind = %q, want secret", result.Kind)
	}
	if result.ToolName != "gitleaks" {
		t.Fatalf("ToolName = %q, want gitleaks", result.ToolName)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(result.Findings))
	}
	f := result.Findings[0]
	if f.Category != security.FindingSecret || f.Severity != "HIGH" || f.FilePath != "config.env" || f.Line != 12 {
		t.Fatalf("unexpected finding: %#v", f)
	}
	rendered := adapters.NewGitleaks().RenderFinding(f)
	if !strings.Contains(rendered, "Generic API Key") || !strings.Contains(rendered, "config.env:12:5") {
		t.Fatalf("rendered finding = %q", rendered)
	}
}

func TestSemgrepScanParsesFixture(t *testing.T) {
	t.Parallel()

	result := scanWithFixture(t, adapters.NewSemgrep, "semgrep.json", []string{"/work/app"})
	if result.Kind != security.ScanSAST {
		t.Fatalf("Kind = %q, want sast", result.Kind)
	}
	f := singleFinding(t, result)
	if f.Category != security.FindingSAST || f.Severity != "MEDIUM" || f.ID == "" || f.FilePath != "server.go" {
		t.Fatalf("unexpected finding: %#v", f)
	}
}

func TestDefaultScanArgsAvoidVersionAndMetricsNetworkChecks(t *testing.T) {
	t.Parallel()

	var got []string
	adapter := adapters.NewSemgrep(
		adapters.WithLookPath(func(file string) (string, error) {
			return "/fake/" + file, nil
		}),
		adapters.WithExec(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
			got = append([]string(nil), args...)
			return []byte(`{"results":[]}`), nil, nil
		}),
	)
	if _, err := adapter.Scan(context.Background(), "/work/app", nil); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	joined := strings.Join(got, " ")
	for _, want := range []string{"--disable-version-check", "--metrics=off"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args = %v, missing %s", got, want)
		}
	}
}

func TestGovulncheckDefaultTargetIsWorkspaceRooted(t *testing.T) {
	t.Parallel()

	var got []string
	adapter := adapters.NewGovulncheck(
		adapters.WithLookPath(func(file string) (string, error) {
			return "/fake/" + file, nil
		}),
		adapters.WithExec(func(_ context.Context, _ string, args ...string) ([]byte, []byte, error) {
			got = append([]string(nil), args...)
			return nil, nil, nil
		}),
	)
	if _, err := adapter.Scan(context.Background(), "/work/app", nil); err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if strings.Join(got, " ") != "-json /work/app/..." {
		t.Fatalf("args = %v, want -json /work/app/...", got)
	}
}

func TestGovulncheckScanParsesFixture(t *testing.T) {
	t.Parallel()

	result := scanWithFixture(t, adapters.NewGovulncheck, "govulncheck.jsonl", []string{"./..."})
	if result.Kind != security.ScanDependency {
		t.Fatalf("Kind = %q, want dependency", result.Kind)
	}
	f := singleFinding(t, result)
	if f.CVEID != "CVE-2024-12345" || f.PackageName != "example.com/mod" || f.FixedInVersion != "v1.2.3" {
		t.Fatalf("unexpected finding: %#v", f)
	}
}

func TestOSVScannerScanParsesFixture(t *testing.T) {
	t.Parallel()

	result := scanWithFixture(t, adapters.NewOSVScanner, "osv-scanner.json", []string{"/work/app/go.sum"})
	f := singleFinding(t, result)
	if f.CVEID != "CVE-2024-12345" || f.Severity != "HIGH" || f.ScanSource != "go.sum" {
		t.Fatalf("unexpected finding: %#v", f)
	}
	rendered := adapters.NewOSVScanner().RenderFinding(f)
	if !strings.Contains(rendered, "CVE-2024-12345") || !strings.Contains(rendered, "fixed in v1.2.3") {
		t.Fatalf("rendered finding = %q", rendered)
	}
}

func TestDefaultExecCanRunFakeBinary(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("shell fake binary is unix-specific")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "gitleaks")
	script := "#!/bin/sh\nif [ \"$1\" = \"--version\" ]; then echo 'gitleaks 8.24.0'; exit 0; fi\ncat '" + fixturePath(t, "gitleaks.json") + "'\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	adapter := adapters.NewGitleaks(adapters.WithLookPath(func(string) (string, error) {
		return bin, nil
	}))

	version, err := adapter.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version != "gitleaks 8.24.0" {
		t.Fatalf("version = %q", version)
	}
	result, err := adapter.Scan(context.Background(), "/work/app", nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1", len(result.Findings))
	}
}

type adapterConstructor func(...adapters.Option) adapters.ScannerAdapter

func scanWithFixture(t *testing.T, newAdapter adapterConstructor, fixture string, targets []string) security.ScanResult {
	t.Helper()
	output, err := os.ReadFile(fixturePath(t, fixture))
	if err != nil {
		t.Fatal(err)
	}
	adapter := newAdapter(
		adapters.WithLookPath(func(file string) (string, error) {
			return "/fake/" + file, nil
		}),
		adapters.WithExec(func(_ context.Context, _ string, _ ...string) ([]byte, []byte, error) {
			return output, nil, nil
		}),
	)
	result, err := adapter.Scan(context.Background(), "/work/app", targets)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return result
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	return filepath.Join("testdata", name)
}

func singleFinding(t *testing.T, result security.ScanResult) security.Finding {
	t.Helper()
	if len(result.Findings) != 1 {
		t.Fatalf("findings = %d, want 1: %#v", len(result.Findings), result.Findings)
	}
	return result.Findings[0]
}
