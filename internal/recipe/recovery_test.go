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

package recipe

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

func TestParseStrategy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  FailureStrategy
	}{
		{"stop", StrategyStop},
		{"retry-course", StrategyRetryCourse},
		{"skip-course", StrategySkipCourse},
		{"restart-from", StrategyRestartFrom},
		{"unknown", StrategyStop},
		{"", StrategyStop},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			if got := ParseStrategy(tt.input); got != tt.want {
				t.Errorf("ParseStrategy(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestHandleFailure_SkipCourse(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	failed := CourseResult{Step: Step{Station: "fail", Kitchen: "test"}, Index: 1}

	shouldContinue, retryResult := HandleFailure(context.Background(), StrategySkipCourse, failed, reg, nil)
	if !shouldContinue {
		t.Error("skip-course should continue")
	}
	if retryResult != nil {
		t.Error("skip-course should not return a retry result")
	}
}

func TestHandleFailure_Stop(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	capture := installRecoveryTestLogger(t)
	failed := CourseResult{
		Step:   Step{Station: "fail", Kitchen: "test"},
		Index:  0,
		Result: kitchen.Result{ExitCode: 1, Output: "error output"},
	}

	shouldContinue, _ := HandleFailure(context.Background(), StrategyStop, failed, reg, nil)
	if shouldContinue {
		t.Error("stop should not continue")
	}

	records := capture.records()
	if len(records) != 1 {
		t.Fatalf("expected 1 log record, got %d", len(records))
	}
	if records[0].Level != slog.LevelInfo {
		t.Fatalf("log level = %v, want %v", records[0].Level, slog.LevelInfo)
	}
	if records[0].Message != "recipe partial output saved" {
		t.Fatalf("log message = %q, want %q", records[0].Message, "recipe partial output saved")
	}
	path, ok := records[0].Attrs["path"].(string)
	if !ok {
		t.Fatalf("path attr type = %T, want string", records[0].Attrs["path"])
	}
	if !strings.Contains(path, filepath.Join("milliways-partial", "course-1-fail.txt")) {
		t.Fatalf("path attr = %q, want partial output path", path)
	}
}

func TestHandleFailure_RetryCourse_Success(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo-test", Cmd: "echo", Enabled: true}))

	failed := CourseResult{
		Step:   Step{Station: "code", Kitchen: "echo-test"},
		Index:  1,
		Result: kitchen.Result{ExitCode: 1, Output: "first attempt failed"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shouldContinue, retryResult := HandleFailure(ctx, StrategyRetryCourse, failed, reg, nil)
	if !shouldContinue {
		t.Error("retry succeeded — should continue")
	}
	if retryResult == nil {
		t.Fatal("expected retry result")
	}
	if retryResult.Result.ExitCode != 0 {
		t.Errorf("retry exit code: %d", retryResult.Result.ExitCode)
	}
	if retryResult.RetryAttempt != 1 {
		t.Errorf("retry attempt = %d, want 1", retryResult.RetryAttempt)
	}
}

func TestHandleFailure_RetryCourse_RetriesWithBackoff(t *testing.T) {
	t.Parallel()

	reg := kitchen.NewRegistry()
	retryKitchen := &retryRecoveryKitchen{
		name: "retry-test",
		results: []kitchen.Result{
			{ExitCode: 1, Output: "retry failed once"},
			{ExitCode: 0, Output: "retry succeeded"},
		},
	}
	reg.Register(retryKitchen)

	failed := CourseResult{
		Step:         Step{Station: "code", Kitchen: "retry-test"},
		Index:        1,
		Result:       kitchen.Result{ExitCode: 1, Output: "initial failure"},
		RetryAttempt: 0,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	shouldContinue, retryResult := HandleFailure(ctx, StrategyRetryCourse, failed, reg, nil)
	elapsed := time.Since(start)

	if !shouldContinue {
		t.Fatal("retry succeeded — should continue")
	}
	if retryResult == nil {
		t.Fatal("expected retry result")
	}
	if retryResult.Result.ExitCode != 0 {
		t.Fatalf("retry exit code = %d, want 0", retryResult.Result.ExitCode)
	}
	if retryResult.RetryAttempt != 2 {
		t.Fatalf("retry attempt = %d, want 2", retryResult.RetryAttempt)
	}
	if retryKitchen.calls() != 2 {
		t.Fatalf("retry calls = %d, want 2", retryKitchen.calls())
	}
	if elapsed < retryBackoff(0) {
		t.Fatalf("elapsed = %v, want at least %v", elapsed, retryBackoff(0))
	}
	if got := retryKitchen.prompts(); len(got) != 2 {
		t.Fatalf("prompt count = %d, want 2", len(got))
	} else if !strings.Contains(got[1], "retry failed once") {
		t.Fatalf("second prompt = %q, want latest retry failure output", got[1])
	}
}

func TestHandleFailure_RetryCourse_KitchenUnavailable(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	// Kitchen not registered

	failed := CourseResult{
		Step:  Step{Station: "code", Kitchen: "missing"},
		Index: 0,
	}

	shouldContinue, _ := HandleFailure(context.Background(), StrategyRetryCourse, failed, reg, nil)
	if shouldContinue {
		t.Error("retry with missing kitchen should not continue")
	}
}

var recoveryTestLoggerMu sync.Mutex

type recoveryTestLogRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]any
}

type recoveryTestLogCapture struct {
	mu      sync.Mutex
	entries []recoveryTestLogRecord
}

type retryRecoveryKitchen struct {
	mu              sync.Mutex
	name            string
	results         []kitchen.Result
	recordedPrompts []string
	idx             int
}

func installRecoveryTestLogger(t *testing.T) *recoveryTestLogCapture {
	t.Helper()
	recoveryTestLoggerMu.Lock()
	capture := &recoveryTestLogCapture{}
	previous := slog.Default()
	slog.SetDefault(slog.New(capture))
	t.Cleanup(func() {
		slog.SetDefault(previous)
		recoveryTestLoggerMu.Unlock()
	})
	return capture
}

func (c *recoveryTestLogCapture) Enabled(context.Context, slog.Level) bool { return true }

func (c *recoveryTestLogCapture) Handle(_ context.Context, record slog.Record) error {
	attrs := make(map[string]any, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		attrs[attr.Key] = attr.Value.Any()
		return true
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = append(c.entries, recoveryTestLogRecord{Level: record.Level, Message: record.Message, Attrs: attrs})
	return nil
}

func (c *recoveryTestLogCapture) WithAttrs(_ []slog.Attr) slog.Handler { return c }

func (c *recoveryTestLogCapture) WithGroup(string) slog.Handler { return c }

func (c *recoveryTestLogCapture) records() []recoveryTestLogRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	clone := make([]recoveryTestLogRecord, len(c.entries))
	copy(clone, c.entries)
	return clone
}

func (k *retryRecoveryKitchen) Name() string {
	return k.name
}

func (k *retryRecoveryKitchen) Exec(_ context.Context, task kitchen.Task) (kitchen.Result, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.recordedPrompts = append(k.recordedPrompts, task.Prompt)
	if k.idx >= len(k.results) {
		return kitchen.Result{}, fmt.Errorf("unexpected retry call %d", k.idx+1)
	}
	result := k.results[k.idx]
	k.idx++
	return result, nil
}

func (k *retryRecoveryKitchen) Stations() []string {
	return []string{"code"}
}

func (k *retryRecoveryKitchen) CostTier() kitchen.CostTier {
	return kitchen.Local
}

func (k *retryRecoveryKitchen) Status() kitchen.Status {
	return kitchen.Ready
}

func (k *retryRecoveryKitchen) calls() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.idx
}

func (k *retryRecoveryKitchen) prompts() []string {
	k.mu.Lock()
	defer k.mu.Unlock()
	clone := make([]string, len(k.recordedPrompts))
	copy(clone, k.recordedPrompts)
	return clone
}

var _ kitchen.Kitchen = (*retryRecoveryKitchen)(nil)
