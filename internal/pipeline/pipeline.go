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

import "time"

// StepMode defines how a step executes relative to its siblings.
type StepMode int

const (
	// Sequential runs after the previous step completes.
	Sequential StepMode = iota
	// Parallel runs concurrently with other Parallel steps that share the same dependencies.
	Parallel
)

// StepStatus tracks step lifecycle.
type StepStatus string

const (
	StatusPending StepStatus = "pending"
	StatusActive  StepStatus = "active"
	StatusDone    StepStatus = "done"
	StatusFailed  StepStatus = "failed"
	StatusSkipped StepStatus = "skipped"
)

// MaxSteps is the hard limit on steps per pipeline.
const MaxSteps = 10

// Step defines one unit of work in a pipeline.
type Step struct {
	ID        string
	Kitchen   string
	Prompt    string
	Mode      StepMode
	DependsOn []string
	Status    StepStatus
	Output    string
	ExitCode  int
	StartedAt time.Time
	Duration  time.Duration
	TicketID  string
}

// Pipeline is an ordered execution plan with dependency tracking.
type Pipeline struct {
	ID        string
	Prompt    string // original user prompt
	Steps     []*Step
	Status    StepStatus
	CreatedAt time.Time
}

// StepResult is emitted when a step completes.
type StepResult struct {
	StepID   string
	Output   string
	ExitCode int
	Duration time.Duration
	Err      error
}

// StepByID returns the step with the given ID, or nil.
func (p *Pipeline) StepByID(id string) *Step {
	for _, s := range p.Steps {
		if s.ID == id {
			return s
		}
	}
	return nil
}
