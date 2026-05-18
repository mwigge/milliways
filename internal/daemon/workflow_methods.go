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

func (s *Server) workflowGet(enc *json.Encoder, req *Request) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("decode params: %v", err))
		return
	}
	p.ID = strings.TrimSpace(p.ID)
	if p.ID == "" {
		writeError(enc, req.ID, ErrInvalidParams, "workflow.get requires id")
		return
	}
	store := s.workflowStoreForRPC()
	if store == nil {
		writeError(enc, req.ID, ErrInvalidParams, "workflow store unavailable")
		return
	}
	wf, err := store.Load(context.Background(), p.ID)
	if err != nil {
		writeError(enc, req.ID, ErrInvalidParams, fmt.Sprintf("workflow get %s: %v", p.ID, err))
		return
	}
	writeResult(enc, req.ID, map[string]any{"workflow": wf})
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
