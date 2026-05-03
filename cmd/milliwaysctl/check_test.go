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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunCheck_PrintsHeader verifies that the output contains the canonical
// title line regardless of the environment.
func TestRunCheck_PrintsHeader(t *testing.T) {
	var stdout bytes.Buffer
	runCheck(nil, &stdout, &bytes.Buffer{})
	if !strings.Contains(stdout.String(), "milliwaysctl check") {
		t.Errorf("expected header in output; got:\n%s", stdout.String())
	}
}

// TestRunCheck_AllFAIL_ExitsOne verifies that runCheck returns 1 when at
// least one item is [FAIL].
func TestRunCheck_AllFAIL_ExitsOne(t *testing.T) {
	// Blank PATH so no binary can be found and no Python venv exists.
	t.Setenv("PATH", "")
	t.Setenv("HOME", t.TempDir()) // clean home — no ~/.local/share tree

	var stdout bytes.Buffer
	rc := runCheck(nil, &stdout, &bytes.Buffer{})
	if rc != 1 {
		t.Errorf("expected rc=1 when all items fail, got %d\noutput:\n%s", rc, stdout.String())
	}
}

// TestRunCheck_ContainsKnownSections verifies that all major check sections
// appear in the output.
func TestRunCheck_ContainsKnownSections(t *testing.T) {
	var stdout bytes.Buffer
	runCheck(nil, &stdout, &bytes.Buffer{})
	out := stdout.String()

	for _, want := range []string{
		"milliways binary",
		"milliwaysd binary",
		"milliwaysctl binary",
		"Python venv",
		"MemPalace",
		"python-pptx",
		"CodeGraph",
		"Agent toolkit",
		"ANTHROPIC_API_KEY",
		"GEMINI_API_KEY",
		"OPENAI_API_KEY",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected section %q in output; got:\n%s", want, out)
		}
	}
}

// TestRunCheck_APIKeySet verifies that a set API key is shown as "set" and
// a missing one as "not set".
func TestRunCheck_APIKeySet(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")
	t.Setenv("GEMINI_API_KEY", "")

	var stdout bytes.Buffer
	runCheck(nil, &stdout, &bytes.Buffer{})
	out := stdout.String()

	// ANTHROPIC_API_KEY should show [PASS] and "set"
	if !strings.Contains(out, "ANTHROPIC_API_KEY") {
		t.Errorf("ANTHROPIC_API_KEY line missing; output:\n%s", out)
	}

	// Find the ANTHROPIC_API_KEY line and verify it says "set"
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "ANTHROPIC_API_KEY") {
			if !strings.Contains(line, "set") {
				t.Errorf("ANTHROPIC_API_KEY line should show 'set'; got: %q", line)
			}
			if strings.Contains(line, "not set") {
				t.Errorf("ANTHROPIC_API_KEY line should not show 'not set'; got: %q", line)
			}
		}
	}
}

// TestRunCheck_LocalServerReachable verifies that a live local server endpoint
// is reported as "reachable".
func TestRunCheck_LocalServerReachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", srv.URL)

	var stdout bytes.Buffer
	runCheck(nil, &stdout, &bytes.Buffer{})
	out := stdout.String()

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Local server") {
			if !strings.Contains(line, "reachable") {
				t.Errorf("local server line should say 'reachable'; got: %q", line)
			}
			return
		}
	}
	t.Errorf("Local server line not found in output:\n%s", out)
}

// TestRunCheck_LocalServerNotReachable verifies that an unreachable local
// server endpoint is reported as "not reachable".
func TestRunCheck_LocalServerNotReachable(t *testing.T) {
	// Port 1 is reserved and will refuse connections.
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "http://127.0.0.1:1")

	var stdout bytes.Buffer
	runCheck(nil, &stdout, &bytes.Buffer{})
	out := stdout.String()

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Local server") {
			if !strings.Contains(line, "not reachable") {
				t.Errorf("local server line should say 'not reachable'; got: %q", line)
			}
			return
		}
	}
	t.Errorf("Local server line not found in output:\n%s", out)
}

// TestRunCheck_LocalServerNotConfigured verifies that when
// MILLIWAYS_LOCAL_ENDPOINT is unset the check reports "not configured".
func TestRunCheck_LocalServerNotConfigured(t *testing.T) {
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "")

	var stdout bytes.Buffer
	runCheck(nil, &stdout, &bytes.Buffer{})
	out := stdout.String()

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "Local server") {
			if !strings.Contains(line, "not configured") {
				t.Errorf("local server line should say 'not configured' when endpoint unset; got: %q", line)
			}
			return
		}
	}
	t.Errorf("Local server line not found in output:\n%s", out)
}

