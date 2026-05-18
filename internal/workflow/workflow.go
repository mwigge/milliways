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
	"fmt"
	"strings"
	"time"
)

// Status is the durable lifecycle state for a workflow or workflow node.
type Status string

const (
	StatusQueued          Status = "queued"
	StatusRunning         Status = "running"
	StatusWaitingApproval Status = "waiting_approval"
	StatusResumed         Status = "resumed"
	StatusVerifying       Status = "verifying"
	StatusCompleted       Status = "completed"
	StatusFailed          Status = "failed"
	StatusCanceled        Status = "canceled"
	StatusSkipped         Status = "skipped"
)

// NodeType describes the role a node plays in an agentic coding workflow.
type NodeType string

const (
	NodeGoal         NodeType = "goal"
	NodeContext      NodeType = "context"
	NodeAgent        NodeType = "agent"
	NodeToolCall     NodeType = "tool_call"
	NodeApproval     NodeType = "approval"
	NodeVerification NodeType = "verification"
	NodeCommit       NodeType = "commit"
	NodeRelease      NodeType = "release"
	NodeSummary      NodeType = "summary"
)

// ApprovalMode describes the approval requirement attached to a node.
type ApprovalMode string

const (
	ApprovalUnknown     ApprovalMode = ""
	ApprovalNotRequired ApprovalMode = "not_required"
	ApprovalRequired    ApprovalMode = "required"
	ApprovalApproved    ApprovalMode = "approved"
	ApprovalDenied      ApprovalMode = "denied"
)

// ArtifactKind classifies workflow outputs that can be reused or inspected.
type ArtifactKind string

const (
	ArtifactLog      ArtifactKind = "log"
	ArtifactDiff     ArtifactKind = "diff"
	ArtifactTest     ArtifactKind = "test"
	ArtifactLint     ArtifactKind = "lint"
	ArtifactApproval ArtifactKind = "approval"
	ArtifactCommit   ArtifactKind = "commit"
	ArtifactRelease  ArtifactKind = "release"
)

var (
	// ErrMissingWorkflowID means a workflow has no stable identifier.
	ErrMissingWorkflowID = errors.New("workflow id is required")
	// ErrMissingNodeID means a node has no stable identifier.
	ErrMissingNodeID = errors.New("workflow node id is required")
	// ErrDuplicateNode means two nodes share one identifier.
	ErrDuplicateNode = errors.New("duplicate workflow node")
	// ErrUnknownNode means an edge references a node that is not present.
	ErrUnknownNode = errors.New("workflow edge references unknown node")
	// ErrCycle means the workflow graph is not acyclic.
	ErrCycle = errors.New("workflow graph contains a cycle")
)

// Workflow is a durable, serializable graph for one agentic coding run.
type Workflow struct {
	ID        string    `json:"id"`
	Goal      string    `json:"goal,omitempty"`
	Status    Status    `json:"status"`
	Nodes     []Node    `json:"nodes"`
	Edges     []Edge    `json:"edges,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// Node is one executable or inspectable step in a workflow graph.
type Node struct {
	ID        string            `json:"id"`
	Type      NodeType          `json:"type,omitempty"`
	Status    Status            `json:"status"`
	Client    string            `json:"client,omitempty"`
	Agent     string            `json:"agent,omitempty"`
	Inputs    map[string]string `json:"inputs,omitempty"`
	Outputs   map[string]string `json:"outputs,omitempty"`
	Security  SecurityEnvelope  `json:"security,omitempty"`
	Memory    MemoryLink        `json:"memory,omitempty"`
	Artifacts []Artifact        `json:"artifacts,omitempty"`
	StartedAt time.Time         `json:"started_at,omitempty"`
	EndedAt   time.Time         `json:"ended_at,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// Edge declares that To depends on From completing successfully.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// SecurityEnvelope records the approval and risk context for one node.
type SecurityEnvelope struct {
	Operation string       `json:"operation,omitempty"`
	Paths     []string     `json:"paths,omitempty"`
	Approval  ApprovalMode `json:"approval,omitempty"`
	Risk      string       `json:"risk,omitempty"`
	Reason    string       `json:"reason,omitempty"`
}

// MemoryLink records workflow memory reads and writes for lineage.
type MemoryLink struct {
	Reads  []string `json:"reads,omitempty"`
	Writes []string `json:"writes,omitempty"`
}

// Artifact is a durable pointer to a node output or external execution object.
type Artifact struct {
	Kind ArtifactKind `json:"kind"`
	Path string       `json:"path,omitempty"`
	Ref  string       `json:"ref,omitempty"`
}

// Validate verifies that a workflow has stable IDs and an acyclic graph.
func Validate(wf Workflow) error {
	if strings.TrimSpace(wf.ID) == "" {
		return ErrMissingWorkflowID
	}
	nodes := make(map[string]Node, len(wf.Nodes))
	for _, node := range wf.Nodes {
		id := strings.TrimSpace(node.ID)
		if id == "" {
			return ErrMissingNodeID
		}
		if _, exists := nodes[id]; exists {
			return fmt.Errorf("%w: %s", ErrDuplicateNode, id)
		}
		nodes[id] = node
	}
	graph := make(map[string][]string, len(nodes))
	inDegree := make(map[string]int, len(nodes))
	for id := range nodes {
		graph[id] = nil
		inDegree[id] = 0
	}
	for _, edge := range wf.Edges {
		from := strings.TrimSpace(edge.From)
		to := strings.TrimSpace(edge.To)
		if _, ok := nodes[from]; !ok {
			return fmt.Errorf("%w: %s", ErrUnknownNode, from)
		}
		if _, ok := nodes[to]; !ok {
			return fmt.Errorf("%w: %s", ErrUnknownNode, to)
		}
		graph[from] = append(graph[from], to)
		inDegree[to]++
	}
	if hasCycle(graph, inDegree) {
		return ErrCycle
	}
	return nil
}

// ReadyNodes returns queued nodes whose dependency edges all point from
// completed nodes.
func ReadyNodes(wf Workflow) ([]Node, error) {
	if err := Validate(wf); err != nil {
		return nil, err
	}
	nodes := make(map[string]Node, len(wf.Nodes))
	for _, node := range wf.Nodes {
		nodes[strings.TrimSpace(node.ID)] = node
	}
	blockers := make(map[string][]string, len(wf.Nodes))
	for _, edge := range wf.Edges {
		to := strings.TrimSpace(edge.To)
		from := strings.TrimSpace(edge.From)
		blockers[to] = append(blockers[to], from)
	}

	ready := make([]Node, 0)
	for _, node := range wf.Nodes {
		if node.Status != StatusQueued {
			continue
		}
		if dependenciesCompleted(nodes, blockers[strings.TrimSpace(node.ID)]) {
			ready = append(ready, node)
		}
	}
	return ready, nil
}

func dependenciesCompleted(nodes map[string]Node, dependencyIDs []string) bool {
	for _, id := range dependencyIDs {
		if nodes[id].Status != StatusCompleted {
			return false
		}
	}
	return true
}

func hasCycle(graph map[string][]string, inDegree map[string]int) bool {
	queue := make([]string, 0, len(inDegree))
	for id, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, id)
		}
	}
	visited := 0
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		visited++
		for _, next := range graph[id] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	return visited != len(inDegree)
}
