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
	"path/filepath"
	"strings"
	"testing"
)

// TestRunCodegraph_NoArgsPrintsUsageAndExits2 verifies that calling
// runCodegraph with no arguments returns exit code 2 and prints usage.
func TestRunCodegraph_NoArgsPrintsUsageAndExits2(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runCodegraph(nil, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "usage") {
		t.Errorf("stderr = %q, want usage mention", stderr.String())
	}
}

// TestRunCodegraph_HelpExitsZero verifies --help returns 0 and prints usage.
func TestRunCodegraph_HelpExitsZero(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runCodegraph([]string{"--help"}, &stdout, &stderr)
	if code != 0 {
		t.Errorf("exit = %d, want 0", code)
	}
	if !strings.Contains(stdout.String(), "index") {
		t.Errorf("help output = %q, want it to list index", stdout.String())
	}
}

// TestRunCodegraph_UnknownVerbReturnsError verifies an unknown sub-subcommand
// returns 2 and names the bad verb.
func TestRunCodegraph_UnknownVerbReturnsError(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runCodegraph([]string{"hallucinated-verb"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("exit = %d, want 2 (usage error)", code)
	}
	if !strings.Contains(stderr.String(), "hallucinated-verb") {
		t.Errorf("stderr = %q, want it to name the bad verb", stderr.String())
	}
}

// writeTestBinary creates an executable shell script at dir/name and returns
// its path. The content must include the shebang line.
func writeTestBinary(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		t.Fatalf("writeTestBinary(%s): %v", name, err)
	}
	return path
}

// TestRunCodegraphIndex_SuccessWritesEnvFile verifies that on a successful
// codegraph index run the workspace path is written to local.env.
func TestRunCodegraphIndex_SuccessWritesEnvFile(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))

	// Fake codegraph binary that exits 0 and prints "indexed".
	binDir := t.TempDir()
	fakeCodegraph := writeTestBinary(t, binDir, "codegraph", "#!/bin/sh\necho indexed\nexit 0\n")
	t.Setenv("MILLIWAYS_CODEGRAPH_MCP_CMD", fakeCodegraph)

	indexPath := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runCodegraph([]string{"index", indexPath}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stdout = %q stderr = %q", code, stdout.String(), stderr.String())
	}

	envPath := filepath.Join(tmp, ".config", "milliways", "local.env")
	body, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("expected %s to exist, err = %v", envPath, err)
	}
	if !strings.Contains(string(body), "MILLIWAYS_CODEGRAPH_WORKSPACE=") {
		t.Errorf("env file missing MILLIWAYS_CODEGRAPH_WORKSPACE; got:\n%s", body)
	}
	if !strings.Contains(string(body), indexPath) {
		t.Errorf("env file missing path %q; got:\n%s", indexPath, body)
	}
}

// TestRunCodegraphIndex_FailurePropagatesExitCode verifies that when the
// codegraph binary exits non-zero, runCodegraph returns that exit code.
func TestRunCodegraphIndex_FailurePropagatesExitCode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))

	binDir := t.TempDir()
	fakeCodegraph := writeTestBinary(t, binDir, "codegraph-fail", "#!/bin/sh\necho 'index failed' >&2\nexit 1\n")
	t.Setenv("MILLIWAYS_CODEGRAPH_MCP_CMD", fakeCodegraph)

	var stdout, stderr bytes.Buffer
	code := runCodegraph([]string{"index"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("exit = 0, want non-zero when codegraph fails")
	}
}

// TestRunCodegraphIndex_NoBinaryReturnsError verifies that when no codegraph
// binary can be found runCodegraph returns 1 and prints a diagnostic.
func TestRunCodegraphIndex_NoBinaryReturnsError(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tmp, ".config"))
	// No MILLIWAYS_CODEGRAPH_MCP_CMD, no codegraph on PATH, no share dirs.
	t.Setenv("MILLIWAYS_CODEGRAPH_MCP_CMD", "")
	// Point share dirs to the empty tmp so all candidate paths miss.
	// We achieve this by ensuring the executable-relative share doesn't
	// exist (it won't in tests) and PATH has no codegraph.
	t.Setenv("PATH", tmp) // only tmp on PATH; no codegraph there

	var stdout, stderr bytes.Buffer
	code := runCodegraph([]string{"index"}, &stdout, &stderr)
	if code == 0 {
		t.Errorf("exit = 0, want non-zero when binary not found")
	}
	if !strings.Contains(stderr.String(), "codegraph") {
		t.Errorf("stderr = %q, want mention of codegraph", stderr.String())
	}
}

// TestSetLocalEnvKey_CreatesFileWhenAbsent verifies that setLocalEnvKey
// creates the file with the new KEY=VALUE when it does not exist.
func TestSetLocalEnvKey_CreatesFileWhenAbsent(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "local.env")

	if err := setLocalEnvKey(path, "MY_KEY", "my_value"); err != nil {
		t.Fatalf("setLocalEnvKey err = %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "MY_KEY=my_value") {
		t.Errorf("file = %q, want MY_KEY=my_value", body)
	}
}

// TestSetLocalEnvKey_UpdatesExistingKey verifies that a second call replaces
// the old value rather than appending a duplicate line.
func TestSetLocalEnvKey_UpdatesExistingKey(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "local.env")

	if err := setLocalEnvKey(path, "MY_KEY", "first"); err != nil {
		t.Fatalf("first call err = %v", err)
	}
	if err := setLocalEnvKey(path, "MY_KEY", "second"); err != nil {
		t.Fatalf("second call err = %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.Contains(string(body), "first") {
		t.Errorf("file still contains old value 'first'; got:\n%s", body)
	}
	if !strings.Contains(string(body), "MY_KEY=second") {
		t.Errorf("file missing MY_KEY=second; got:\n%s", body)
	}
}

// TestSetLocalEnvKey_PreservesOtherKeys verifies that only the targeted key
// is replaced, leaving other keys intact.
func TestSetLocalEnvKey_PreservesOtherKeys(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	path := filepath.Join(tmp, "local.env")

	if err := setLocalEnvKey(path, "OTHER_KEY", "keep_me"); err != nil {
		t.Fatalf("setup err = %v", err)
	}
	if err := setLocalEnvKey(path, "MY_KEY", "new_value"); err != nil {
		t.Fatalf("update err = %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "OTHER_KEY=keep_me") {
		t.Errorf("OTHER_KEY was lost; got:\n%s", body)
	}
	if !strings.Contains(string(body), "MY_KEY=new_value") {
		t.Errorf("MY_KEY missing; got:\n%s", body)
	}
}

// TestRunCodegraphStatus_Unimplemented verifies the status subcommand
// returns a non-panic, informative response.
func TestRunCodegraphStatus_Unimplemented(t *testing.T) {
	t.Parallel()

	var stdout, stderr bytes.Buffer
	code := runCodegraph([]string{"status"}, &stdout, &stderr)
	// status must not return 0 if unimplemented, or return 0 with output.
	// Either way it must not panic and must produce output.
	combined := stdout.String() + stderr.String()
	if combined == "" {
		t.Error("expected output from `codegraph status`, got none")
	}
	_ = code
}
