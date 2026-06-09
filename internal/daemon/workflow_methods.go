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

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/orchestrator"
	"github.com/mwigge/milliways/internal/workflow"
)

func (s *Server) workflowList(enc *json.Encoder, req *Request) {
	store := s.workflowStoreForRPC()
	if store == nil {
		writeResult(enc, req.ID, map[string]any{"workflows": []workflow.Summary{}})
		return
	}
	summaries, err := store.List(context.Background())
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow list: %v", err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflows": summaries})
}

func (s *Server) workflowTemplates(enc *json.Encoder, req *Request) {
	writeResult(enc, req.ID, map[string]any{"templates": workflow.BuiltInTemplates()})
}

func (s *Server) workflowCreate(enc *json.Encoder, req *Request) {
	var p struct {
		ID       string `json:"id"`
		Template string `json:"template"`
		Goal     string `json:"goal,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.Template = strings.TrimSpace(p.Template)
	p.Goal = strings.TrimSpace(p.Goal)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.create requires id")
		return
	}
	if p.Template == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.create requires template")
		return
	}
	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	now := time.Now().UTC()
	wf, err := workflow.InstantiateTemplate(p.Template, p.ID, p.Goal, now)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.create %s: %v", p.Template, err))
		return
	}
	if err := store.Save(context.Background(), wf); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.create save %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": wf})
}

func (s *Server) workflowGet(enc *json.Encoder, req *Request) {
	wf, ok := s.loadWorkflowByID(enc, req, "workflow.get")
	if !ok {
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": wf})
}

func (s *Server) workflowExport(enc *json.Encoder, req *Request) {
	wf, ok := s.loadWorkflowByID(enc, req, "workflow.export")
	if !ok {
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": wf})
}

func (s *Server) workflowImport(enc *json.Encoder, req *Request) {
	var p struct {
		Workflow workflow.Workflow `json:"workflow"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	if err := workflow.Validate(p.Workflow); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.import validate: %v", err))
		return
	}
	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	if err := store.Save(context.Background(), p.Workflow); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.import save %s: %v", p.Workflow.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": p.Workflow})
}

func (s *Server) workflowReady(enc *json.Encoder, req *Request) {
	wf, ok := s.loadWorkflowByID(enc, req, "workflow.ready")
	if !ok {
		return
	}
	nodes, err := workflow.ReadyNodes(wf)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow ready %s: %v", wf.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"nodes": nodes})
}

func (s *Server) workflowCancel(enc *json.Encoder, req *Request) {
	var p struct {
		ID     string `json:"id"`
		Reason string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.Reason = strings.TrimSpace(p.Reason)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.cancel requires id")
		return
	}

	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.cancel %s: %v", p.ID, err))
		return
	}
	endedAt := time.Now().UTC()
	updated, err := workflow.CancelWorkflow(wf, endedAt, p.Reason)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.cancel %s: %v", p.ID, err))
		return
	}
	if err := store.Save(context.Background(), updated); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.cancel save %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": updated})
}

func (s *Server) workflowNodeStart(enc *json.Encoder, req *Request) {
	var p struct {
		ID     string `json:"id"`
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.NodeID = strings.TrimSpace(p.NodeID)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.start requires id")
		return
	}
	if p.NodeID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.start requires node_id")
		return
	}

	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.start %s: %v", p.ID, err))
		return
	}
	startedAt := time.Now().UTC()
	updated, err := workflow.StartReadyNode(wf, p.NodeID, startedAt)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.start %s/%s: %v", p.ID, p.NodeID, err))
		return
	}
	updated.Status = workflow.StatusRunning
	updated.UpdatedAt = startedAt
	node := workflow.Node{}
	for _, candidate := range updated.Nodes {
		if strings.TrimSpace(candidate.ID) == p.NodeID {
			node = candidate
			break
		}
	}
	if err := store.Save(context.Background(), updated); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.start save %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": updated, "node": node})
}

