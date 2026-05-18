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
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mwigge/milliways/internal/pantry"
)

type approvalListResult struct {
	Approvals []approvalRequestWire `json:"approvals"`
	Storage   string                `json:"storage"`
}

type approvalRequestWire struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Command   string `json:"command,omitempty"`
	Path      string `json:"path,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
	Decision  string `json:"decision,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type approvalRespondParams struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

type approvalRespondResult struct {
	OK       bool   `json:"ok"`
	Accepted bool   `json:"accepted"`
	ID       string `json:"id"`
	Decision string `json:"decision"`
	Storage  string `json:"storage"`
}

func (s *Server) approvalList(enc *json.Encoder, req *Request) {
	if s.pantryDB != nil {
		approvals, err := s.pantryDB.Coding().ListToolApprovals("", "", 50)
		if err != nil {
			writeError(enc, req.ID, ErrInternal, fmt.Sprintf("approval.list: %v", err))
			return
		}
		out := make([]approvalRequestWire, 0, len(approvals))
		for _, approval := range approvals {
			out = append(out, approvalWireFromRecord(approval))
		}
		writeResult(enc, req.ID, approvalListResult{
			Approvals: out,
			Storage:   "pantry",
		})
		return
	}
	writeResult(enc, req.ID, approvalListResult{
		Approvals: []approvalRequestWire{},
		Storage:   "stub",
	})
}

func (s *Server) approvalRespond(enc *json.Encoder, req *Request) {
	var p approvalRespondParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "approval.respond requires id")
		return
	}
	p.Decision = normalizeApprovalDecision(p.Decision)
	if p.Decision == "" {
		writeError(enc, req.ID, ErrInvalidParams, "approval.respond requires decision approve or deny")
		return
	}
	if s.pantryDB != nil {
		numericID, err := strconv.ParseInt(p.ID, 10, 64)
		if err != nil || numericID <= 0 {
			writeError(enc, req.ID, ErrInvalidParams, "approval.respond pantry id must be a positive integer")
			return
		}
		if err := s.pantryDB.Coding().UpdateToolApprovalDecision(numericID, p.Decision, p.Reason); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, err.Error())
			return
		}
		notifyToolApproval(numericID, p.Decision)
		writeResult(enc, req.ID, approvalRespondResult{
			OK:       true,
			Accepted: true,
			ID:       p.ID,
			Decision: p.Decision,
			Storage:  "pantry",
		})
		return
	}
	writeResult(enc, req.ID, approvalRespondResult{
		OK:       true,
		Accepted: true,
		ID:       p.ID,
		Decision: p.Decision,
		Storage:  "stub",
	})
}

func normalizeApprovalDecision(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approve", "approved", "allow", "allowed", "yes":
		return "approve"
	case "deny", "denied", "reject", "rejected", "no":
		return "deny"
	default:
		return ""
	}
}

func approvalWireFromRecord(a pantry.ToolApproval) approvalRequestWire {
	summary := strings.TrimSpace(a.ToolName)
	if a.Operation != "" {
		summary = strings.TrimSpace(summary + " " + a.Operation)
	}
	if a.Path != "" {
		summary = strings.TrimSpace(summary + " " + a.Path)
	}
	return approvalRequestWire{
		ID:        strconv.FormatInt(a.ID, 10),
		AgentID:   a.Client,
		SessionID: a.SessionID,
		Kind:      a.Operation,
		Summary:   summary,
		Path:      a.Path,
		CreatedAt: a.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Decision:  a.Decision,
		Reason:    a.Reason,
	}
}
