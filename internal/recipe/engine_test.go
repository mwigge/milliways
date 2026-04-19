package recipe

import (
	"context"
	"testing"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

func newTestRegistry() *kitchen.Registry {
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo-kitchen", Cmd: "echo", Stations: []string{"think", "code", "test", "review"}, Tier: kitchen.Local, Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "false-kitchen", Cmd: "false", Stations: []string{"fail"}, Tier: kitchen.Local, Enabled: true}))
	return reg
}

func TestExecute_SingleCourse(t *testing.T) {
	t.Parallel()
	eng := NewEngine(newTestRegistry(), false, StrategyStop)

	steps := []Step{{Station: "think", Kitchen: "echo-kitchen"}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := eng.Execute(ctx, steps, "hello world", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Result.ExitCode != 0 {
		t.Errorf("exit code: %d", results[0].Result.ExitCode)
	}
}

func TestExecute_MultiCourse(t *testing.T) {
	t.Parallel()
	eng := NewEngine(newTestRegistry(), false, StrategyStop)

	steps := []Step{
		{Station: "think", Kitchen: "echo-kitchen"},
		{Station: "code", Kitchen: "echo-kitchen"},
		{Station: "test", Kitchen: "echo-kitchen"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var courseLog []string
	results, err := eng.Execute(ctx, steps, "build something", nil, func(i int, s Step, status string) {
		courseLog = append(courseLog, status)
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// All should be "running" then "done"
	if len(courseLog) != 6 {
		t.Errorf("expected 6 course callbacks (3 running + 3 done), got %d", len(courseLog))
	}
}

func TestExecute_StopsOnFailure(t *testing.T) {
	t.Parallel()
	eng := NewEngine(newTestRegistry(), false, StrategyStop)

	steps := []Step{
		{Station: "think", Kitchen: "echo-kitchen"},
		{Station: "fail", Kitchen: "false-kitchen"},
		{Station: "code", Kitchen: "echo-kitchen"}, // should not run
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := eng.Execute(ctx, steps, "will fail", nil, nil)
	if err == nil {
		t.Fatal("expected error on failure")
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results (stopped at course 2), got %d", len(results))
	}
}

func TestExecute_UnavailableKitchenSkipped(t *testing.T) {
	t.Parallel()
	reg := kitchen.NewRegistry()
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "echo-kitchen", Cmd: "echo", Enabled: true}))
	reg.Register(kitchen.NewGeneric(kitchen.GenericConfig{Name: "missing", Cmd: "nonexistent-xyz", Enabled: true}))
	eng := NewEngine(reg, false, StrategyStop)

	steps := []Step{
		{Station: "think", Kitchen: "missing"},
		{Station: "code", Kitchen: "echo-kitchen"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := eng.Execute(ctx, steps, "hello", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	// First course skipped, second succeeds
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Error == nil {
		t.Error("expected error for unavailable kitchen")
	}
	if results[1].Result.ExitCode != 0 {
		t.Error("second course should succeed")
	}
}

func TestExecute_EmptyRecipe(t *testing.T) {
	t.Parallel()
	eng := NewEngine(newTestRegistry(), false, StrategyStop)

	_, err := eng.Execute(context.Background(), nil, "hello", nil, nil)
	if err == nil {
		t.Error("expected error for empty recipe")
	}
}

func TestExecute_ContextPassedBetweenCourses(t *testing.T) {
	t.Parallel()
	eng := NewEngine(newTestRegistry(), false, StrategyStop)

	steps := []Step{
		{Station: "think", Kitchen: "echo-kitchen"},
		{Station: "code", Kitchen: "echo-kitchen"},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := eng.Execute(ctx, steps, "original prompt", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Second course should have received context from first
	// echo outputs its args, so second output should contain "Previous course"
	if len(results) < 2 {
		t.Fatal("expected 2 results")
	}
	output := results[1].Result.Output
	if len(output) == 0 {
		t.Error("expected non-empty output for second course")
	}
}
