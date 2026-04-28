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

package orchestrator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mwigge/milliways/internal/observability"
)

func TestTraceDelegateEmitsSuccessTrace(t *testing.T) {
	t.Parallel()

	emitter, err := observability.NewTraceEmitterForDir("delegate-success", t.TempDir())
	if err != nil {
		t.Fatalf("NewTraceEmitterForDir() error = %v", err)
	}

	result, err := traceDelegate(context.Background(), emitter, func(context.Context, string, string, string) (string, error) {
		return "done", nil
	}, "coder-go", "/repo", "implement tests")
	if err != nil {
		t.Fatalf("traceDelegate() error = %v", err)
	}
	if result != "done" {
		t.Fatalf("result = %q, want done", result)
	}

	events, err := observability.ReadTraceFile(emitter.TraceFilePath())
	if err != nil {
		t.Fatalf("ReadTraceFile() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].Type != "agent.think" {
		t.Fatalf("first event = %q, want agent.think", events[0].Type)
	}
	if events[1].Type != "agent.delegate" {
		t.Fatalf("second event = %q, want agent.delegate", events[1].Type)
	}
	if got := events[1].Data["outcome"]; got != "pass" {
		t.Fatalf("outcome = %v, want pass", got)
	}
}

func TestTraceDelegateEmitsDecisionForFailure(t *testing.T) {
	t.Parallel()

	emitter, err := observability.NewTraceEmitterForDir("delegate-failure", t.TempDir())
	if err != nil {
		t.Fatalf("NewTraceEmitterForDir() error = %v", err)
	}

	_, err = traceDelegate(context.Background(), emitter, func(context.Context, string, string, string) (string, error) {
		return "boom", errors.New("delegate failed")
	}, "coder-go", "/repo", "implement tests")
	if err == nil {
		t.Fatal("expected error")
	}

	events, err := observability.ReadTraceFile(emitter.TraceFilePath())
	if err != nil {
		t.Fatalf("ReadTraceFile() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("events = %d, want 3", len(events))
	}
	if events[2].Type != "agent.decide" {
		t.Fatalf("third event = %q, want agent.decide", events[2].Type)
	}
	if got := events[1].Data["outcome"]; got != "fail" {
		t.Fatalf("outcome = %v, want fail", got)
	}
	if got := events[2].Data["next_action"]; !strings.Contains(got.(string), "retry") {
		t.Fatalf("next action = %v, want retry guidance", got)
	}
}

func TestTraceDelegateMarksStallAsRework(t *testing.T) {
	t.Parallel()

	emitter, err := observability.NewTraceEmitterForDir("delegate-stall", t.TempDir())
	if err != nil {
		t.Fatalf("NewTraceEmitterForDir() error = %v", err)
	}

	result, err := traceDelegate(context.Background(), emitter, func(context.Context, string, string, string) (string, error) {
		return "no commits appear for 300s", nil
	}, "coder-go", "/repo", "implement tests")
	if err != nil {
		t.Fatalf("traceDelegate() error = %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result")
	}

	events, err := observability.ReadTraceFile(emitter.TraceFilePath())
	if err != nil {
		t.Fatalf("ReadTraceFile() error = %v", err)
	}
	if got := events[1].Data["outcome"]; got != "rework" {
		t.Fatalf("outcome = %v, want rework", got)
	}
}