func (s *Server) workflowNodeDelegate(enc *json.Encoder, req *Request) {
	var p struct {
		ID     string `json:"id"`
		NodeID string `json:"node_id"`
		Agent  string `json:"agent,omitempty"`
		Dir    string `json:"dir,omitempty"`
		Prompt string `json:"prompt,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.NodeID = strings.TrimSpace(p.NodeID)
	p.Agent = strings.TrimSpace(p.Agent)
	p.Dir = strings.TrimSpace(p.Dir)
	p.Prompt = strings.TrimSpace(p.Prompt)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.delegate requires id")
		return
	}
	if p.NodeID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.delegate requires node_id")
		return
	}

	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.delegate %s: %v", p.ID, err))
		return
	}
	node := workflowNodeByID(wf, p.NodeID)
	if node.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.delegate %s/%s: %v", p.ID, p.NodeID, workflow.ErrUnknownNode))
		return
	}
	agent := firstWorkflowValue(p.Agent, node.Agent, node.Inputs["agent"])
	dir := firstWorkflowValue(p.Dir, node.Inputs["dir"], ".")
	prompt := firstWorkflowValue(p.Prompt, node.Inputs["prompt"], node.Inputs["task"], node.Inputs["goal"])
	if strings.TrimSpace(agent) == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.delegate requires agent")
		return
	}
	if strings.TrimSpace(prompt) == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.delegate requires prompt")
		return
	}

	startedAt := time.Now().UTC()
	updated, err := workflow.StartReadyNode(wf, p.NodeID, startedAt)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.delegate %s/%s: %v", p.ID, p.NodeID, err))
		return
	}
	updated.Status = workflow.StatusRunning
	updated.UpdatedAt = startedAt
	startedNode := workflowNodeByID(updated, p.NodeID)
	if err := store.Save(context.Background(), updated); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.delegate save %s: %v", p.ID, err))
		return
	}

	ctx := s.bgCtx
	if ctx == nil {
		ctx = context.Background()
	}
	s.bgWG.Add(1)
	go func() {
		defer s.bgWG.Done()
		s.finishWorkflowDelegate(ctx, store, p.ID, p.NodeID, agent, dir, prompt)
	}()
	writeResult(enc, req.ID, map[string]any{"workflow": updated, "node": startedNode})
}

func (s *Server) finishWorkflowDelegate(ctx context.Context, store *workflow.FileStore, workflowID, nodeID, agent, dir, prompt string) {
	runner := s.workflowDelegateRunner
	if runner == nil {
		runner = func(ctx context.Context, agent, dir, prompt string) (string, error) {
			return orchestrator.TraceDelegate(ctx, nil, agent, dir, prompt)
		}
	}
	output, runErr := runner(ctx, agent, dir, prompt)
	endedAt := time.Now().UTC()

	outcome := "pass"
	if runErr != nil {
		outcome = "fail"
	} else if strings.Contains(strings.ToLower(output), "stall") ||
		strings.Contains(strings.ToLower(output), "no commits appear for 300s") {
		outcome = "rework"
	}
	s.recordDelegateOutcome(outcome)

	wf, err := store.Load(context.Background(), workflowID)
	if err != nil {
		return
	}
	var updated workflow.Workflow
	if runErr != nil {
		updated, err = workflow.FailRunningNode(wf, nodeID, endedAt, strings.TrimSpace(runErr.Error()))
		if err == nil {
			updated.Status = workflow.StatusFailed
		}
	} else {
		updated, err = workflow.CompleteRunningNode(wf, nodeID, endedAt, map[string]string{"delegate_output": output}, nil)
	}
	if err != nil {
		return
	}
	updated.UpdatedAt = endedAt
	_ = store.Save(context.Background(), updated)
}

func (s *Server) workflowNodeComplete(enc *json.Encoder, req *Request) {
	var p struct {
		ID        string              `json:"id"`
		NodeID    string              `json:"node_id"`
		Outputs   map[string]string   `json:"outputs"`
		Artifacts []workflow.Artifact `json:"artifacts"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.NodeID = strings.TrimSpace(p.NodeID)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.complete requires id")
		return
	}
	if p.NodeID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.complete requires node_id")
		return
	}

	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.complete %s: %v", p.ID, err))
		return
	}
	endedAt := time.Now().UTC()
	updated, err := workflow.CompleteRunningNode(wf, p.NodeID, endedAt, p.Outputs, p.Artifacts)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.complete %s/%s: %v", p.ID, p.NodeID, err))
		return
	}
	updated.UpdatedAt = endedAt
	node := workflowNodeByID(updated, p.NodeID)
	readyNodes, err := workflow.ReadyNodes(updated)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.complete ready %s: %v", p.ID, err))
		return
	}
	if err := store.Save(context.Background(), updated); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.complete save %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": updated, "node": node, "ready_nodes": readyNodes})
}

