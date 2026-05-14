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

package runners

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureRunnerSystemPathAddsBashPath(t *testing.T) {
	path := ensureRunnerSystemPath("/tmp/copilot-bin")

	if !pathContains(path, "/tmp/copilot-bin") {
		t.Fatalf("custom path missing from %q", path)
	}
	if !pathContains(path, "/bin") {
		t.Fatalf("/bin missing from %q", path)
	}
	if !pathContains(path, "/usr/bin") {
		t.Fatalf("/usr/bin missing from %q", path)
	}
}

func TestSafeRunnerEnvMILLIWAYSPathKeepsSystemFallbacks(t *testing.T) {
	t.Setenv("PATH", "/should/not/win")
	t.Setenv("MILLIWAYS_PATH", "/tmp/copilot-bin")

	env := safeRunnerEnv()
	path := envValue(env, "PATH")
	if !pathContains(path, "/tmp/copilot-bin") {
		t.Fatalf("MILLIWAYS_PATH missing from PATH=%q", path)
	}
	if pathContains(path, "/should/not/win") {
		t.Fatalf("inherited PATH leaked despite MILLIWAYS_PATH override: %q", path)
	}
	if !pathContains(path, "/bin") {
		t.Fatalf("/bin missing from PATH=%q", path)
	}
}

func TestControlledRunnerEnvAddsIdentityWorkspaceAndShimPath(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	binDir := filepath.Join(root, "bin")
	t.Setenv("PATH", binDir)
	t.Setenv("MILLIWAYS_PATH", "")

	env := controlledRunnerEnv(controlledRunnerEnvOptions{
		ClientID:  AgentIDCodex,
		SessionID: "session-1",
		Workspace: filepath.Join(root, "workspace"),
		ShimDir:   shimDir,
	})

	if got := envValue(env, "MILLIWAYS_CLIENT_ID"); got != AgentIDCodex {
		t.Fatalf("MILLIWAYS_CLIENT_ID = %q, want %q", got, AgentIDCodex)
	}
	if got := envValue(env, "MILLIWAYS_SESSION_ID"); got != "session-1" {
		t.Fatalf("MILLIWAYS_SESSION_ID = %q, want session-1", got)
	}
	if got := envValue(env, "MILLIWAYS_WORKSPACE_ROOT"); got == "" {
		t.Fatalf("MILLIWAYS_WORKSPACE_ROOT missing from env")
	}
	if got := envValue(env, "MILLIWAYS_SHIM_DIR"); got != shimDir {
		t.Fatalf("MILLIWAYS_SHIM_DIR = %q, want %q", got, shimDir)
	}
	if got := envValue(env, "MILLIWAYS_SHIMS_ENABLED"); got != "1" {
		t.Fatalf("MILLIWAYS_SHIMS_ENABLED = %q, want 1", got)
	}
	path := envValue(env, "PATH")
	if firstPath(path) != shimDir {
		t.Fatalf("PATH first entry = %q, want shim dir; PATH=%q", firstPath(path), path)
	}
	if !pathContains(path, binDir) {
		t.Fatalf("original PATH dir missing after shim prepend: %q", path)
	}
}

func TestControlledRunnerEnvMILLIWAYSPathStillOverridesInheritedPath(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	milliwaysPath := filepath.Join(root, "milliways-bin")
	t.Setenv("PATH", filepath.Join(root, "inherited"))
	t.Setenv("MILLIWAYS_PATH", milliwaysPath)

	env := controlledRunnerEnv(controlledRunnerEnvOptions{
		ClientID:  AgentIDClaude,
		SessionID: "session-2",
		ShimDir:   shimDir,
	})
	path := envValue(env, "PATH")
	if firstPath(path) != shimDir {
		t.Fatalf("PATH first entry = %q, want shim dir; PATH=%q", firstPath(path), path)
	}
	if !pathContains(path, milliwaysPath) {
		t.Fatalf("MILLIWAYS_PATH missing from PATH=%q", path)
	}
	if pathContains(path, filepath.Join(root, "inherited")) {
		t.Fatalf("inherited PATH leaked despite MILLIWAYS_PATH override: %q", path)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func firstPath(path string) string {
	parts := strings.Split(path, string(os.PathListSeparator))
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}

func pathContains(path, want string) bool {
	for _, part := range strings.Split(path, string(os.PathListSeparator)) {
		if part == want {
			return true
		}
	}
	return false
}

func readEnvCapture(t *testing.T, path string) []string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read env capture: %v", err)
	}
	fields := strings.Split(strings.TrimSpace(string(raw)), "\t")
	if len(fields) != 6 || fields[0] != "ENV" {
		t.Fatalf("bad env capture: %q", raw)
	}
	return fields
}
