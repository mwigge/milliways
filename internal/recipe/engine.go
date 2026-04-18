package recipe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
)

// Step defines one course in a recipe.
type Step struct {
	Station string `yaml:"station" json:"station"`
	Kitchen string `yaml:"kitchen" json:"kitchen"`
}

// CourseResult captures the outcome of one recipe course.
type CourseResult struct {
	Step     Step
	Index    int
	Result   kitchen.Result
	Error    error
	Duration time.Duration
}

// ContextFile is the JSON written between courses for context handoff.
type ContextFile struct {
	Course   int    `json:"course"`
	Kitchen  string `json:"kitchen"`
	Station  string `json:"station"`
	Task     string `json:"task"`
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

// Engine executes multi-course recipes sequentially.
type Engine struct {
	registry    *kitchen.Registry
	keepContext bool
}

// NewEngine creates a recipe execution engine.
func NewEngine(registry *kitchen.Registry, keepContext bool) *Engine {
	return &Engine{registry: registry, keepContext: keepContext}
}

// Execute runs a recipe: dispatches each step sequentially, passing context forward.
// Returns results for each course and stops on first failure unless continueOnError is set.
func (e *Engine) Execute(ctx context.Context, steps []Step, prompt string, onLine func(string), onCourse func(int, Step, string)) ([]CourseResult, error) {
	if len(steps) == 0 {
		return nil, fmt.Errorf("recipe has no steps")
	}

	recipeID := fmt.Sprintf("%d", time.Now().UnixNano())
	var results []CourseResult
	var prevContext string

	for i, step := range steps {
		if onCourse != nil {
			onCourse(i, step, "running")
		}

		k, ok := e.registry.Get(step.Kitchen)
		if !ok {
			err := fmt.Errorf("kitchen %q not found for course %d (%s)", step.Kitchen, i+1, step.Station)
			results = append(results, CourseResult{Step: step, Index: i, Error: err})
			if onCourse != nil {
				onCourse(i, step, "skipped")
			}
			continue
		}

		if k.Status() != kitchen.Ready {
			err := fmt.Errorf("kitchen %q not ready for course %d (%s): %s", step.Kitchen, i+1, step.Station, k.Status())
			results = append(results, CourseResult{Step: step, Index: i, Error: err})
			if onCourse != nil {
				onCourse(i, step, "skipped")
			}
			continue
		}

		// Build prompt with context from previous course
		coursePrompt := prompt
		if prevContext != "" {
			prevContext = sanitizePromptInjection(prevContext)
			coursePrompt = fmt.Sprintf("Previous course (%s) output:\n%s\n\nTask: %s", steps[i-1].Station, prevContext, prompt)
		}

		task := kitchen.Task{
			Prompt: coursePrompt,
			OnLine: onLine,
		}

		start := time.Now()
		result, execErr := k.Exec(ctx, task)
		dur := time.Since(start)

		cr := CourseResult{
			Step:     step,
			Index:    i,
			Result:   result,
			Error:    execErr,
			Duration: dur,
		}
		results = append(results, cr)

		// Write context file for handoff
		contextPath := filepath.Join(os.TempDir(), fmt.Sprintf("milliways-%s-%d.json", recipeID, i))
		cf := ContextFile{
			Course:   i,
			Kitchen:  step.Kitchen,
			Station:  step.Station,
			Task:     prompt,
			Output:   result.Output,
			ExitCode: result.ExitCode,
		}
		if data, err := json.MarshalIndent(cf, "", "  "); err == nil {
			_ = os.WriteFile(contextPath, data, 0o600)
		}

		prevContext = result.Output

		if execErr != nil {
			if onCourse != nil {
				onCourse(i, step, "failed")
			}
			return results, fmt.Errorf("course %d (%s via %s) failed: %w", i+1, step.Station, step.Kitchen, execErr)
		}

		if result.ExitCode != 0 {
			if onCourse != nil {
				onCourse(i, step, "failed")
			}
			return results, fmt.Errorf("course %d (%s via %s) exited with code %d", i+1, step.Station, step.Kitchen, result.ExitCode)
		}

		if onCourse != nil {
			onCourse(i, step, "done")
		}
	}

	// Cleanup context files unless --keep-context
	if !e.keepContext {
		for i := range steps {
			contextPath := filepath.Join(os.TempDir(), fmt.Sprintf("milliways-%s-%d.json", recipeID, i))
			_ = os.Remove(contextPath)
		}
	}

	return results, nil
}
