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

	"github.com/mwigge/milliways/internal/daemon/textproc"
)

// apply.* JSON-RPC handlers. Currently only apply.extract is wired —
// it parses fenced code blocks out of the most recent runner output
// captured in the session's rolling response buffer (see
// AgentSession.recordResponse).

type applyExtractParams struct {
	Handle int64 `json:"handle"`
}

type applyExtractResult struct {
	Blocks []textproc.CodeBlock `json:"blocks"`
}

// applyExtract returns the parsed code blocks from the session's
// rolling response buffer. If the session is unknown the call errors;
// if the buffer is empty the result has an empty `blocks` slice.
func (s *Server) applyExtract(enc *json.Encoder, req *Request) {
	var p applyExtractParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeError(enc, req.ID, ErrInvalidParams, "invalid apply.extract params: "+err.Error())
		return
	}
	sess, ok := s.agents.Get(AgentHandle(p.Handle))
	if !ok {
		writeError(enc, req.ID, ErrInvalidParams, "unknown handle")
		return
	}
	text := sess.snapshotResponse()
	blocks := textproc.ExtractCodeBlocks(text)
	if blocks == nil {
		blocks = []textproc.CodeBlock{}
	}
	writeResult(enc, req.ID, applyExtractResult{Blocks: blocks})
}
