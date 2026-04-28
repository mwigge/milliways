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

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/editorcontext"
)

func TestRootCmd_RegistersContextFlags(t *testing.T) {
	cmd := rootCmd()
	for _, flagName := range []string{"context-json", "context-stdin", "context-file"} {
		if flag := cmd.Flags().Lookup(flagName); flag == nil {
			t.Fatalf("expected %s flag to be registered", flagName)
		}
	}
}

func TestLoadDispatchContextBundle_PrefersStdin(t *testing.T) {
	stdinBundle := sampleEditorBundleJSON("stdin_test.go", "go")
	jsonBundle := sampleEditorBundleJSON("ignored.py", "python")

	bundle, err := loadDispatchContextBundle(strings.NewReader(stdinBundle), true, jsonBundle, "")
	if err != nil {
		t.Fatalf("loadDispatchContextBundle() error = %v", err)
	}
	if bundle == nil {
		t.Fatal("expected bundle")
	}
	if got := bundle.Collectors["editor"].Buffer.Path; got != "stdin_test.go" {
		t.Fatalf("bundle buffer path = %q, want stdin_test.go", got)
	}
}

func TestAssembleSignals_MergesEditorBundleWithoutPantry(t *testing.T) {
	bundle, err := editorcontext.ParseBundle([]byte(sampleEditorBundleJSON("handler_test.go", "go")))
	if err != nil {
		t.Fatalf("ParseBundle() error = %v", err)
	}

	signals := assembleSignals(nil, nil, "fix failing test", false, bundle)
	if signals == nil {
		t.Fatal("expected signals")
	}
	if signals.LSPErrors != 2 {
		t.Fatalf("LSPErrors = %d, want 2", signals.LSPErrors)
	}
	if !signals.Dirty {
		t.Fatal("Dirty = false, want true")
	}
	if !signals.InTestFile {
		t.Fatal("InTestFile = false, want true")
	}
	if signals.Language != "go" {
		t.Fatalf("Language = %q, want go", signals.Language)
	}
	if signals.FilesChanged != 4 {
		t.Fatalf("FilesChanged = %d, want 4", signals.FilesChanged)
	}
}

func TestDispatchWithContextStdin(t *testing.T) {
	configHome := t.TempDir()
	binDir := t.TempDir()
	repoRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(repoRoot, ".git"), 0o755); err != nil {
		t.Fatalf("Mkdir(.git): %v", err)
	}

	kitchenPath := writeExecutable(t, binDir, "opencode", `#!/bin/sh
printf '%s\n' 'context stdin ok'
`)

	configPath := filepath.Join(configHome, "carte.yaml")
	if err := os.WriteFile(configPath, []byte("kitchens:\n  opencode:\n    cmd: \""+kitchenPath+"\"\n    enabled: true\nrouting:\n  default: opencode\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(config): %v", err)
	}

	helper := exec.Command(
		os.Args[0],
		"-test.run=TestDispatchWithContextStdinHelper",
		"--",
		"--config", configPath,
		"--project-root", repoRoot,
		"--use-legacy-conversation",
		"--context-stdin",
		"explain the auth flow",
	)
	helper.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "HOME="+configHome)
	stdin, err := helper.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe(): %v", err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	helper.Stdout = &stdout
	helper.Stderr = &stderr

	if err := helper.Start(); err != nil {
		t.Fatalf("Start(): %v", err)
	}
	if _, err := stdin.Write([]byte(sampleEditorBundleJSON("handler_test.go", "go"))); err != nil {
		t.Fatalf("stdin.Write(): %v", err)
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("stdin.Close(): %v", err)
	}
	if err := helper.Wait(); err != nil {
		t.Fatalf("Wait() error = %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "context stdin ok") {
		t.Fatalf("stdout = %q, want kitchen output", stdout.String())
	}
}

func TestDispatchWithContextStdinHelper(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	for i, arg := range os.Args {
		if arg == "--" {
			args = os.Args[i+1:]
			break
		}
	}

	cmd := rootCmd()
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	os.Exit(0)
}

func sampleEditorBundleJSON(path, language string) string {
	return "{" +
		"\"schema_version\":\"1.0\"," +
		"\"collected_at\":\"2026-04-19T12:00:00Z\"," +
		"\"collectors\":{" +
		"\"editor\":{" +
		"\"buffer\":{" +
		"\"path\":\"" + path + "\"," +
		"\"filetype\":\"" + language + "\"," +
		"\"modified\":true," +
		"\"total_lines\":120," +
		"\"visible_start\":1," +
		"\"visible_end\":40}," +
		"\"git\":{" +
		"\"branch\":\"main\"," +
		"\"dirty\":true," +
		"\"files_changed\":4," +
		"\"ahead\":0," +
		"\"behind\":0}," +
		"\"lsp\":{" +
		"\"scope\":\"buffer\"," +
		"\"total\":3," +
		"\"errors\":2," +
		"\"warnings\":1}," +
		"\"project\":{" +
		"\"root\":\"/tmp/project\"," +
		"\"primary_language\":\"" + language + "\"}}}," +
		"\"total_bytes\":256}"
}
