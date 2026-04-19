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
	Step         Step
	Index        int
	Result       kitchen.Result
	Error        error
	Duration     time.Duration
	RetryAttempt int
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
	registry        *kitchen.Registry
	keepContext     bool
	failureStrategy FailureStrategy
}

// NewEngine creates a recipe execution engine.
func NewEngine(registry *kitchen.Registry, keepContext bool, strategy FailureStrategy) *Engine {
	return &Engine{registry: registry, keepContext: keepContext, failureStrategy: strategy}
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
			Step:         step,
			Index:        i,
			Result:       result,
			Error:        execErr,
			Duration:     dur,
			RetryAttempt: 0,
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

		if execErr != nil || result.ExitCode != 0 {
			if onCourse != nil {
				onCourse(i, step, "failed")
			}

			shouldContinue, retryResult := HandleFailure(ctx, e.failureStrategy, cr, e.registry, onLine)
			if retryResult != nil {
				results = append(results, *retryResult)
			}
			if !shouldContinue {
				if retryResult != nil {
					return results, fmt.Errorf("course %d (%s via %s) failed after retries: %w", i+1, step.Station, step.Kitchen, courseFailureError(*retryResult))
				}
				return results, courseFailureError(cr)
			}

			if retryResult != nil {
				prevContext = retryResult.Result.Output
			} else {
				prevContext = ""
			}

			if onCourse != nil {
				onCourse(i, step, "done")
			}
			continue
		}

		prevContext = result.Output

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

func courseFailureError(result CourseResult) error {
	if result.Error != nil {
		return fmt.Errorf("course %d (%s via %s) failed: %w", result.Index+1, result.Step.Station, result.Step.Kitchen, result.Error)
	}

	return fmt.Errorf("course %d (%s via %s) exited with code %d", result.Index+1, result.Step.Station, result.Step.Kitchen, result.Result.ExitCode)
}
