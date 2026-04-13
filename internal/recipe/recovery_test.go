package recipe

import (
	"context"
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
	failed := CourseResult{
		Step:   Step{Station: "fail", Kitchen: "test"},
		Index:  0,
		Result: kitchen.Result{ExitCode: 1, Output: "error output"},
	}

	shouldContinue, _ := HandleFailure(context.Background(), StrategyStop, failed, reg, nil)
	if shouldContinue {
		t.Error("stop should not continue")
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
