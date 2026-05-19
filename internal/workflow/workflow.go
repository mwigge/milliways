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
	"sort"
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
	// ErrNodeNotQueued means a node cannot be started because it is not queued.
	ErrNodeNotQueued = errors.New("workflow node is not queued")
	// ErrNodeBlocked means a queued node cannot be started because dependencies are incomplete.
	ErrNodeBlocked = errors.New("workflow node is blocked")
	// ErrNodeNotRunning means a node cannot be finished because it is not running.
	ErrNodeNotRunning = errors.New("workflow node is not running")
	// ErrNodeNotWaiting means a node cannot be resumed because it is not waiting for approval.
	ErrNodeNotWaiting = errors.New("workflow node is not waiting for approval")
	// ErrNodeNotResumed means a node cannot be verified because it is not resumed.
	ErrNodeNotResumed = errors.New("workflow node is not resumed")
	// ErrNodeNotRetryable means a node cannot be retried from its current status.
	ErrNodeNotRetryable = errors.New("workflow node is not retryable")
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
	ID         string            `json:"id"`
	Type       NodeType          `json:"type,omitempty"`
	Status     Status            `json:"status"`
	Client     string            `json:"client,omitempty"`
	Agent      string            `json:"agent,omitempty"`
	Inputs     map[string]string `json:"inputs,omitempty"`
	Outputs    map[string]string `json:"outputs,omitempty"`
	Security   SecurityEnvelope  `json:"security,omitempty"`
	Memory     MemoryLink        `json:"memory,omitempty"`
	Artifacts  []Artifact        `json:"artifacts,omitempty"`
	ToolCalls  []ToolCall        `json:"tool_calls,omitempty"`
	Logs       []LogRecord       `json:"logs,omitempty"`
	Mutations  []FileMutation    `json:"mutations,omitempty"`
	StartedAt  time.Time         `json:"started_at,omitempty"`
	EndedAt    time.Time         `json:"ended_at,omitempty"`
	Error      string            `json:"error,omitempty"`
	RetryCount int               `json:"retry_count,omitempty"`
	Priority   int               `json:"priority,omitempty"`
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

// ToolCall records a single tool invocation by a workflow node.
type ToolCall struct {
	Tool     string            `json:"tool"`
	Args     map[string]string `json:"args,omitempty"`
	Result   string            `json:"result,omitempty"`
	Error    string            `json:"error,omitempty"`
	Duration string            `json:"duration,omitempty"`
}

// LogRecord is a timestamped log entry from a node.
type LogRecord struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level,omitempty"`
	Message string    `json:"message"`
}

// FileMutation records a file operation performed by a node.
type FileMutation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Lines int    `json:"lines,omitempty"`
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
	sort.SliceStable(ready, func(i, j int) bool {
		if ready[i].Priority != ready[j].Priority {
			return ready[i].Priority > ready[j].Priority
		}
		return strings.TrimSpace(ready[i].ID) < strings.TrimSpace(ready[j].ID)
	})
	return ready, nil
}

// StartReadyNode moves a queued, dependency-ready node to running and records
// when it started. It returns an updated workflow value without mutating wf.
func StartReadyNode(wf Workflow, nodeID string, startedAt time.Time) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetID := strings.TrimSpace(nodeID)
	targetIndex := -1
	nodes := make(map[string]Node, len(wf.Nodes))
	for i, node := range wf.Nodes {
		id := strings.TrimSpace(node.ID)
		nodes[id] = node
		if id == targetID {
			targetIndex = i
		}
	}
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, targetID)
	}

	target := wf.Nodes[targetIndex]
	if target.Status != StatusQueued {
		return Workflow{}, fmt.Errorf("%w: %s", ErrNodeNotQueued, strings.TrimSpace(target.ID))
	}

	blockers := make(map[string][]string, len(wf.Nodes))
	for _, edge := range wf.Edges {
		to := strings.TrimSpace(edge.To)
		from := strings.TrimSpace(edge.From)
		blockers[to] = append(blockers[to], from)
	}
	if !dependenciesCompleted(nodes, blockers[targetID]) {
		return Workflow{}, fmt.Errorf("%w: %s", ErrNodeBlocked, targetID)
	}

	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].Status = StatusRunning
	updated.Nodes[targetIndex].StartedAt = startedAt
	return updated, nil
}

// WaitForApprovalNode moves a running node to waiting_approval status,
// indicating the node is blocked pending user approval. It records when the
// wait started and the reason. It does not mutate wf.
func WaitForApprovalNode(wf Workflow, nodeID string, waitedAt time.Time, reason string) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetID := strings.TrimSpace(nodeID)
	targetIndex := -1
	for i, node := range wf.Nodes {
		if strings.TrimSpace(node.ID) == targetID {
			targetIndex = i
			break
		}
	}
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, targetID)
	}

	target := wf.Nodes[targetIndex]
	if target.Status != StatusRunning {
		return Workflow{}, fmt.Errorf("%w: %s", ErrNodeNotRunning, strings.TrimSpace(target.ID))
	}

	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].Status = StatusWaitingApproval
	updated.Nodes[targetIndex].Error = reason
	return updated, nil
}

