package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/kitchen"
	"github.com/mwigge/milliways/internal/kitchen/adapter"
)

// PlanOutput is the JSON structure the planning step must emit.
type PlanOutput struct {
	Tasks []PlannedTask `json:"tasks"`
}

// PlannedTask is one sub-task identified by the planning step.
type PlannedTask struct {
	ID      string `json:"id"`
	Kitchen string `json:"kitchen"`
	Prompt  string `json:"prompt"`
}

// PlanPromptTemplate wraps the user's prompt with decomposition instructions.
const PlanPromptTemplate = `You are a task planner for a multi-kitchen AI orchestration system called Milliways.

The user wants: %s

Available kitchens: %s

Analyze this request and decompose it into concrete sub-tasks that can be executed independently by different AI kitchens. Each sub-task must be self-contained with enough context to execute without seeing other sub-tasks.

Rules:
- Maximum 8 sub-tasks
- Each task ID must be a short kebab-case identifier (e.g. "check-auth-service")
- Each kitchen must be one of the available kitchens listed above
- Each prompt must be fully self-contained

Respond with ONLY a JSON object in this exact format (no markdown fences, no explanation):
{"tasks": [{"id": "short-id", "kitchen": "kitchen-name", "prompt": "full prompt for this kitchen"}]}

If the task does not need decomposition (it's simple enough for one kitchen), return:
{"tasks": []}`

// SummarizePromptTemplate wraps collected outputs for the final summary step.
const SummarizePromptTemplate = `Summarize the results from parallel sub-tasks executed by different AI tools.

Original request: %s

Results from sub-tasks:

%s

Provide a clear, consolidated summary of all findings. If any sub-task failed, note what was missed.`

// Planner constructs a Pipeline from a user prompt via a Claude planning step.
type Planner struct {
	factory           AdapterFactory
	availableKitchens []string
	defaultKitchen    string
}

// NewPlanner creates a planner that uses Claude for task decomposition.
func NewPlanner(factory AdapterFactory, kitchens []string, defaultKitchen string) *Planner {
	return &Planner{
		factory:           factory,
		availableKitchens: kitchens,
		defaultKitchen:    defaultKitchen,
	}
}

// Plan asks Claude to decompose the prompt, then builds a Pipeline.
// Returns nil if Claude determines no decomposition is needed.
func (p *Planner) Plan(ctx context.Context, userPrompt string) (*Pipeline, error) {
	planPrompt := fmt.Sprintf(PlanPromptTemplate, userPrompt, strings.Join(p.availableKitchens, ", "))

	adapt, err := p.factory(ctx, "claude")
	if err != nil {
		return nil, fmt.Errorf("creating claude adapter for planning: %w", err)
	}

	task := kitchen.Task{Prompt: planPrompt}
	eventCh, err := adapt.Exec(ctx, task)
	if err != nil {
		return nil, fmt.Errorf("starting planning step: %w", err)
	}

	// Drain events and accumulate output.
	var output string
	for evt := range eventCh {
		switch evt.Type {
		case adapter.EventText:
			output += evt.Text
		case adapter.EventCodeBlock:
			output += evt.Code
		}
	}

	plan, err := parsePlanJSON(output)
	if err != nil {
		return nil, fmt.Errorf("parsing plan output: %w", err)
	}

	if len(plan.Tasks) == 0 {
		return nil, nil // no decomposition needed
	}

	return p.buildPipeline(userPrompt, plan), nil
}

// buildPipeline constructs a Pipeline from a PlanOutput.
func (p *Planner) buildPipeline(userPrompt string, plan *PlanOutput) *Pipeline {
	pipe := &Pipeline{
		ID:        fmt.Sprintf("pipe-%d", time.Now().UnixMilli()),
		Prompt:    userPrompt,
		Status:    StatusPending,
		CreatedAt: time.Now(),
	}

	// Fan-out steps: each task runs in parallel with no dependencies.
	taskIDs := make([]string, 0, len(plan.Tasks))
	for _, t := range plan.Tasks {
		kitchenName := p.resolveKitchen(t.Kitchen)
		taskIDs = append(taskIDs, t.ID)
		pipe.Steps = append(pipe.Steps, &Step{
			ID:        t.ID,
			Kitchen:   kitchenName,
			Prompt:    t.Prompt,
			Mode:      Parallel,
			DependsOn: nil, // no plan step dependency — tasks are self-contained from planner output
			Status:    StatusPending,
		})
	}

	// Summarize step: depends on all fan-out steps.
	pipe.Steps = append(pipe.Steps, &Step{
		ID:        "summarize",
		Kitchen:   "claude",
		Prompt:    "", // populated at execution time with collected outputs
		Mode:      Sequential,
		DependsOn: taskIDs,
		Status:    StatusPending,
	})

	return pipe
}

// resolveKitchen validates a kitchen name against the available list.
// Returns the default kitchen if the name is unknown.
func (p *Planner) resolveKitchen(name string) string {
	for _, k := range p.availableKitchens {
		if strings.EqualFold(k, name) {
			return k
		}
	}
	return p.defaultKitchen
}

// parsePlanJSON extracts PlanOutput from Claude's text output.
// Handles markdown code fences and surrounding whitespace.
func parsePlanJSON(output string) (*PlanOutput, error) {
	text := strings.TrimSpace(output)

	// Strip markdown code fences if present.
	if idx := strings.Index(text, "```"); idx >= 0 {
		text = text[idx+3:]
		// Skip optional language tag (e.g. "json").
		if nl := strings.IndexByte(text, '\n'); nl >= 0 {
			text = text[nl+1:]
		}
		if end := strings.LastIndex(text, "```"); end >= 0 {
			text = text[:end]
		}
	}

	text = strings.TrimSpace(text)

	// Find the JSON object boundaries.
	start := strings.IndexByte(text, '{')
	end := strings.LastIndexByte(text, '}')
	if start < 0 || end < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in planner output")
	}
	text = text[start : end+1]

	var plan PlanOutput
	if err := json.Unmarshal([]byte(text), &plan); err != nil {
		return nil, fmt.Errorf("unmarshalling plan JSON: %w", err)
	}

	// Validate tasks.
	for i, t := range plan.Tasks {
		if t.ID == "" {
			return nil, fmt.Errorf("task %d has empty id", i)
		}
		if t.Kitchen == "" {
			return nil, fmt.Errorf("task %q has empty kitchen", t.ID)
		}
		if t.Prompt == "" {
			return nil, fmt.Errorf("task %q has empty prompt", t.ID)
		}
	}

	if len(plan.Tasks) > MaxSteps-1 { // -1 for the summarize step
		plan.Tasks = plan.Tasks[:MaxSteps-1]
	}

	return &plan, nil
}

// BuildSummarizePrompt creates the prompt for the summarize step from collected outputs.
func BuildSummarizePrompt(userPrompt string, steps []*Step) string {
	var parts []string
	for _, s := range steps {
		status := "completed"
		if s.Status == StatusFailed {
			status = "FAILED"
		} else if s.Status == StatusSkipped {
			status = "SKIPPED"
		}

		output := s.Output
		if output == "" && s.Status != StatusDone {
			output = "(no output)"
		}

		parts = append(parts, fmt.Sprintf("### %s [%s] (kitchen: %s)\n%s", s.ID, status, s.Kitchen, output))
	}

	return fmt.Sprintf(SummarizePromptTemplate, userPrompt, strings.Join(parts, "\n\n"))
}
