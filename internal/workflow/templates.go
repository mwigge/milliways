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

package workflow

import (
	"fmt"
	"strings"
	"time"
)

// TemplateSummary describes a built-in workflow template.
type TemplateSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Nodes       int    `json:"nodes"`
}

type templateDefinition struct {
	Summary TemplateSummary
	Nodes   []Node
	Edges   []Edge
}

var builtInTemplates = []templateDefinition{
	{
		Summary: TemplateSummary{Name: "tdd-bug-fix", Description: "Reproduce, fix, test, and summarize a bug", Nodes: 5},
		Nodes: []Node{
			{ID: "context", Type: NodeContext, Status: StatusQueued, Priority: 40},
			{ID: "reproduce", Type: NodeVerification, Status: StatusQueued, Priority: 30},
			{ID: "implement", Type: NodeAgent, Status: StatusQueued, Priority: 20},
			{ID: "verify", Type: NodeVerification, Status: StatusQueued, Priority: 10},
			{ID: "summary", Type: NodeSummary, Status: StatusQueued},
		},
		Edges: []Edge{{From: "context", To: "reproduce"}, {From: "reproduce", To: "implement"}, {From: "implement", To: "verify"}, {From: "verify", To: "summary"}},
	},
	{
		Summary: TemplateSummary{Name: "parallel-4-implementation", Description: "Split implementation across four independent agents, then integrate", Nodes: 7},
		Nodes: []Node{
			{ID: "context", Type: NodeContext, Status: StatusQueued, Priority: 50},
			{ID: "worker-1", Type: NodeAgent, Status: StatusQueued, Priority: 40},
			{ID: "worker-2", Type: NodeAgent, Status: StatusQueued, Priority: 40},
			{ID: "worker-3", Type: NodeAgent, Status: StatusQueued, Priority: 40},
			{ID: "worker-4", Type: NodeAgent, Status: StatusQueued, Priority: 40},
			{ID: "integrate", Type: NodeAgent, Status: StatusQueued, Priority: 20},
			{ID: "verify", Type: NodeVerification, Status: StatusQueued, Priority: 10},
		},
		Edges: []Edge{
			{From: "context", To: "worker-1"}, {From: "context", To: "worker-2"}, {From: "context", To: "worker-3"}, {From: "context", To: "worker-4"},
			{From: "worker-1", To: "integrate"}, {From: "worker-2", To: "integrate"}, {From: "worker-3", To: "integrate"}, {From: "worker-4", To: "integrate"},
			{From: "integrate", To: "verify"},
		},
	},
	{
		Summary: TemplateSummary{Name: "security-review", Description: "Inspect sensitive changes, run security checks, and produce findings", Nodes: 4},
		Nodes: []Node{
			{ID: "context", Type: NodeContext, Status: StatusQueued, Priority: 30},
			{ID: "review", Type: NodeAgent, Status: StatusQueued, Priority: 20, Security: SecurityEnvelope{Approval: ApprovalRequired, Risk: "security-review"}},
			{ID: "scan", Type: NodeVerification, Status: StatusQueued, Priority: 10},
			{ID: "summary", Type: NodeSummary, Status: StatusQueued},
		},
		Edges: []Edge{{From: "context", To: "review"}, {From: "review", To: "scan"}, {From: "scan", To: "summary"}},
	},
	{
		Summary: TemplateSummary{Name: "release-train", Description: "Verify, package, tag, and summarize a release", Nodes: 5},
		Nodes: []Node{
			{ID: "verify", Type: NodeVerification, Status: StatusQueued, Priority: 40},
			{ID: "package", Type: NodeAgent, Status: StatusQueued, Priority: 30},
			{ID: "approval", Type: NodeApproval, Status: StatusQueued, Priority: 20, Security: SecurityEnvelope{Operation: "release", Approval: ApprovalRequired, Risk: "publishes artifacts"}},
			{ID: "release", Type: NodeRelease, Status: StatusQueued, Priority: 10},
			{ID: "summary", Type: NodeSummary, Status: StatusQueued},
		},
		Edges: []Edge{{From: "verify", To: "package"}, {From: "package", To: "approval"}, {From: "approval", To: "release"}, {From: "release", To: "summary"}},
	},
}

// BuiltInTemplates returns the available built-in workflow templates.
func BuiltInTemplates() []TemplateSummary {
	out := make([]TemplateSummary, 0, len(builtInTemplates))
	for _, def := range builtInTemplates {
		out = append(out, def.Summary)
	}
	return out
}

// InstantiateTemplate creates a queued workflow graph from a built-in template.
func InstantiateTemplate(name, id, goal string, createdAt time.Time) (Workflow, error) {
	templateName := strings.TrimSpace(name)
	workflowID := strings.TrimSpace(id)
	if workflowID == "" {
		return Workflow{}, ErrMissingWorkflowID
	}
	for _, def := range builtInTemplates {
		if def.Summary.Name != templateName {
			continue
		}
		wf := Workflow{
			ID:        workflowID,
			Goal:      strings.TrimSpace(goal),
			Status:    StatusQueued,
			Nodes:     append([]Node(nil), def.Nodes...),
			Edges:     append([]Edge(nil), def.Edges...),
			CreatedAt: createdAt,
			UpdatedAt: createdAt,
		}
		for i := range wf.Nodes {
			if wf.Nodes[i].Inputs == nil {
				wf.Nodes[i].Inputs = map[string]string{}
			}
			wf.Nodes[i].Inputs["template"] = templateName
			if wf.Goal != "" {
				wf.Nodes[i].Inputs["goal"] = wf.Goal
			}
		}
		return wf, Validate(wf)
	}
	return Workflow{}, fmt.Errorf("unknown workflow template: %s", templateName)
}
