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
	"context"
	"strings"
	"testing"
	"time"
)

func TestRootTimeoutWithoutPromptExplainsScope(t *testing.T) {
	cmd := rootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--timeout", "1s"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for --timeout without prompt")
	}
	for _, want := range []string{"--timeout only applies to one-shot prompts", "interactive chat", "milliways --timeout 2m"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("timeout scope error missing %q:\n%s", want, err.Error())
		}
	}
}

func TestRootRejectsNegativeTimeout(t *testing.T) {
	cmd := rootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--timeout=-1s", "hello"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected negative timeout error")
	}
	if !strings.Contains(err.Error(), "--timeout must be zero or greater") {
		t.Fatalf("negative timeout error = %q", err.Error())
	}
}

func TestDispatchTimeoutContextZeroDisablesDeadline(t *testing.T) {
	ctx, cancel := dispatchTimeoutContext(context.Background(), 0)
	defer cancel()

	if _, ok := ctx.Deadline(); ok {
		t.Fatal("zero timeout should not install a context deadline")
	}
}

func TestDispatchTimeoutContextPositiveSetsDeadline(t *testing.T) {
	ctx, cancel := dispatchTimeoutContext(context.Background(), time.Minute)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("positive timeout should install a context deadline")
	}
	if time.Until(deadline) <= 0 {
		t.Fatalf("deadline is not in the future: %s", deadline)
	}
}

func TestHeadlessTimeoutReportIncludesActionableFields(t *testing.T) {
	report := headlessTimeoutReport(dispatchOpts{
		sessionName: "review",
		timeout:     2 * time.Minute,
	}, "codex", 90*time.Second)

	for _, want := range []string{"headless dispatch timed out", "2m0s", "codex", "--session review"} {
		msg, _ := report["message"].(string)
		if !strings.Contains(msg, want) {
			t.Fatalf("timeout message missing %q:\n%v", want, report)
		}
	}
	if report["error"] != "timeout" {
		t.Fatalf("error = %v, want timeout", report["error"])
	}
	if report["kitchen"] != "codex" {
		t.Fatalf("kitchen = %v, want codex", report["kitchen"])
	}
	if report["session"] != "review" {
		t.Fatalf("session = %v, want review", report["session"])
	}
	if report["timeout_s"] != float64(120) {
		t.Fatalf("timeout_s = %v, want 120", report["timeout_s"])
	}
}

func TestDispatchRejectsNegativeTimeoutBeforeLoadingConfig(t *testing.T) {
	err := dispatch(dispatchOpts{timeout: -time.Second})
	if err == nil {
		t.Fatal("expected negative timeout error")
	}
	if !strings.Contains(err.Error(), "--timeout must be zero or greater") {
		t.Fatalf("dispatch error = %q", err.Error())
	}
}
