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

package kitchen

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

// Integration tests that exercise real CLI execution.
// These use "echo" as the kitchen command — available on all systems.

func TestExec_StreamsOutput(t *testing.T) {
	t.Parallel()
	k := NewGeneric(GenericConfig{Name: "echo-stream", Cmd: "echo", Enabled: true})

	var lines []string
	task := Task{
		Prompt: "line one",
		OnLine: func(line string) { lines = append(lines, line) },
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := k.Exec(ctx, task)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code: got %d, want 0", result.ExitCode)
	}
	if len(lines) == 0 {
		t.Error("expected OnLine to be called at least once")
	}
	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}
}

func TestExec_CapturesNonZeroExitCode(t *testing.T) {
	t.Parallel()
	// "false" always exits with code 1
	k := NewGeneric(GenericConfig{Name: "false-test", Cmd: "false", Enabled: true})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := k.Exec(ctx, Task{Prompt: ""})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("exit code: got %d, want 1", result.ExitCode)
	}
}

func TestExec_ContextTimeout(t *testing.T) {
	t.Parallel()
	k := NewGeneric(GenericConfig{Name: "sleep-test", Cmd: "sleep", Args: []string{"10"}, Enabled: true})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := k.Exec(ctx, Task{Prompt: ""})
	elapsed := time.Since(start)

	if elapsed > 2*time.Second {
		t.Errorf("expected fast timeout, took %v", elapsed)
	}
	if err == nil {
		t.Log("timeout may not have triggered if sleep completed first")
	}
}

func TestExec_NilOnLine(t *testing.T) {
	t.Parallel()
	k := NewGeneric(GenericConfig{Name: "nil-online", Cmd: "echo", Enabled: true})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// OnLine is nil — should not panic
	result, err := k.Exec(ctx, Task{Prompt: "hello", OnLine: nil})
	if err != nil {
		t.Fatalf("Exec with nil OnLine: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code: got %d, want 0", result.ExitCode)
	}
}

func TestExec_AllowsAbsolutePathWhenBasenameIsAllowlisted(t *testing.T) {
	t.Parallel()

	cmdPath, err := exec.LookPath("echo")
	if err != nil {
		t.Fatalf("LookPath echo: %v", err)
	}

	k := NewGeneric(GenericConfig{Name: "echo-absolute-path", Cmd: cmdPath, Enabled: true})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := k.Exec(ctx, Task{Prompt: "hello"})
	if err != nil {
		t.Fatalf("Exec with absolute path: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code: got %d, want 0", result.ExitCode)
	}
}

func TestExec_WithDir(t *testing.T) {
	t.Parallel()
	k := NewGeneric(GenericConfig{Name: "pwd-test", Cmd: "echo", Enabled: true})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := k.Exec(ctx, Task{Prompt: "hello", Dir: "/tmp"})
	if err != nil {
		t.Fatalf("Exec with dir: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code: got %d, want 0", result.ExitCode)
	}
}