// ResumeApprovalNode moves a waiting_approval node back to running, indicating
// the user has approved and execution should continue. It does not mutate wf.
func ResumeApprovalNode(wf Workflow, nodeID string, resumedAt time.Time) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetID := strings.TrimSpace(nodeID)
	targetIndex := -1
	for i, node := range wf.Nodes {
		if strings.TrimSpace(node.ID) == targetID {
			targetIndex = i
			break
		}
	}
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, targetID)
	}

	target := wf.Nodes[targetIndex]
	if target.Status != StatusWaitingApproval {
		return Workflow{}, fmt.Errorf("%w: %s", ErrNodeNotWaiting, strings.TrimSpace(target.ID))
	}

	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].Status = StatusResumed
	updated.Nodes[targetIndex].Error = ""
	return updated, nil
}

// DenyApprovalNode moves a waiting_approval node to failed, indicating the user
// denied the operation. It does not mutate wf.
func DenyApprovalNode(wf Workflow, nodeID string, deniedAt time.Time, reason string) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetID := strings.TrimSpace(nodeID)
	targetIndex := -1
	for i, node := range wf.Nodes {
		if strings.TrimSpace(node.ID) == targetID {
			targetIndex = i
			break
		}
	}
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, targetID)
	}

	target := wf.Nodes[targetIndex]
	if target.Status != StatusWaitingApproval {
		return Workflow{}, fmt.Errorf("%w: %s", ErrNodeNotWaiting, strings.TrimSpace(target.ID))
	}

	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].Status = StatusFailed
	updated.Nodes[targetIndex].EndedAt = deniedAt
	updated.Nodes[targetIndex].Error = reason
	if target.Security.Approval != "" {
		updated.Nodes[targetIndex].Security.Approval = ApprovalDenied
	}
	if allNodesCompleted(updated.Nodes) {
		updated.Status = StatusCompleted
	}
	return updated, nil
}

// VerifyRunningNode moves a resumed node to verifying status, indicating the node
// is running verification checks. It does not mutate wf.
func VerifyRunningNode(wf Workflow, nodeID string) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetID := strings.TrimSpace(nodeID)
	targetIndex := -1
	for i, node := range wf.Nodes {
		if strings.TrimSpace(node.ID) == targetID {
			targetIndex = i
			break
		}
	}
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, targetID)
	}

	target := wf.Nodes[targetIndex]
	if target.Status != StatusResumed {
		return Workflow{}, fmt.Errorf("%w: %s", ErrNodeNotResumed, strings.TrimSpace(target.ID))
	}

	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].Status = StatusVerifying
	return updated, nil
}

// AppendNodeToolCall appends a tool call record to a node. It does not mutate
// wf.
func AppendNodeToolCall(wf Workflow, nodeID string, call ToolCall) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetIndex := findNodeIndex(wf.Nodes, nodeID)
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, strings.TrimSpace(nodeID))
	}

	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].ToolCalls = append(append([]ToolCall(nil), wf.Nodes[targetIndex].ToolCalls...), call)
	return updated, nil
}

// AppendNodeLog appends a log record to a node and records the log time on the
// workflow. It does not mutate wf.
func AppendNodeLog(wf Workflow, nodeID string, record LogRecord) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetIndex := findNodeIndex(wf.Nodes, nodeID)
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, strings.TrimSpace(nodeID))
	}

	updated := wf
	updated.UpdatedAt = record.Time
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].Logs = append(append([]LogRecord(nil), wf.Nodes[targetIndex].Logs...), record)
	return updated, nil
}

// AppendNodeMutation appends a file mutation record to a node. It does not
// mutate wf.
func AppendNodeMutation(wf Workflow, nodeID string, mutation FileMutation) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetIndex := findNodeIndex(wf.Nodes, nodeID)
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, strings.TrimSpace(nodeID))
	}

	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].Mutations = append(append([]FileMutation(nil), wf.Nodes[targetIndex].Mutations...), mutation)
	return updated, nil
}

// CompleteRunningNode moves a running node to completed, records when it ended,
// and merges any supplied outputs and artifacts. It does not mutate wf.
func CompleteRunningNode(wf Workflow, nodeID string, endedAt time.Time, outputs map[string]string, artifacts []Artifact) (Workflow, error) {
	return finishRunningNode(wf, nodeID, StatusCompleted, endedAt, "", outputs, artifacts)
}

// FailRunningNode moves a running node to failed, records when it ended, and
// stores the failure message. It does not mutate wf.
func FailRunningNode(wf Workflow, nodeID string, endedAt time.Time, message string) (Workflow, error) {
	return finishRunningNode(wf, nodeID, StatusFailed, endedAt, message, nil, nil)
}

