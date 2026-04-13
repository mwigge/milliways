package recipe

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

// FailureStrategy defines how a recipe handles course failures.
type FailureStrategy string

const (
	StrategyStop        FailureStrategy = "stop"         // halt recipe (default)
	StrategyRetryCourse FailureStrategy = "retry-course" // retry failed course once
	StrategySkipCourse  FailureStrategy = "skip-course"  // skip and continue
	StrategyRestartFrom FailureStrategy = "restart-from" // restart from N courses back
)

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
		// Retry the same course once
		k, ok := registry.Get(failedResult.Step.Kitchen)
		if !ok || k.Status() != kitchen.Ready {
			return false, nil
		}

		task := kitchen.Task{
			Prompt: fmt.Sprintf("Retry (previous attempt failed): %s", failedResult.Result.Output),
			OnLine: onLine,
		}

		start := time.Now()
		result, err := k.Exec(ctx, task)
		dur := time.Since(start)

		cr := &CourseResult{
			Step:     failedResult.Step,
			Index:    failedResult.Index,
			Result:   result,
			Error:    err,
			Duration: dur,
		}

		if err != nil || result.ExitCode != 0 {
			return false, cr // retry also failed
		}
		return true, cr

	case StrategyStop:
		// Save partial output for user inspection
		savePartial(failedResult)
		return false, nil

	default:
		return false, nil
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

	fmt.Fprintf(os.Stderr, "[recipe] partial output saved to %s\n", path)
}
