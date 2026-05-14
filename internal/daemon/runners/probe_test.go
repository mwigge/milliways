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
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProbeClaudeUsesRunnerBinarySearchPath(t *testing.T) {
	dir := t.TempDir()
	writeProbeExecutable(t, filepath.Join(dir, "claude"))
	t.Setenv("MILLIWAYS_PATH", dir)
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty-path"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CLAUDE_CODE_EXECPATH", "")

	info := probeClaude(context.Background())
	if !info.Available {
		t.Fatalf("probeClaude Available = false, want true for MILLIWAYS_PATH binary")
	}
	if info.AuthStatus != "unknown" {
		t.Fatalf("probeClaude AuthStatus = %q, want unknown", info.AuthStatus)
	}
}

func TestProbeCodexUsesRunnerBinarySearchPath(t *testing.T) {
	dir := t.TempDir()
	writeProbeExecutable(t, filepath.Join(dir, "codex"))
	t.Setenv("MILLIWAYS_PATH", dir)
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty-path"))
	t.Setenv("HOME", t.TempDir())

	info := probeCodex(context.Background())
	if !info.Available {
		t.Fatalf("probeCodex Available = false, want true for MILLIWAYS_PATH binary")
	}
	if info.AuthStatus != "unknown" {
		t.Fatalf("probeCodex AuthStatus = %q, want unknown", info.AuthStatus)
	}
}

func TestExternalCLIProbesUseRunnerBinarySearchPath(t *testing.T) {
	for _, tc := range []struct {
		name  string
		probe func(context.Context) AgentInfo
	}{
		{"copilot", probeCopilot},
		{"gemini", probeGemini},
		{"pool", probePool},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeProbeExecutable(t, filepath.Join(dir, tc.name))
			t.Setenv("MILLIWAYS_PATH", dir)
			t.Setenv("PATH", filepath.Join(t.TempDir(), "empty-path"))
			t.Setenv("HOME", t.TempDir())

			info := tc.probe(context.Background())
			if !info.Available {
				t.Fatalf("probe %s Available = false, want true for MILLIWAYS_PATH binary", tc.name)
			}
			if info.AuthStatus != "ok" {
				t.Fatalf("probe %s AuthStatus = %q, want ok", tc.name, info.AuthStatus)
			}
		})
	}
}

func TestProbeSkipsShimBinaryWhenResolvingExternalCLI(t *testing.T) {
	SetBrokerPathProvider(nil)
	t.Cleanup(func() { SetBrokerPathProvider(nil) })

	root := t.TempDir()
	shimDir := filepath.Join(root, "shims")
	realDir := filepath.Join(root, "real")
	if err := os.MkdirAll(shimDir, 0o755); err != nil {
		t.Fatalf("mkdir shim dir: %v", err)
	}
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("mkdir real dir: %v", err)
	}
	writeProbeExecutable(t, filepath.Join(shimDir, "gemini"))
	writeProbeExecutable(t, filepath.Join(realDir, "gemini"))
	SetBrokerPathProvider(func(agentID string) string {
		if agentID == AgentIDGemini {
			return shimDir
		}
		return ""
	})
	t.Setenv("MILLIWAYS_PATH", shimDir+string(os.PathListSeparator)+realDir)
	t.Setenv("PATH", filepath.Join(t.TempDir(), "empty-path"))
	t.Setenv("HOME", t.TempDir())

	info := probeGemini(context.Background())
	if !info.Available || info.AuthStatus != "ok" {
		t.Fatalf("probeGemini = %#v, want available ok using non-shim binary", info)
	}

	if err := os.Remove(filepath.Join(realDir, "gemini")); err != nil {
		t.Fatalf("remove real gemini: %v", err)
	}
	info = probeGemini(context.Background())
	if info.Available {
		t.Fatalf("probeGemini Available = true with only shim binary, want false")
	}
}

func writeProbeExecutable(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write probe executable: %v", err)
	}
}
