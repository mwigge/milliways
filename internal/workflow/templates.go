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
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// TemplateSummary describes a workflow template.
type TemplateSummary struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Nodes       int    `json:"nodes"`
}

// TemplateDefinition describes a repository-local workflow template.
type TemplateDefinition struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges,omitempty"`
}

type templateDefinition struct {
	Summary TemplateSummary
	Nodes   []Node
	Edges   []Edge
}

type templateDefinitionsFile struct {
	Templates []TemplateDefinition `json:"templates"`
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

// LoadLocalTemplates reads repository-local workflow template definitions from
// JSON. The JSON may be a single template object, an array of template objects,
// or an object with a "templates" array.
func LoadLocalTemplates(r io.Reader) ([]TemplateDefinition, error) {
	var raw json.RawMessage
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return nil, fmt.Errorf("local workflow templates JSON has trailing data")
	}
	if len(raw) == 0 {
		return nil, io.ErrUnexpectedEOF
	}

	var defs []TemplateDefinition
	switch raw[0] {
	case '[':
		if err := unmarshalLocalTemplates(raw, &defs); err != nil {
			return nil, err
		}
	case '{':
		var file templateDefinitionsFile
		if err := unmarshalLocalTemplates(raw, &file); err == nil && file.Templates != nil {
			defs = file.Templates
			break
		}
		var def TemplateDefinition
		if err := unmarshalLocalTemplates(raw, &def); err != nil {
			return nil, err
		}
		defs = []TemplateDefinition{def}
	default:
		return nil, fmt.Errorf("local workflow templates JSON must be an object or array")
	}

	out := make([]TemplateDefinition, 0, len(defs))
	for i, def := range defs {
		normalized, err := normalizeTemplateDefinition(def)
		if err != nil {
			return nil, fmt.Errorf("local workflow template %d: %w", i, err)
		}
		out = append(out, normalized)
	}
	return out, nil
}

// LoadLocalTemplatesFile reads repository-local workflow template definitions
// from a JSON file.
func LoadLocalTemplatesFile(path string) ([]TemplateDefinition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return LoadLocalTemplates(f)
}

// TemplateSummaries returns summaries for template definitions.
func TemplateSummaries(defs []TemplateDefinition) []TemplateSummary {
	out := make([]TemplateSummary, 0, len(defs))
	for _, def := range defs {
		out = append(out, TemplateSummary{Name: def.Name, Description: def.Description, Nodes: len(def.Nodes)})
	}
	return out
}

// AvailableTemplates returns built-in summaries followed by local template
// summaries.
func AvailableTemplates(local []TemplateDefinition) []TemplateSummary {
	builtIns := BuiltInTemplates()
	out := make([]TemplateSummary, 0, len(builtIns)+len(local))
	out = append(out, builtIns...)
	out = append(out, TemplateSummaries(local)...)
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

// InstantiateTemplateDefinition creates a queued workflow graph from a local
// template definition.
func InstantiateTemplateDefinition(def TemplateDefinition, id, goal string, createdAt time.Time) (Workflow, error) {
	normalized, err := normalizeTemplateDefinition(def)
	if err != nil {
		return Workflow{}, err
	}
	workflowID := strings.TrimSpace(id)
	if workflowID == "" {
		return Workflow{}, ErrMissingWorkflowID
	}
	wf := Workflow{
		ID:        workflowID,
		Goal:      strings.TrimSpace(goal),
		Status:    StatusQueued,
		Nodes:     cloneTemplateNodes(normalized.Nodes),
		Edges:     append([]Edge(nil), normalized.Edges...),
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	for i := range wf.Nodes {
		if wf.Nodes[i].Inputs == nil {
			wf.Nodes[i].Inputs = map[string]string{}
		}
		wf.Nodes[i].Inputs["template"] = normalized.Name
		if wf.Goal != "" {
			wf.Nodes[i].Inputs["goal"] = wf.Goal
		}
	}
	return wf, Validate(wf)
}

func unmarshalLocalTemplates(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("local workflow templates JSON has trailing data")
	}
	return nil
}

func normalizeTemplateDefinition(def TemplateDefinition) (TemplateDefinition, error) {
	def.Name = strings.TrimSpace(def.Name)
	def.Description = strings.TrimSpace(def.Description)
	if def.Name == "" {
		return TemplateDefinition{}, fmt.Errorf("workflow template name is required")
	}
	if len(def.Nodes) == 0 {
		return TemplateDefinition{}, fmt.Errorf("workflow template nodes are required")
	}
	def.Nodes = cloneTemplateNodes(def.Nodes)
	def.Edges = append([]Edge(nil), def.Edges...)
	for i := range def.Nodes {
		def.Nodes[i].ID = strings.TrimSpace(def.Nodes[i].ID)
		if def.Nodes[i].Status == "" {
			def.Nodes[i].Status = StatusQueued
		}
	}
	for i := range def.Edges {
		def.Edges[i].From = strings.TrimSpace(def.Edges[i].From)
		def.Edges[i].To = strings.TrimSpace(def.Edges[i].To)
	}
	wf := Workflow{
		ID:     "template:" + def.Name,
		Status: StatusQueued,
		Nodes:  def.Nodes,
		Edges:  def.Edges,
	}
	if err := Validate(wf); err != nil {
		return TemplateDefinition{}, err
	}
	return def, nil
}

func cloneTemplateNodes(nodes []Node) []Node {
	out := append([]Node(nil), nodes...)
	for i := range out {
		out[i].Inputs = cloneStringMap(out[i].Inputs)
		out[i].Outputs = cloneStringMap(out[i].Outputs)
		out[i].Security.Paths = append([]string(nil), out[i].Security.Paths...)
		out[i].Memory.Reads = append([]string(nil), out[i].Memory.Reads...)
		out[i].Memory.Writes = append([]string(nil), out[i].Memory.Writes...)
		out[i].Artifacts = append([]Artifact(nil), out[i].Artifacts...)
		out[i].ToolCalls = cloneToolCalls(out[i].ToolCalls)
		out[i].Logs = append([]LogRecord(nil), out[i].Logs...)
		out[i].Mutations = append([]FileMutation(nil), out[i].Mutations...)
	}
	return out
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneToolCalls(in []ToolCall) []ToolCall {
	out := append([]ToolCall(nil), in...)
	for i := range out {
		out[i].Args = cloneStringMap(out[i].Args)
	}
	return out
}
