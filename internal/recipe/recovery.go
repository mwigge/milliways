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
	"os"
	"path/filepath"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

// FailureStrategy defines how a recipe handles course failures.
type FailureStrategy string

const (
	StrategyStop        FailureStrategy = "stop"         // halt recipe (default)
	StrategyRetryCourse FailureStrategy = "retry-course" // retry failed course with backoff
	StrategySkipCourse  FailureStrategy = "skip-course"  // skip and continue
	StrategyRestartFrom FailureStrategy = "restart-from" // restart from N courses back

	retryBaseDelay   = 1 * time.Second
	retryMaxDelay    = 30 * time.Second
	retryMaxAttempts = 3
)

func retryBackoff(attempt int) time.Duration {
	base := retryBaseDelay * (1 << attempt)
	if base > retryMaxDelay {
		base = retryMaxDelay
	}
	jitter := time.Duration(attempt*13+7) * base / 100
	return base + jitter
}

// ParseStrategy converts a string to a FailureStrategy.
func ParseStrategy(s string) FailureStrategy {
	switch s {
	case "retry-course":
		return StrategyRetryCourse
	case "skip-course":
		return StrategySkipCourse
	case "restart-from":
		return StrategyRestartFrom
	default:
		return StrategyStop
	}
}

// RecoverableCourseError wraps a failed course result for recovery handling.
type RecoverableCourseError struct {
	CourseIndex int
	Step        Step
	Result      kitchen.Result
	OrigError   error
}

func (e *RecoverableCourseError) Error() string {
	return fmt.Sprintf("course %d (%s via %s) failed: %v", e.CourseIndex+1, e.Step.Station, e.Step.Kitchen, e.OrigError)
}

// HandleFailure applies a recovery strategy to a failed course.
// Returns true if the recipe should continue, false if it should stop.
func HandleFailure(ctx context.Context, strategy FailureStrategy, failedResult CourseResult, registry *kitchen.Registry, onLine func(string)) (bool, *CourseResult) {
	switch strategy {
	case StrategySkipCourse:
		return true, nil

	case StrategyRetryCourse:
		k, ok := registry.Get(failedResult.Step.Kitchen)
		if !ok || k.Status() != kitchen.Ready {
			return false, nil
		}

		lastResult := failedResult
		for attempt := 1; attempt <= retryMaxAttempts; attempt++ {
			delay := retryBackoff(attempt - 1)
			if onLine != nil {
				onLine(fmt.Sprintf("retrying in %v...", delay))
			}
			if err := waitForRetry(ctx, delay); err != nil {
				return false, nil
			}

			task := kitchen.Task{
				Prompt: fmt.Sprintf("Retry (previous attempt failed): %s", lastResult.Result.Output),
				OnLine: onLine,
			}

			start := time.Now()
			result, err := k.Exec(ctx, task)
			dur := time.Since(start)

			cr := &CourseResult{
				Step:         failedResult.Step,
				Index:        failedResult.Index,
				Result:       result,
				Error:        err,
				Duration:     dur,
				RetryAttempt: attempt,
			}

			if err == nil && result.ExitCode == 0 {
				return true, cr
			}

			lastResult = *cr
			if attempt == retryMaxAttempts {
				return false, cr
			}
		}

		return false, nil

	case StrategyStop:
		// Save partial output for user inspection
		savePartial(failedResult)
		return false, nil

	default:
		return false, nil
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// savePartial writes the partial recipe output to a file for later inspection.
func savePartial(result CourseResult) {
	dir := filepath.Join(os.TempDir(), "milliways-partial")
	_ = os.MkdirAll(dir, 0o700)

	path := filepath.Join(dir, fmt.Sprintf("course-%d-%s.txt", result.Index+1, result.Step.Station))
	content := fmt.Sprintf("Course %d: %s via %s\nStatus: failed\nExit: %d\n\n%s",
		result.Index+1, result.Step.Station, result.Step.Kitchen,
		result.Result.ExitCode, result.Result.Output)
	_ = os.WriteFile(path, []byte(content), 0o600)

	slog.Info("recipe partial output saved", "path", path)
}
