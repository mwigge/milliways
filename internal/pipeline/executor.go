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

package pipeline

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
)

// AdapterFactory creates an adapter for a named kitchen.
type AdapterFactory func(ctx context.Context, kitchen string) (adapter.Adapter, error)

// StepEventCallback is called for every adapter event from any step.
type StepEventCallback func(stepID string, evt adapter.Event)

// StepLifecycleCallback is called when step status changes.
type StepLifecycleCallback func(stepID string, status StepStatus)

// Executor runs a pipeline respecting step dependencies and parallelism.
type Executor struct {
	factory  AdapterFactory
	onEvent  StepEventCallback
	onStatus StepLifecycleCallback
}

// NewExecutor creates a pipeline executor.
func NewExecutor(factory AdapterFactory, onEvent StepEventCallback, onStatus StepLifecycleCallback) *Executor {
	return &Executor{factory: factory, onEvent: onEvent, onStatus: onStatus}
}

// Run executes all pipeline steps respecting the dependency DAG.
// It blocks until all steps complete or ctx is cancelled.
func (e *Executor) Run(ctx context.Context, p *Pipeline) error {
	if len(p.Steps) > MaxSteps {
		return fmt.Errorf("pipeline has %d steps, max is %d", len(p.Steps), MaxSteps)
	}

	if err := validateDAG(p); err != nil {
		return fmt.Errorf("invalid pipeline DAG: %w", err)
	}

	p.Status = StatusActive

	// Build step lookup and in-degree map.
	stepMap := make(map[string]*Step, len(p.Steps))
	inDegree := make(map[string]int, len(p.Steps))
	dependents := make(map[string][]string) // stepID -> list of steps that depend on it

	for _, s := range p.Steps {
		stepMap[s.ID] = s
		inDegree[s.ID] = len(s.DependsOn)
		for _, dep := range s.DependsOn {
			dependents[dep] = append(dependents[dep], s.ID)
		}
	}

	// Result channel collects completed steps.
	results := make(chan StepResult, len(p.Steps))

	var mu sync.Mutex // protects inDegree updates
	active := 0
	completed := 0
	anyFailed := false

	// Launch initial wave (in-degree == 0).
	for _, s := range p.Steps {
		if inDegree[s.ID] == 0 {
			active++
			go e.runStepAsync(ctx, s, results)
		}
	}

	// Process completions until all steps are done or context cancelled.
	for completed < len(p.Steps) && active > 0 {
		select {
		case <-ctx.Done():
			p.Status = StatusFailed
			return ctx.Err()
		case res := <-results:
			active--
			completed++

			step := stepMap[res.StepID]
			step.Output = res.Output
			step.ExitCode = res.ExitCode
			step.Duration = res.Duration

			if res.Err != nil || res.ExitCode != 0 {
				step.Status = StatusFailed
				anyFailed = true
				e.notifyStatus(step.ID, StatusFailed)
			} else {
				step.Status = StatusDone
				e.notifyStatus(step.ID, StatusDone)
			}

			// Decrement in-degree for dependents and launch newly ready steps.
			mu.Lock()
			for _, depID := range dependents[res.StepID] {
				inDegree[depID]--
				if inDegree[depID] == 0 {
					depStep := stepMap[depID]

					// Check if all dependencies actually succeeded.
					// If any dependency failed, skip this step unless it's a
					// "summarize" step (last step) — those run with partial results.
					allDepsOK := true
					for _, reqID := range depStep.DependsOn {
						if stepMap[reqID].Status == StatusFailed {
							allDepsOK = false
							break
						}
					}

					isLastStep := depStep.ID == p.Steps[len(p.Steps)-1].ID
					if !allDepsOK && !isLastStep {
						depStep.Status = StatusSkipped
						e.notifyStatus(depStep.ID, StatusSkipped)
						// Push a synthetic result so the loop can proceed.
						completed++
						continue
					}

					active++
					go e.runStepAsync(ctx, depStep, results)
				}
			}
			mu.Unlock()
		}
	}

	// Mark any remaining pending steps as skipped.
	for _, s := range p.Steps {
		if s.Status == StatusPending {
			s.Status = StatusSkipped
			e.notifyStatus(s.ID, StatusSkipped)
		}
	}

	if anyFailed {
		p.Status = StatusFailed
	} else {
		p.Status = StatusDone
	}

	return nil
}

// runStepAsync executes a single step and sends the result on the channel.
func (e *Executor) runStepAsync(ctx context.Context, step *Step, results chan<- StepResult) {
	res := e.runStep(ctx, step)
	results <- res
}

// runStep executes a single step: creates adapter, drains events, captures output.
func (e *Executor) runStep(ctx context.Context, step *Step) StepResult {
	step.Status = StatusActive
	step.StartedAt = time.Now()
	e.notifyStatus(step.ID, StatusActive)

	adapt, err := e.factory(ctx, step.Kitchen)
	if err != nil {
		return StepResult{
			StepID:   step.ID,
			ExitCode: 1,
			Err:      fmt.Errorf("creating adapter for %s: %w", step.Kitchen, err),
		}
	}

	task := kitchen.Task{Prompt: step.Prompt}
	eventCh, err := adapt.Exec(ctx, task)
	if err != nil {
		return StepResult{
			StepID:   step.ID,
			ExitCode: 1,
			Err:      fmt.Errorf("starting %s: %w", step.Kitchen, err),
		}
	}

	var output string
	exitCode := 0

	for evt := range eventCh {
		e.notifyEvent(step.ID, evt)

		switch evt.Type {
		case adapter.EventText:
			output += evt.Text + "\n"
		case adapter.EventCodeBlock:
			output += evt.Code + "\n"
		case adapter.EventDone:
			exitCode = evt.ExitCode
		case adapter.EventError:
			if exitCode == 0 {
				exitCode = 1
			}
		}
	}

	dur := time.Since(step.StartedAt)

	return StepResult{
		StepID:   step.ID,
		Output:   output,
		ExitCode: exitCode,
		Duration: dur,
	}
}

// notifyEvent safely calls the event callback if set.
func (e *Executor) notifyEvent(stepID string, evt adapter.Event) {
	if e.onEvent != nil {
		e.onEvent(stepID, evt)
	}
}

// notifyStatus safely calls the lifecycle callback if set.
func (e *Executor) notifyStatus(stepID string, status StepStatus) {
	if e.onStatus != nil {
		e.onStatus(stepID, status)
	}
}

// validateDAG checks for cycles and missing dependencies.
func validateDAG(p *Pipeline) error {
	ids := make(map[string]bool, len(p.Steps))
	for _, s := range p.Steps {
		if ids[s.ID] {
			return fmt.Errorf("duplicate step ID %q", s.ID)
		}
		ids[s.ID] = true
	}

	for _, s := range p.Steps {
		for _, dep := range s.DependsOn {
			if !ids[dep] {
				return fmt.Errorf("step %q depends on unknown step %q", s.ID, dep)
			}
		}
	}

	// Kahn's algorithm to detect cycles.
	inDegree := make(map[string]int, len(p.Steps))
	adj := make(map[string][]string)
	for _, s := range p.Steps {
		inDegree[s.ID] = len(s.DependsOn)
		for _, dep := range s.DependsOn {
			adj[dep] = append(adj[dep], s.ID)
		}
	}

	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range adj[curr] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if visited != len(p.Steps) {
		return fmt.Errorf("cycle detected in step dependencies")
	}

	return nil
}
