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
	"os/exec"
	"strings"
	"testing"
)

func TestRunInstall_NoArgsPrintsUsage(t *testing.T) {
	var stdout bytes.Buffer
	if rc := runInstall(nil, &stdout, &bytes.Buffer{}); rc != 0 {
		t.Fatalf("expected rc=0 for no args, got %d", rc)
	}
	if !strings.Contains(stdout.String(), "milliwaysctl install <client>") {
		t.Errorf("usage missing canonical line; got:\n%s", stdout.String())
	}
}

func TestRunInstall_UnknownClient(t *testing.T) {
	var stderr bytes.Buffer
	if rc := runInstall([]string{"bogus"}, &bytes.Buffer{}, &stderr); rc != 2 {
		t.Errorf("expected rc=2 for unknown client, got %d", rc)
	}
	if !strings.Contains(stderr.String(), "unknown client") {
		t.Errorf("stderr should explain the unknown client; got:\n%s", stderr.String())
	}
}

func TestRunInstall_HTTPOnlyClient_PrintsInfo(t *testing.T) {
	for _, name := range []string{"minimax", "pool"} {
		t.Run(name, func(t *testing.T) {
			var stdout bytes.Buffer
			if rc := runInstall([]string{name}, &stdout, &bytes.Buffer{}); rc != 0 {
				t.Fatalf("expected rc=0 for HTTP-only %s, got %d", name, rc)
			}
			if stdout.Len() == 0 {
				t.Errorf("expected info output for %s, got nothing", name)
			}
		})
	}
}

func TestRunInstall_PrereqMissing_ReturnsError(t *testing.T) {
	// Force LookPath to fail by shadowing $PATH for the duration of the
	// test. exec.LookPath honours PATH, so an empty PATH guarantees no
	// "npm" binary is found regardless of the host setup.
	t.Setenv("PATH", "")
	var stderr bytes.Buffer
	rc := runInstall([]string{"claude"}, &bytes.Buffer{}, &stderr)
	if rc == 0 {
		t.Errorf("expected non-zero rc when prereq missing, got 0")
	}
	if !strings.Contains(stderr.String(), "prerequisite") {
		t.Errorf("stderr should mention missing prerequisite; got:\n%s", stderr.String())
	}
}

func TestRunInstall_ListEnumeratesAllClients(t *testing.T) {
	var stdout bytes.Buffer
	runInstall([]string{"list"}, &stdout, &bytes.Buffer{})
	for _, want := range []string{"claude", "codex", "copilot", "gemini", "local", "minimax", "pool"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("install list missing %q; got:\n%s", want, stdout.String())
		}
	}
}

// TestRunInstall_AlreadyInstalled — when `<client> --version` exits 0,
// runInstall should short-circuit and not invoke the install command.
// We stub execCommand so the "check" returns success and capture
// whether the install argv ever got called.
func TestRunInstall_AlreadyInstalled(t *testing.T) {
	calls := 0
	prevExec := execCommand
	t.Cleanup(func() { execCommand = prevExec })
	execCommand = func(name string, args ...string) *exec.Cmd {
		calls++
		// `true` is a real binary on every Unix; exits 0.
		return exec.Command("true")
	}

	var stdout bytes.Buffer
	rc := runInstall([]string{"claude"}, &stdout, &bytes.Buffer{})
	if rc != 0 {
		t.Fatalf("expected rc=0 when already installed, got %d", rc)
	}
	// We expect exactly one call (the check). If runInstall went on to
	// call the install argv we'd see ≥2.
	if calls != 1 {
		t.Errorf("expected 1 execCommand call (check only), got %d", calls)
	}
	if !strings.Contains(stdout.String(), "already installed") {
		t.Errorf("stdout should report already-installed; got:\n%s", stdout.String())
	}
}
