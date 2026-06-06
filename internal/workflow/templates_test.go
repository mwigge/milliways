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
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBuiltInTemplatesIncludesExpectedWorkflowKinds(t *testing.T) {
	t.Parallel()

	templates := BuiltInTemplates()
	names := make(map[string]bool, len(templates))
	for _, tmpl := range templates {
		names[tmpl.Name] = true
		if strings.TrimSpace(tmpl.Description) == "" || tmpl.Nodes == 0 {
			t.Fatalf("template summary = %#v, want description and node count", tmpl)
		}
	}
	for _, want := range []string{"tdd-bug-fix", "parallel-4-implementation", "security-review", "release-train"} {
		if !names[want] {
			t.Fatalf("templates missing %q: %#v", want, templates)
		}
	}
}

func TestInstantiateTemplateCreatesValidQueuedGraph(t *testing.T) {
	t.Parallel()

	createdAt := time.Date(2026, 5, 19, 13, 0, 0, 0, time.UTC)
	wf, err := InstantiateTemplate("parallel-4-implementation", "wf-template", "ship feature", createdAt)
	if err != nil {
		t.Fatalf("InstantiateTemplate returned error: %v", err)
	}
	if wf.ID != "wf-template" || wf.Goal != "ship feature" || wf.Status != StatusQueued {
		t.Fatalf("workflow = %#v, want queued template workflow", wf)
	}
	if len(wf.Nodes) != 7 || len(wf.Edges) == 0 {
		t.Fatalf("nodes/edges = %d/%d, want template graph", len(wf.Nodes), len(wf.Edges))
	}
	if !wf.CreatedAt.Equal(createdAt) || !wf.UpdatedAt.Equal(createdAt) {
		t.Fatalf("timestamps = %v/%v, want %v", wf.CreatedAt, wf.UpdatedAt, createdAt)
	}
	if wf.Nodes[0].Inputs["template"] != "parallel-4-implementation" || wf.Nodes[0].Inputs["goal"] != "ship feature" {
		t.Fatalf("node inputs = %#v, want template and goal", wf.Nodes[0].Inputs)
	}
	if err := Validate(wf); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestInstantiateTemplateRejectsMissingID(t *testing.T) {
	t.Parallel()

	_, err := InstantiateTemplate("tdd-bug-fix", " ", "goal", time.Now())
	if !errors.Is(err, ErrMissingWorkflowID) {
		t.Fatalf("InstantiateTemplate error = %v, want %v", err, ErrMissingWorkflowID)
	}
}

func TestInstantiateTemplateRejectsUnknownTemplate(t *testing.T) {
	t.Parallel()

	_, err := InstantiateTemplate("unknown", "wf", "goal", time.Now())
	if err == nil || !strings.Contains(err.Error(), "unknown workflow template") {
		t.Fatalf("InstantiateTemplate error = %v, want unknown template", err)
	}
}

func TestLoadLocalTemplatesReadsValidDefinitions(t *testing.T) {
	t.Parallel()

	defs, err := LoadLocalTemplates(strings.NewReader(`{
		"templates": [
			{
				"name": "repo-review",
				"description": "Review the current repository state",
				"nodes": [
					{"id": "context", "type": "context"},
					{"id": "review", "type": "agent", "inputs": {"prompt": "inspect changes"}},
					{"id": "summary", "type": "summary"}
				],
				"edges": [
					{"from": "context", "to": "review"},
					{"from": "review", "to": "summary"}
				]
			}
		]
	}`))
	if err != nil {
		t.Fatalf("LoadLocalTemplates returned error: %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("definitions = %d, want 1", len(defs))
	}
	def := defs[0]
	if def.Name != "repo-review" || def.Description != "Review the current repository state" {
		t.Fatalf("definition = %#v, want name and description", def)
	}
	if len(def.Nodes) != 3 || def.Nodes[0].Status != StatusQueued {
		t.Fatalf("nodes = %#v, want queued graph nodes", def.Nodes)
	}

	createdAt := time.Date(2026, 5, 20, 9, 0, 0, 0, time.UTC)
	wf, err := InstantiateTemplateDefinition(def, "wf-local", "find gaps", createdAt)
	if err != nil {
		t.Fatalf("InstantiateTemplateDefinition returned error: %v", err)
	}
	if wf.ID != "wf-local" || wf.Goal != "find gaps" || wf.Nodes[1].Inputs["template"] != "repo-review" {
		t.Fatalf("workflow = %#v, want local template workflow", wf)
	}
	if wf.Nodes[1].Inputs["prompt"] != "inspect changes" || wf.Nodes[1].Inputs["goal"] != "find gaps" {
		t.Fatalf("node inputs = %#v, want template inputs preserved with goal", wf.Nodes[1].Inputs)
	}
	if def.Nodes[1].Inputs["template"] != "" || def.Nodes[1].Inputs["goal"] != "" {
		t.Fatalf("definition inputs = %#v, want instantiation not to mutate definition", def.Nodes[1].Inputs)
	}
	if err := Validate(wf); err != nil {
		t.Fatalf("Validate returned error: %v", err)
	}
}

func TestLoadLocalTemplatesRejectsInvalidGraph(t *testing.T) {
	t.Parallel()

	_, err := LoadLocalTemplates(strings.NewReader(`{
		"name": "bad-graph",
		"nodes": [{"id": "context", "type": "context"}],
		"edges": [{"from": "context", "to": "missing"}]
	}`))
	if !errors.Is(err, ErrUnknownNode) {
		t.Fatalf("LoadLocalTemplates error = %v, want %v", err, ErrUnknownNode)
	}
}

func TestLoadLocalTemplatesRejectsMissingName(t *testing.T) {
	t.Parallel()

	_, err := LoadLocalTemplates(strings.NewReader(`{
		"description": "missing name",
		"nodes": [{"id": "context", "type": "context"}]
	}`))
	if err == nil || !strings.Contains(err.Error(), "workflow template name is required") {
		t.Fatalf("LoadLocalTemplates error = %v, want missing name", err)
	}
}

func TestLoadLocalTemplatesRejectsMissingNodes(t *testing.T) {
	t.Parallel()

	_, err := LoadLocalTemplates(strings.NewReader(`{"name": "empty"}`))
	if err == nil || !strings.Contains(err.Error(), "workflow template nodes are required") {
		t.Fatalf("LoadLocalTemplates error = %v, want missing nodes", err)
	}
}

func TestAvailableTemplatesCombinesBuiltInsWithLocalDefinitions(t *testing.T) {
	t.Parallel()

	local := []TemplateDefinition{
		{
			Name:        "repo-cleanup",
			Description: "Clean up repository issues",
			Nodes:       []Node{{ID: "cleanup", Type: NodeAgent, Status: StatusQueued}},
		},
	}
	summaries := AvailableTemplates(local)
	if len(summaries) != len(BuiltInTemplates())+1 {
		t.Fatalf("summaries = %d, want built-ins plus one local", len(summaries))
	}
	got := summaries[len(summaries)-1]
	if got.Name != "repo-cleanup" || got.Description != "Clean up repository issues" || got.Nodes != 1 {
		t.Fatalf("local summary = %#v, want compatible local summary", got)
	}
}
