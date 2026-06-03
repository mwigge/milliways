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
)

type codingChangesResult struct {
	Changes []codingChangeWire `json:"changes"`
	Storage string             `json:"storage"`
}

type codingChangeWire struct {
	ID        int64    `json:"id"`
	Workspace string   `json:"workspace,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	Client    string   `json:"client,omitempty"`
	Operation string   `json:"operation,omitempty"`
	Status    string   `json:"status,omitempty"`
	Reason    string   `json:"reason,omitempty"`
	Paths     []string `json:"paths,omitempty"`
	CreatedAt string   `json:"created_at,omitempty"`
}

type codingDiffParams struct {
	ID int64 `json:"id,omitempty"`
}

type codingDiffResult struct {
	ID      int64  `json:"id,omitempty"`
	Diff    string `json:"diff"`
	Storage string `json:"storage"`
}

func (s *Server) codingChanges(enc *json.Encoder, req *Request) {
	if s.pantryDB == nil {
		writeResult(enc, req.ID, codingChangesResult{Changes: []codingChangeWire{}, Storage: "stub"})
		return
	}
	sets, err := s.pantryDB.Coding().ListChangeSets("", "", 20)
	if err != nil {
		writeError(enc, req.ID, ErrInternal, fmt.Sprintf("coding.changes: %v", err))
		return
	}
	out := make([]codingChangeWire, 0, len(sets))
	for _, set := range sets {
		paths := []string{}
		files, err := s.pantryDB.Coding().ListFileChanges(set.ID)
		if err == nil {
			for _, file := range files {
				paths = append(paths, file.Path)
			}
		}
		out = append(out, codingChangeWire{
			ID:        set.ID,
			Workspace: set.Workspace,
			SessionID: set.SessionID,
			Client:    set.Client,
			Operation: set.Operation,
			Status:    set.Status,
			Reason:    set.Reason,
			Paths:     paths,
			CreatedAt: set.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	writeResult(enc, req.ID, codingChangesResult{Changes: out, Storage: "pantry"})
}

func (s *Server) codingDiff(enc *json.Encoder, req *Request) {
	var p codingDiffParams
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			writeError(enc, req.ID, ErrInvalidParams, "invalid coding.diff params: "+err.Error())
			return
		}
	}
	if s.pantryDB == nil {
		writeResult(enc, req.ID, codingDiffResult{Storage: "stub"})
		return
	}
	id := p.ID
	if id == 0 {
		sets, err := s.pantryDB.Coding().ListChangeSets("", "", 1)
		if err != nil {
			writeError(enc, req.ID, ErrInternal, fmt.Sprintf("coding.diff: %v", err))
			return
		}
		if len(sets) == 0 {
			writeResult(enc, req.ID, codingDiffResult{Storage: "pantry"})
			return
		}
		id = sets[0].ID
	}
	files, err := s.pantryDB.Coding().ListFileChanges(id)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, err.Error())
		return
	}
	diff := ""
	for _, file := range files {
		if file.Diff != "" {
			diff += file.Diff + "\n"
		} else if file.Preview != "" {
			diff += "### " + file.Path + "\n" + file.Preview + "\n"
		}
	}
	writeResult(enc, req.ID, codingDiffResult{ID: id, Diff: diff, Storage: "pantry"})
}
