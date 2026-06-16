package workflow

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type LocalExecutionControl struct {
	GlobalEnabled bool            `json:"global_enabled"`
	Sessions      map[string]bool `json:"sessions,omitempty"`
}

func NewTieredWorkflow(id, goal string, now time.Time) Workflow {
	nodes := []Node{
		{ID: "plan", Type: NodePlan, Status: StatusQueued},
		{ID: "qualify", Type: NodeQualify, Status: StatusQueued},
		{ID: "prewarm", Type: NodePrewarm, Status: StatusQueued},
		{ID: "execute", Type: NodeExecute, Status: StatusQueued},
		{ID: "verify", Type: NodeVerify, Status: StatusQueued},
		{ID: "review", Type: NodeReview, Status: StatusQueued},
		{ID: "repair", Type: NodeRepair, Status: StatusQueued},
		{ID: "reroute", Type: NodeReroute, Status: StatusQueued},
		{ID: "accept", Type: NodeAccept, Status: StatusQueued},
		{ID: "supervisor-takeover", Type: NodeTakeover, Status: StatusQueued},
	}
	edges := make([]Edge, 0, len(nodes)-1)
	for i := 1; i < len(nodes); i++ {
		edges = append(edges, Edge{From: nodes[i-1].ID, To: nodes[i].ID})
	}
	return Workflow{
		ID: id, Goal: goal, Status: StatusQueued, Nodes: nodes, Edges: edges,
		CreatedAt: now, UpdatedAt: now,
	}
}

// ParallelCompatible reports whether two nodes can run concurrently. Nodes
// are incompatible if their declared write scopes overlap, or if both
// declare the same non-empty "verification_lock" input (an exclusive lock
// shared by verification operations). The returned string is empty when
// compatible, or a short reason when not.
func ParallelCompatible(left, right Node) (bool, string) {
	if pathsOverlap(left.Security.Paths, right.Security.Paths) {
		return false, "declared write scopes overlap"
	}
	leftLock := left.Inputs["verification_lock"]
	rightLock := right.Inputs["verification_lock"]
	if leftLock != "" && leftLock == rightLock {
		return false, "verification operations share an exclusive lock"
	}
	return true, ""
}

// ConflictAwareReadyNodes partitions the workflow's ready nodes (as returned
// by ReadyNodes) into a set that can run in parallel and a set that must wait
// for serialized execution. A ready node is added to ready only if it is
// ParallelCompatible with every node already selected for ready; otherwise it
// is placed in serialized.
func ConflictAwareReadyNodes(wf Workflow) (ready []Node, serialized []Node, err error) {
	candidates, err := ReadyNodes(wf)
	if err != nil {
		return nil, nil, err
	}
	for _, candidate := range candidates {
		compatible := true
		for _, selected := range ready {
			if ok, _ := ParallelCompatible(candidate, selected); !ok {
				compatible = false
				break
			}
		}
		if compatible {
			ready = append(ready, candidate)
		} else {
			serialized = append(serialized, candidate)
		}
	}
	return ready, serialized, nil
}

// RepairOrTakeover decides whether a failed node should be retried via the
// repair path or escalated to the supervisor takeover path. It returns
// "repair" while node.RetryCount is below maxRepairs (negative maxRepairs is
// treated as zero), and "supervisor-takeover" once the retry budget is
// exhausted.
func RepairOrTakeover(node Node, maxRepairs int) string {
	if maxRepairs < 0 {
		maxRepairs = 0
	}
	if node.RetryCount < maxRepairs {
		return "repair"
	}
	return "supervisor-takeover"
}

func (control LocalExecutionControl) Enabled(sessionID string) bool {
	if !control.GlobalEnabled {
		return false
	}
	enabled, configured := control.Sessions[sessionID]
	return !configured || enabled
}

func DisableLocalExecution(wf Workflow, disabledAt time.Time, reason string) (Workflow, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, err
	}
	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	updated.UpdatedAt = disabledAt
	for index, node := range updated.Nodes {
		if !isLocalTierNode(node.Type) {
			continue
		}
		switch node.Status {
		case StatusQueued:
			updated.Nodes[index].Status = StatusCanceled
			updated.Nodes[index].EndedAt = disabledAt
			updated.Nodes[index].Error = reason
		case StatusRunning, StatusResumed, StatusVerifying:
			updated.Nodes[index].Status = StatusDraining
			updated.Nodes[index].Error = reason
		}
	}
	return updated, nil
}

func RecoverTieredInterrupted(wf Workflow, recoveredAt time.Time, repositoryUnchanged bool) (Workflow, bool, error) {
	if err := Validate(wf); err != nil {
		return Workflow{}, false, err
	}
	updated := wf
	updated.Nodes = append([]Node(nil), wf.Nodes...)
	changed := false
	for index, node := range updated.Nodes {
		if node.Status != StatusRunning && node.Status != StatusResumed && node.Status != StatusVerifying && node.Status != StatusDraining {
			continue
		}
		changed = true
		if repositoryUnchanged && (node.Type == NodeReview || node.Type == NodeAccept) {
			updated.Nodes[index].Status = StatusQueued
			updated.Nodes[index].StartedAt = time.Time{}
			updated.Nodes[index].EndedAt = time.Time{}
			updated.Nodes[index].Error = ""
			continue
		}
		updated.Nodes[index].Status = StatusFailed
		updated.Nodes[index].EndedAt = recoveredAt
		updated.Nodes[index].Error = "daemon restarted during local execution"
	}
	if changed {
		updated.UpdatedAt = recoveredAt
		updated.Status = StatusRunning
	}
	return updated, changed, nil
}

func pathsOverlap(left, right []string) bool {
	for _, leftPath := range normalizedPaths(left) {
		for _, rightPath := range normalizedPaths(right) {
			if leftPath == rightPath ||
				strings.HasPrefix(leftPath, rightPath+"/") ||
				strings.HasPrefix(rightPath, leftPath+"/") {
				return true
			}
		}
	}
	return false
}

func normalizedPaths(paths []string) []string {
	normalized := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.ToSlash(filepath.Clean(path))
		if clean != "." && clean != "" {
			normalized = append(normalized, strings.TrimSuffix(clean, "/"))
		}
	}
	sort.Strings(normalized)
	return normalized
}

func isLocalTierNode(nodeType NodeType) bool {
	switch nodeType {
	case NodeQualify, NodePrewarm, NodeExecute, NodeVerify, NodeRepair, NodeReroute:
		return true
	default:
		return false
	}
}

func ValidateTieredWorkflow(wf Workflow) error {
	if err := Validate(wf); err != nil {
		return err
	}
	required := map[NodeType]bool{
		NodePlan: false, NodeQualify: false, NodePrewarm: false, NodeExecute: false,
		NodeVerify: false, NodeReview: false, NodeRepair: false, NodeReroute: false,
		NodeAccept: false, NodeTakeover: false,
	}
	for _, node := range wf.Nodes {
		if _, ok := required[node.Type]; ok {
			required[node.Type] = true
		}
	}
	for nodeType, present := range required {
		if !present {
			return fmt.Errorf("tiered workflow missing %s node", nodeType)
		}
	}
	return nil
}
