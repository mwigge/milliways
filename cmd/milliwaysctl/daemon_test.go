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
	"syscall"
	"testing"
)

func TestRunDaemonStopMissingPidIsIdempotent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runDaemon([]string{"--state", t.TempDir(), "stop"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runDaemon stop = %d, stderr=%q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "not running") {
		t.Fatalf("stdout = %q, want not running", stdout.String())
	}
}

func TestRunDaemonStopSignalsPid(t *testing.T) {
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "pid"), []byte("12345\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	oldSignal := daemonSignalProcess
	oldAlive := daemonProcessAlive
	t.Cleanup(func() {
		daemonSignalProcess = oldSignal
		daemonProcessAlive = oldAlive
	})
	var gotPID int
	var gotSig os.Signal
	daemonSignalProcess = func(pid int, sig os.Signal) error {
		gotPID = pid
		gotSig = sig
		return nil
	}
	checks := 0
	daemonProcessAlive = func(pid int) bool {
		checks++
		return checks == 1
	}

	var stdout, stderr bytes.Buffer
	code := runDaemon([]string{"--state", state, "stop"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("runDaemon stop = %d, stderr=%q", code, stderr.String())
	}
	if gotPID != 12345 || gotSig != syscall.SIGTERM {
		t.Fatalf("signal = (%d,%v), want (12345,SIGTERM)", gotPID, gotSig)
	}
	if !strings.Contains(stdout.String(), "stopped milliwaysd pid 12345") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestDaemonProcessAliveTreatsPermissionDeniedAsAlive(t *testing.T) {
	oldProbe := daemonProbeProcess
	oldAlive := daemonProcessAlive
	t.Cleanup(func() {
		daemonProbeProcess = oldProbe
		daemonProcessAlive = oldAlive
	})

	daemonProbeProcess = func(pid int) error { return syscall.EPERM }
	daemonProcessAlive = oldAlive
	if !daemonProcessAlive(12345) {
		t.Fatal("permission-denied process probe should be treated as alive")
	}
}

func TestRunDaemonStopInvalidPid(t *testing.T) {
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "pid"), []byte("nope\n"), 0o600); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := runDaemon([]string{"--state", state, "stop"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("runDaemon stop = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "invalid pid file") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