// TestRunCheck_OtelEndpointValid verifies that a valid OTel endpoint URL is
// accepted without warning.
func TestRunCheck_OtelEndpointValid(t *testing.T) {
	t.Setenv("MILLIWAYS_OTEL_ENDPOINT", "http://localhost:4318")

	var stdout bytes.Buffer
	runCheck(nil, &stdout, &bytes.Buffer{})
	out := stdout.String()

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "OTel endpoint") {
			if strings.Contains(line, "FAIL") {
				t.Errorf("valid OTel endpoint should not be [FAIL]; got: %q", line)
			}
			return
		}
	}
	t.Errorf("OTel endpoint line not found in output:\n%s", out)
}

// TestRunCheck_OtelEndpointInvalid verifies that a malformed OTel endpoint URL
// is reported as [WARN] or [FAIL].
func TestRunCheck_OtelEndpointInvalid(t *testing.T) {
	t.Setenv("MILLIWAYS_OTEL_ENDPOINT", "://not-a-url")

	var stdout bytes.Buffer
	runCheck(nil, &stdout, &bytes.Buffer{})
	out := stdout.String()

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "OTel endpoint") {
			if !strings.Contains(line, "WARN") && !strings.Contains(line, "FAIL") {
				t.Errorf("invalid OTel endpoint should be [WARN] or [FAIL]; got: %q", line)
			}
			return
		}
	}
	t.Errorf("OTel endpoint line not found in output:\n%s", out)
}

// TestRunCheck_BinaryFound verifies that milliwaysctl itself (the binary under
// test) can be found on PATH when PATH is set up appropriately.
func TestRunCheck_BinaryFound(t *testing.T) {
	// Create a temp directory with a fake "milliwaysctl" binary.
	dir := t.TempDir()
	fake := filepath.Join(dir, "milliwaysctl")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho milliwaysctl-fake"), 0o755); err != nil {
		t.Fatalf("create fake binary: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	var stdout bytes.Buffer
	runCheck(nil, &stdout, &bytes.Buffer{})
	out := stdout.String()

	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "milliwaysctl binary") {
			if !strings.Contains(line, "PASS") {
				t.Errorf("milliwaysctl binary should be [PASS] when on PATH; got: %q", line)
			}
			return
		}
	}
	t.Errorf("milliwaysctl binary line not found in output:\n%s", out)
}

// TestRunCheck_AllWarnOrPass_ExitsZero verifies that exit code is 0 when no
// item is [FAIL].
func TestRunCheck_AllWarnOrPass_ExitsZero(t *testing.T) {
	// Set up PATH with all three required binaries present.
	dir := t.TempDir()
	for _, name := range []string{"milliways", "milliwaysd", "milliwaysctl"} {
		fake := filepath.Join(dir, name)
		if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0"), 0o755); err != nil {
			t.Fatalf("create fake %s: %v", name, err)
		}
	}

	// Create fake support scripts in a temp XDG_DATA_HOME.
	dataDir := t.TempDir()
	scriptDir := filepath.Join(dataDir, "milliways", "scripts")
	if err := os.MkdirAll(scriptDir, 0o755); err != nil {
		t.Fatalf("create script dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(scriptDir, "install_local.sh"), []byte("#!/bin/sh\necho ok"), 0o755); err != nil {
		t.Fatalf("create fake script: %v", err)
	}

	// Unset everything that could cause a FAIL.
	t.Setenv("PATH", dir)
	t.Setenv("XDG_DATA_HOME", dataDir)
	t.Setenv("MILLIWAYS_LOCAL_ENDPOINT", "")
	t.Setenv("MILLIWAYS_OTEL_ENDPOINT", "")
	t.Setenv("MILLIWAYS_CODEGRAPH_MCP_CMD", "")
	t.Setenv("MILLIWAYS_AGENTS_DIR", "")
	t.Setenv("HOME", t.TempDir())

	var stdout bytes.Buffer
	rc := runCheck(nil, &stdout, &bytes.Buffer{})
	if rc != 0 {
		t.Errorf("expected rc=0 when no [FAIL] items, got %d\noutput:\n%s", rc, stdout.String())
	}
}