// RetryNode moves a failed or canceled node back to queued, clears execution
// timestamps and error state, and increments its retry count. It does not
// mutate wf.
func RetryNode(wf Workflow, nodeID string, retriedAt time.Time) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetID := strings.TrimSpace(nodeID)
	targetIndex := -1
	for i, node := range wf.Nodes {
		if strings.TrimSpace(node.ID) == targetID {
			targetIndex = i
			break
		}
	}
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, targetID)
	}

	target := wf.Nodes[targetIndex]
	if target.Status != StatusFailed && target.Status != StatusCanceled {
		return Workflow{}, fmt.Errorf("%w: %s", ErrNodeNotRetryable, strings.TrimSpace(target.ID))
	}

	updated := wf
	updated.UpdatedAt = retriedAt
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].Status = StatusQueued
	updated.Nodes[targetIndex].StartedAt = time.Time{}
	updated.Nodes[targetIndex].EndedAt = time.Time{}
	updated.Nodes[targetIndex].Error = ""
	updated.Nodes[targetIndex].RetryCount++
	resetRetryDependents(updated.Nodes, wf.Edges, targetID)
	if wf.Status == StatusFailed || wf.Status == StatusCanceled {
		updated.Status = StatusQueued
		if hasOngoingNode(updated.Nodes) {
			updated.Status = StatusRunning
		}
	}
	return updated, nil
}

func resetRetryDependents(nodes []Node, edges []Edge, nodeID string) {
	dependents := make(map[string][]string, len(nodes))
	for _, edge := range edges {
		from := strings.TrimSpace(edge.From)
		to := strings.TrimSpace(edge.To)
		dependents[from] = append(dependents[from], to)
	}
	seen := map[string]bool{nodeID: true}
	queue := append([]string(nil), dependents[nodeID]...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		for i, node := range nodes {
			if strings.TrimSpace(node.ID) != id {
				continue
			}
			if shouldResetRetryDependent(node.Status) {
				nodes[i].Status = StatusQueued
				nodes[i].StartedAt = time.Time{}
				nodes[i].EndedAt = time.Time{}
				nodes[i].Error = ""
			}
			break
		}
		queue = append(queue, dependents[id]...)
	}
}

func shouldResetRetryDependent(status Status) bool {
	switch status {
	case StatusQueued, StatusCompleted, StatusFailed, StatusCanceled:
		return true
	default:
		return false
	}
}

// CancelWorkflow moves the workflow to canceled and cancels every active,
// nonterminal node. It returns an updated workflow value without mutating wf.
func CancelWorkflow(wf Workflow, canceledAt time.Time, reason string) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	updated := wf
	updated.Status = StatusCanceled
	updated.UpdatedAt = canceledAt
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	for i, node := range updated.Nodes {
		if !isActiveStatus(node.Status) {
			continue
		}
		updated.Nodes[i].Status = StatusCanceled
		updated.Nodes[i].EndedAt = canceledAt
		updated.Nodes[i].Error = reason
	}
	return updated, nil
}

func finishRunningNode(wf Workflow, nodeID string, status Status, endedAt time.Time, message string, outputs map[string]string, artifacts []Artifact) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}

	targetID := strings.TrimSpace(nodeID)
	targetIndex := -1
	for i, node := range wf.Nodes {
		if strings.TrimSpace(node.ID) == targetID {
			targetIndex = i
			break
		}
	}
	if targetIndex == -1 {
		return Workflow{}, fmt.Errorf("%w: %s", ErrUnknownNode, targetID)
	}

	target := wf.Nodes[targetIndex]
	if target.Status != StatusRunning {
		return Workflow{}, fmt.Errorf("%w: %s", ErrNodeNotRunning, strings.TrimSpace(target.ID))
	}

	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.Nodes[targetIndex].Status = status
	updated.Nodes[targetIndex].EndedAt = endedAt
	updated.Nodes[targetIndex].Error = message
	if len(outputs) > 0 {
		merged := make(map[string]string, len(target.Outputs)+len(outputs))
		for key, value := range target.Outputs {
			merged[key] = value
		}
		for key, value := range outputs {
			merged[key] = value
		}
		updated.Nodes[targetIndex].Outputs = merged
	}
	if len(artifacts) > 0 {
		updated.Nodes[targetIndex].Artifacts = append(append([]Artifact(nil), target.Artifacts...), artifacts...)
	}
	if status == StatusCompleted && allNodesCompleted(updated.Nodes) {
		updated.Status = StatusCompleted
	}
	return updated, nil
}

func findNodeIndex(nodes []Node, nodeID string) int {
	targetID := strings.TrimSpace(nodeID)
	for i, node := range nodes {
		if strings.TrimSpace(node.ID) == targetID {
			return i
		}
	}
	return -1
}

func isActiveStatus(status Status) bool {
	switch status {
	case StatusQueued, StatusRunning, StatusWaitingApproval, StatusResumed, StatusVerifying:
		return true
	default:
		return false
	}
}

func hasOngoingNode(nodes []Node) bool {
	for _, node := range nodes {
		switch node.Status {
		case StatusRunning, StatusWaitingApproval, StatusResumed, StatusVerifying:
			return true
		}
	}
	return false
}

func allNodesCompleted(nodes []Node) bool {
	if len(nodes) == 0 {
		return false
	}
	for _, node := range nodes {
		if node.Status != StatusCompleted && node.Status != StatusSkipped {
			return false
		}
	}
	return true
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