func (s *Server) workflowNodeFail(enc *json.Encoder, req *Request) {
	var p struct {
		ID      string `json:"id"`
		NodeID  string `json:"node_id"`
		Message string `json:"error"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.NodeID = strings.TrimSpace(p.NodeID)
	p.Message = strings.TrimSpace(p.Message)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.fail requires id")
		return
	}
	if p.NodeID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.fail requires node_id")
		return
	}
	if p.Message == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.fail requires error")
		return
	}

	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.fail %s: %v", p.ID, err))
		return
	}
	endedAt := time.Now().UTC()
	updated, err := workflow.FailRunningNode(wf, p.NodeID, endedAt, p.Message)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.fail %s/%s: %v", p.ID, p.NodeID, err))
		return
	}
	updated.Status = workflow.StatusFailed
	updated.UpdatedAt = endedAt
	node := workflowNodeByID(updated, p.NodeID)
	if err := store.Save(context.Background(), updated); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.fail save %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": updated, "node": node})
}

func (s *Server) workflowNodeRetry(enc *json.Encoder, req *Request) {
	var p struct {
		ID     string `json:"id"`
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.NodeID = strings.TrimSpace(p.NodeID)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.retry requires id")
		return
	}
	if p.NodeID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.retry requires node_id")
		return
	}

	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.retry %s: %v", p.ID, err))
		return
	}
	retriedAt := time.Now().UTC()
	updated, err := workflow.RetryNode(wf, p.NodeID, retriedAt)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.retry %s/%s: %v", p.ID, p.NodeID, err))
		return
	}
	updated.UpdatedAt = retriedAt
	node := workflowNodeByID(updated, p.NodeID)
	if err := store.Save(context.Background(), updated); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.retry save %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": updated, "node": node})
}

func (s *Server) workflowNodeWaitApproval(enc *json.Encoder, req *Request) {
	var p struct {
		ID     string `json:"id"`
		NodeID string `json:"node_id"`
		Reason string `json:"reason,omitempty"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.NodeID = strings.TrimSpace(p.NodeID)
	p.Reason = strings.TrimSpace(p.Reason)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.wait_approval requires id")
		return
	}
	if p.NodeID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.wait_approval requires node_id")
		return
	}

	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.wait_approval %s: %v", p.ID, err))
		return
	}
	waitedAt := time.Now().UTC()
	updated, err := workflow.WaitForApprovalNode(wf, p.NodeID, waitedAt, p.Reason)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.wait_approval %s/%s: %v", p.ID, p.NodeID, err))
		return
	}
	updated.UpdatedAt = waitedAt
	node := workflowNodeByID(updated, p.NodeID)
	if err := store.Save(context.Background(), updated); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.wait_approval save %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": updated, "node": node})
}

func (s *Server) workflowNodeResume(enc *json.Encoder, req *Request) {
	var p struct {
		ID     string `json:"id"`
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.NodeID = strings.TrimSpace(p.NodeID)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.resume requires id")
		return
	}
	if p.NodeID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.resume requires node_id")
		return
	}

	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.resume %s: %v", p.ID, err))
		return
	}
	resumedAt := time.Now().UTC()
	updated, err := workflow.ResumeApprovalNode(wf, p.NodeID, resumedAt)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.resume %s/%s: %v", p.ID, p.NodeID, err))
		return
	}
	updated.UpdatedAt = resumedAt
	node := workflowNodeByID(updated, p.NodeID)
	if err := store.Save(context.Background(), updated); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.resume save %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": updated, "node": node})
}

func (s *Server) workflowNodeDeny(enc *json.Encoder, req *Request) {
	var p struct {
		ID     string `json:"id"`
		NodeID string `json:"node_id"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	p.NodeID = strings.TrimSpace(p.NodeID)
	p.Reason = strings.TrimSpace(p.Reason)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.deny requires id")
		return
	}
	if p.NodeID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.deny requires node_id")
		return
	}
	if p.Reason == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.node.deny requires reason")
		return
	}

	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.deny %s: %v", p.ID, err))
		return
	}
	deniedAt := time.Now().UTC()
	updated, err := workflow.DenyApprovalNode(wf, p.NodeID, deniedAt, p.Reason)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.deny %s/%s: %v", p.ID, p.NodeID, err))
		return
	}
	updated.UpdatedAt = deniedAt
	node := workflowNodeByID(updated, p.NodeID)
	if err := store.Save(context.Background(), updated); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow.node.deny save %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": updated, "node": node})
}

func (s *Server) loadWorkflowByID(enc *json.Encoder, req *Request, method string) (workflow.Workflow, bool) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return workflow.Workflow{}, false
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, method+" requires id")
		return workflow.Workflow{}, false
	}
	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return workflow.Workflow{}, false
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("%s %s: %v", method, p.ID, err))
		return workflow.Workflow{}, false
	}
	return wf, true
}

func workflowNodeByID(wf workflow.Workflow, nodeID string) workflow.Node {
	for _, candidate := range wf.Nodes {
		if strings.TrimSpace(candidate.ID) == nodeID {
			return candidate
		}
	}
	return workflow.Node{}
}

func firstWorkflowValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s *Server) workflowStoreForRPC() *workflow.FileStore {
	if s.workflowStore != nil {
		return s.workflowStore
	}
	if strings.TrimSpace(s.socket) == "" {
		return nil
	}
	s.workflowStore = workflow.NewFileStore(filepath.Join(filepath.Dir(s.socket), "workflows"))
	return s.workflowStore
}
