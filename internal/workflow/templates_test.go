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
