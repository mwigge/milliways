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
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mwigge/milliways/internal/daemon/runners"
	"github.com/mwigge/milliways/internal/pantry"
	mwtools "github.com/mwigge/milliways/internal/tools"
)

func codingToolHooks(store *pantry.CodingStore, agentID, workspace string) runners.ToolHooks {
	return runners.ToolHooks{
		Workspace: workspace,
		Decide: func(ctx context.Context, req runners.ToolDecisionRequest) (runners.ToolDecisionResult, error) {
			permReq := mwtools.BuildToolPermissionRequest(req.ToolName, req.Args)
			policy := mwtools.NewPermissionPolicy(codingPermissionModeFromEnv())
			perm := policy.Evaluate(permReq)

			metadata := map[string]any{
				"permission_decision": string(perm.Decision),
				"permission_reason":   perm.Reason,
				"operation":           string(perm.Request.Operation),
			}
			var approvalID int64
			if meta, ok, err := mwtools.ExtractMutationMetadata(req.ToolName, req.Args); err != nil {
				return runners.ToolDecisionResult{
					Decision: runners.ToolDecisionBlock,
					Message:  fmt.Sprintf("error: mutation metadata refused: %v", err),
					Metadata: metadata,
				}, nil
			} else if ok {
				metadata["mutation"] = meta
				if store != nil {
					var err error
					approvalID, err = recordToolApproval(store, pantry.ToolApproval{
						Workspace:  workspace,
						SessionID:  req.SessionID,
						Client:     agentID,
						ToolCallID: req.Call.ID,
						ToolName:   req.ToolName,
						Operation:  string(meta.Operation),
						Path:       meta.Path,
						BeforeHash: meta.BeforeHash,
						Diff:       meta.Diff,
						Preview:    meta.Preview,
						Reason:     perm.Reason,
						CreatedAt:  time.Now().UTC(),
					}, perm.Decision)
					if err != nil {
						return runners.ToolDecisionResult{}, err
					}
					metadata["approval_id"] = approvalID
				}
			} else if perm.Decision == mwtools.PermissionAsk && store != nil {
				var err error
				approvalID, err = recordToolApproval(store, pantry.ToolApproval{
					Workspace:  workspace,
					SessionID:  req.SessionID,
					Client:     agentID,
					ToolCallID: req.Call.ID,
					ToolName:   req.ToolName,
					Operation:  string(perm.Request.Operation),
					Path:       perm.Request.Path,
					Preview:    sensitiveToolPreview(perm.Request),
					Decision:   "pending",
					Reason:     perm.Reason,
					CreatedAt:  time.Now().UTC(),
				}, perm.Decision)
				if err != nil {
					return runners.ToolDecisionResult{}, err
				}
				metadata["approval_id"] = approvalID
			}

			switch perm.Decision {
			case mwtools.PermissionDeny:
				return runners.ToolDecisionResult{
					Decision: runners.ToolDecisionBlock,
					Message:  "tool execution denied by MilliWays permission policy: " + perm.Reason,
					Metadata: metadata,
				}, nil
			case mwtools.PermissionAsk:
				if approvalID > 0 {
					decision, err := waitForToolApproval(ctx, store, approvalID)
					if err != nil {
						return runners.ToolDecisionResult{
							Decision: runners.ToolDecisionBlock,
							Message:  "approval wait cancelled before tool execution: " + err.Error(),
							Metadata: metadata,
						}, nil
					}
					metadata["approval_decision"] = decision
					if decision == "approve" {
						return runners.ToolDecisionResult{Decision: runners.ToolDecisionAllow, Metadata: metadata}, nil
					}
					return runners.ToolDecisionResult{
						Decision: runners.ToolDecisionBlock,
						Message:  "tool execution denied by approval response",
						Metadata: metadata,
					}, nil
				}
				return runners.ToolDecisionResult{
					Decision: runners.ToolDecisionNeedsApproval,
					Message:  "approval required before tool execution: " + perm.Reason,
					Metadata: metadata,
				}, nil
			default:
				return runners.ToolDecisionResult{Decision: runners.ToolDecisionAllow, Metadata: metadata}, nil
			}
		},
		After: func(ctx context.Context, event runners.ToolExecutionEvent) error {
			if store == nil || len(event.FileChanges) == 0 {
				return nil
			}
			changeSetID, err := store.InsertChangeSet(pantry.CodingChangeSet{
				Workspace:  workspace,
				SessionID:  event.SessionID,
				Client:     agentID,
				ToolCallID: event.Call.ID,
				Operation:  event.ToolName,
				Status:     codingChangeSetStatus(event),
				Reason:     codingChangeSetReason(event),
				CreatedAt:  time.Now().UTC(),
			})
			if err != nil {
				return err
			}
			for _, change := range event.FileChanges {
				if _, err := store.InsertFileChange(pantry.CodingFileChange{
					ChangeSetID: changeSetID,
					Workspace:   workspace,
					SessionID:   event.SessionID,
					Client:      agentID,
					ToolCallID:  event.Call.ID,
					Operation:   event.ToolName,
					Path:        change.Path,
					AfterHash:   codingFileHash(change.Path),
					CreatedAt:   time.Now().UTC(),
				}); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func recordToolApproval(store *pantry.CodingStore, approval pantry.ToolApproval, decision mwtools.PermissionDecision) (int64, error) {
	approval.Decision = string(decision)
	if decision == mwtools.PermissionAsk {
		approval.Decision = "pending"
	}
	return store.RecordToolApproval(approval)
}

func sensitiveToolPreview(req mwtools.ToolPermissionRequest) string {
	if req.Command != "" {
		return req.Command
	}
	if req.URL != "" {
		return req.URL
	}
	return ""
}

func codingPermissionModeFromEnv() mwtools.PermissionMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MILLIWAYS_PERMISSION_MODE"))) {
	case "read_only", "readonly", "plan", "review":
		return mwtools.PermissionModeReadOnly
	case "ask", "default":
		return mwtools.PermissionModeDefault
	default:
		return mwtools.PermissionModeAuto
	}
}

func codingChangeSetStatus(event runners.ToolExecutionEvent) string {
	if event.Blocked {
		return "blocked"
	}
	if strings.HasPrefix(strings.TrimSpace(event.Result), "error:") {
		return "error"
	}
	return "completed"
}

func codingChangeSetReason(event runners.ToolExecutionEvent) string {
	if event.Blocked {
		return "blocked by output gate or permission hook"
	}
	if strings.HasPrefix(strings.TrimSpace(event.Result), "error:") {
		return strings.TrimSpace(event.Result)
	}
	return "tool changed workspace files"
}

func codingFileHash(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("sha256:%x", sum[:])
}
